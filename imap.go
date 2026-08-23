package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	gmailIMAPAddress = "imap.gmail.com:993"
	gmailSMTPAddress = "smtp.gmail.com:465"
)

type IMAPMailClient struct {
	cfg            GmailConfig
	email          string
	password       string
	imapAddr       string
	smtpAddr       string
	smtpServerName string
	dialIMAP       func(context.Context) (net.Conn, error)
	dialSMTP       func(context.Context) (net.Conn, error)

	// A poll used to dial, TLS-handshake, LOGIN and EXAMINE twice — once for
	// List and again for Get — costing seconds per SMS. One authenticated
	// session is now reused for both, reconnecting only when it actually
	// breaks. IDLE keeps its own separate connection: it monopolises a session.
	sessMu   sync.Mutex
	sess     *imapSession
	sendMu   sync.Mutex
	sendConn net.Conn
	sendCli  *smtp.Client
}

// withSession runs fn against a reused authenticated IMAP session. On any
// failure the session is dropped and fn is retried exactly once on a fresh
// connection, so a server-side timeout is indistinguishable from before.
func (c *IMAPMailClient) withSession(ctx context.Context, fn func(*imapSession) error) error {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()

	if s := c.sess; s != nil {
		// NOOP is not merely a keepalive here: RFC 3501 requires the server to
		// flush pending untagged EXISTS responses on it, which is what refreshes
		// the selected mailbox so a reused session can see mail that arrived
		// after EXAMINE.
		if _, err := s.command(ctx, "NOOP"); err == nil {
			if err := fn(s); err == nil {
				return nil
			}
		}
		c.dropSessionLocked()
	}

	s, err := c.openIMAP(ctx)
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		s.close()
		return err
	}
	c.sess = s
	return nil
}

// dropSessionLocked closes a suspect connection without a LOGOUT round trip.
func (c *IMAPMailClient) dropSessionLocked() {
	if c.sess != nil {
		if c.sess.conn != nil {
			_ = c.sess.conn.Close()
		}
		c.sess = nil
	}
}

// Close releases the pooled IMAP and SMTP connections.
func (c *IMAPMailClient) Close() {
	if c == nil {
		return
	}
	c.sessMu.Lock()
	c.dropSessionLocked()
	c.sessMu.Unlock()
	c.sendMu.Lock()
	c.dropSMTPLocked()
	c.sendMu.Unlock()
}

func NewIMAPMailClient(cfg GmailConfig, secretPath string) (*IMAPMailClient, error) {
	s, err := loadAppPasswordSecret(secretPath)
	if err != nil {
		return nil, fmt.Errorf("load Gmail App Password: %w", err)
	}
	c := &IMAPMailClient{
		cfg:            cfg,
		email:          s.Email,
		password:       s.Password,
		imapAddr:       gmailIMAPAddress,
		smtpAddr:       gmailSMTPAddress,
		smtpServerName: "smtp.gmail.com",
	}
	c.dialIMAP = func(ctx context.Context) (net.Conn, error) {
		d := &tls.Dialer{
			NetDialer: &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second},
			Config:    &tls.Config{ServerName: "imap.gmail.com", MinVersion: tls.VersionTLS12},
		}
		return d.DialContext(ctx, "tcp", c.imapAddr)
	}
	c.dialSMTP = func(ctx context.Context) (net.Conn, error) {
		d := &tls.Dialer{
			NetDialer: &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second},
			Config:    &tls.Config{ServerName: "smtp.gmail.com", MinVersion: tls.VersionTLS12},
		}
		return d.DialContext(ctx, "tcp", c.smtpAddr)
	}
	return c, nil
}

func (c *IMAPMailClient) Authorized() bool { return c != nil && c.email != "" && c.password != "" }

func deadlineFor(ctx context.Context, fallback time.Duration) time.Time {
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	return time.Now().Add(fallback)
}

type imapSession struct {
	conn net.Conn
	r    *bufio.Reader
	seq  int
}

func quoteIMAP(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

func (s *imapSession) nextTag() string {
	s.seq++
	return fmt.Sprintf("A%04d", s.seq)
}

func (s *imapSession) command(ctx context.Context, command string) ([]string, error) {
	_ = s.conn.SetDeadline(deadlineFor(ctx, 30*time.Second))
	tag := s.nextTag()
	if _, err := fmt.Fprintf(s.conn, "%s %s\r\n", tag, command); err != nil {
		return nil, err
	}
	var lines []string
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return lines, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, tag+" ") {
			parts := strings.SplitN(line, " ", 3)
			if len(parts) < 2 || !strings.EqualFold(parts[1], "OK") {
				return lines, fmt.Errorf("IMAP %s failed: %s", commandName(command), line)
			}
			return lines, nil
		}
		lines = append(lines, line)
	}
}

func commandName(v string) string {
	f := strings.Fields(v)
	if len(f) == 0 {
		return "command"
	}
	if strings.EqualFold(f[0], "UID") && len(f) > 1 {
		return "UID " + f[1]
	}
	return f[0]
}

func (s *imapSession) close() {
	if s == nil || s.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, _ = s.command(ctx, "LOGOUT")
	cancel()
	_ = s.conn.Close()
}

func (c *IMAPMailClient) openIMAP(ctx context.Context) (*imapSession, error) {
	if !c.Authorized() {
		return nil, errors.New("Gmail App Password is not configured")
	}
	conn, err := c.dialIMAP(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to Gmail IMAP: %w", err)
	}
	_ = conn.SetDeadline(deadlineFor(ctx, 30*time.Second))
	s := &imapSession{conn: conn, r: bufio.NewReaderSize(conn, 64*1024)}
	greeting, err := s.r.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("Gmail IMAP greeting: %w", err)
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(greeting)), "* OK") {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected Gmail IMAP greeting: %s", strings.TrimSpace(greeting))
	}
	if _, err := s.command(ctx, "LOGIN "+quoteIMAP(c.email)+" "+quoteIMAP(c.password)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("Gmail IMAP login failed: %w", err)
	}
	if _, err := s.command(ctx, "EXAMINE INBOX"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open Gmail inbox: %w", err)
	}
	return s, nil
}

func (c *IMAPMailClient) Test(ctx context.Context) error {
	s, err := c.openIMAP(ctx)
	if err != nil {
		return err
	}
	s.close()
	smtpClient, conn, err := c.openSMTP(ctx)
	if err != nil {
		return fmt.Errorf("Gmail SMTP login failed: %w", err)
	}
	_ = smtpClient.Quit()
	_ = conn.Close()
	return nil
}

func (c *IMAPMailClient) List(ctx context.Context) ([]string, error) {
	since := time.Now().Add(-72 * time.Hour).Format("02-Jan-2006")
	query := "UID SEARCH SINCE " + since
	phrase := strings.TrimSpace(c.cfg.SubjectPhrase)
	if phrase == "" {
		phrase = "new text message from"
	}
	query += " HEADER Subject " + quoteIMAP(phrase)

	var lines []string
	if err := c.withSession(ctx, func(s *imapSession) error {
		var err error
		lines, err = s.command(ctx, query)
		return err
	}); err != nil {
		return nil, err
	}

	var ids []string
	for _, line := range lines {
		if !strings.HasPrefix(strings.ToUpper(line), "* SEARCH") {
			continue
		}
		for _, id := range strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "* SEARCH"))) {
			if _, err := strconv.ParseUint(id, 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) > 2000 {
		ids = ids[len(ids)-2000:]
	}
	// Gmail API returns newest first. Mirror that order so Bridge polling has
	// identical semantics for both Gmail backends.
	sort.Slice(ids, func(i, j int) bool {
		a, _ := strconv.ParseUint(ids[i], 10, 64)
		b, _ := strconv.ParseUint(ids[j], 10, 64)
		return a > b
	})
	return ids, nil
}

var imapLiteralRE = regexp.MustCompile(`\{([0-9]+)\+?\}\s*$`)
var imapInternalDateRE = regexp.MustCompile(`(?i)INTERNALDATE\s+"([^"]+)"`)

func (c *IMAPMailClient) Get(ctx context.Context, id string) (GmailMessage, error) {
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return GmailMessage{}, errors.New("invalid IMAP UID")
	}
	var raw []byte
	var internalDate time.Time
	err := c.withSession(ctx, func(s *imapSession) error {
		raw, internalDate = nil, time.Time{}
		_ = s.conn.SetDeadline(deadlineFor(ctx, 30*time.Second))
		tag := s.nextTag()
		if _, err := fmt.Fprintf(s.conn, "%s UID FETCH %s (INTERNALDATE BODY.PEEK[])\r\n", tag, id); err != nil {
			return err
		}
		for {
			line, err := s.r.ReadString('\n')
			if err != nil {
				return err
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if m := imapInternalDateRE.FindStringSubmatch(trimmed); len(m) == 2 {
				internalDate, _ = time.Parse("02-Jan-2006 15:04:05 -0700", m[1])
			}
			if m := imapLiteralRE.FindStringSubmatch(trimmed); len(m) == 2 {
				n, _ := strconv.Atoi(m[1])
				if n <= 0 || n > 10<<20 {
					return fmt.Errorf("unexpected IMAP message size %d", n)
				}
				raw = make([]byte, n)
				if _, err := io.ReadFull(s.r, raw); err != nil {
					return err
				}
				// Consume the closing FETCH line after the literal.
				if _, err := s.r.ReadString('\n'); err != nil {
					return err
				}
				continue
			}
			if strings.HasPrefix(trimmed, tag+" ") {
				parts := strings.SplitN(trimmed, " ", 3)
				if len(parts) < 2 || !strings.EqualFold(parts[1], "OK") {
					return fmt.Errorf("IMAP UID FETCH failed: %s", trimmed)
				}
				return nil
			}
		}
	})
	if err != nil {
		return GmailMessage{}, err
	}
	if len(raw) == 0 {
		return GmailMessage{}, errors.New("IMAP message contained no body")
	}
	return parseRawGmailMessage(id, raw, "", internalDate)
}

func (c *IMAPMailClient) openSMTP(ctx context.Context) (*smtp.Client, net.Conn, error) {
	if !c.Authorized() {
		return nil, nil, errors.New("Gmail App Password is not configured")
	}
	conn, err := c.dialSMTP(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to Gmail SMTP: %w", err)
	}
	_ = conn.SetDeadline(deadlineFor(ctx, 30*time.Second))
	sc, err := smtp.NewClient(conn, c.smtpServerName)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	auth := smtp.PlainAuth("", c.email, c.password, c.smtpServerName)
	if err := sc.Auth(auth); err != nil {
		_ = sc.Close()
		_ = conn.Close()
		return nil, nil, err
	}
	return sc, conn, nil
}

func (c *IMAPMailClient) dropSMTPLocked() {
	if c.sendCli != nil {
		_ = c.sendCli.Close()
		c.sendCli = nil
	}
	if c.sendConn != nil {
		_ = c.sendConn.Close()
		c.sendConn = nil
	}
}

// sendOnce writes one message over an already-authenticated SMTP client.
func (c *IMAPMailClient) sendOnce(sc *smtp.Client, addr, body string) error {
	if err := sc.Mail(c.email); err != nil {
		return err
	}
	if err := sc.Rcpt(addr); err != nil {
		return err
	}
	w, err := sc.Data()
	if err != nil {
		return err
	}
	msg := "From: " + c.email + "\r\nTo: " + addr + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + body + "\r\n"
	if _, err := io.WriteString(w, msg); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// SendText delivers one SMS by replying to the authenticated Google Voice
// address. An ack, any progress lines, and the result are separate messages, so
// the authenticated SMTP connection is reused across them rather than paying a
// TLS handshake and AUTH per text. A stale connection is retried once.
func (c *IMAPMailClient) SendText(ctx context.Context, to, body string) error {
	addr, err := safeGoogleVoiceReplyAddress(to)
	if err != nil {
		return err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("empty reply")
	}

	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	if sc := c.sendCli; sc != nil {
		if c.sendConn != nil {
			_ = c.sendConn.SetDeadline(deadlineFor(ctx, 30*time.Second))
		}
		if err := sc.Noop(); err == nil {
			if err := c.sendOnce(sc, addr, body); err == nil {
				return nil
			}
			// Leave no half-finished transaction behind on a reused link.
			_ = sc.Reset()
		}
		c.dropSMTPLocked()
	}

	sc, conn, err := c.openSMTP(ctx)
	if err != nil {
		return err
	}
	if err := c.sendOnce(sc, addr, body); err != nil {
		_ = sc.Close()
		_ = conn.Close()
		return err
	}
	c.sendCli, c.sendConn = sc, conn
	return nil
}

func parseRawGmailMessage(id string, data []byte, snippet string, internalDate time.Time) (GmailMessage, error) {
	m, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return GmailMessage{}, err
	}
	body, _ := extractMailBody(m.Header, m.Body)
	dec := new(mime.WordDecoder)
	subject, _ := dec.DecodeHeader(m.Header.Get("Subject"))
	from, _ := dec.DecodeHeader(m.Header.Get("From"))
	replyTo, _ := dec.DecodeHeader(m.Header.Get("Reply-To"))
	authResults := m.Header.Get("Authentication-Results")
	if authResults == "" {
		authResults = m.Header.Get("ARC-Authentication-Results")
	}
	if internalDate.IsZero() {
		internalDate, _ = m.Header.Date()
	}
	if snippet == "" {
		rr := []rune(strings.TrimSpace(body))
		if len(rr) > 180 {
			rr = rr[:180]
		}
		snippet = string(rr)
	}
	return GmailMessage{ID: id, Subject: subject, From: from, ReplyTo: replyTo, AuthenticationResults: authResults, Body: body, Snippet: snippet, InternalDate: internalDate}, nil
}
