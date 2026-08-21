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
	s, err := c.openIMAP(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()
	since := time.Now().Add(-72 * time.Hour).Format("02-Jan-2006")
	query := "UID SEARCH SINCE " + since
	phrase := strings.TrimSpace(c.cfg.SubjectPhrase)
	if phrase == "" {
		phrase = "new text message from"
	}
	query += " HEADER Subject " + quoteIMAP(phrase)
	lines, err := s.command(ctx, query)
	if err != nil {
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
	s, err := c.openIMAP(ctx)
	if err != nil {
		return GmailMessage{}, err
	}
	defer s.close()
	_ = s.conn.SetDeadline(deadlineFor(ctx, 30*time.Second))
	tag := s.nextTag()
	if _, err := fmt.Fprintf(s.conn, "%s UID FETCH %s (INTERNALDATE BODY.PEEK[])\r\n", tag, id); err != nil {
		return GmailMessage{}, err
	}
	var raw []byte
	var internalDate time.Time
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return GmailMessage{}, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if m := imapInternalDateRE.FindStringSubmatch(trimmed); len(m) == 2 {
			internalDate, _ = time.Parse("02-Jan-2006 15:04:05 -0700", m[1])
		}
		if m := imapLiteralRE.FindStringSubmatch(trimmed); len(m) == 2 {
			n, _ := strconv.Atoi(m[1])
			if n <= 0 || n > 10<<20 {
				return GmailMessage{}, fmt.Errorf("unexpected IMAP message size %d", n)
			}
			raw = make([]byte, n)
			if _, err := io.ReadFull(s.r, raw); err != nil {
				return GmailMessage{}, err
			}
			// Consume the closing FETCH line after the literal.
			if _, err := s.r.ReadString('\n'); err != nil {
				return GmailMessage{}, err
			}
			continue
		}
		if strings.HasPrefix(trimmed, tag+" ") {
			parts := strings.SplitN(trimmed, " ", 3)
			if len(parts) < 2 || !strings.EqualFold(parts[1], "OK") {
				return GmailMessage{}, fmt.Errorf("IMAP UID FETCH failed: %s", trimmed)
			}
			break
		}
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

func (c *IMAPMailClient) SendText(ctx context.Context, to, body string) error {
	addr, err := safeGoogleVoiceReplyAddress(to)
	if err != nil {
		return err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("empty reply")
	}
	sc, conn, err := c.openSMTP(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer sc.Close()
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
	if err := w.Close(); err != nil {
		return err
	}
	return sc.Quit()
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
