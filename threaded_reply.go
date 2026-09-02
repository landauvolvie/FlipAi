package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type threadedReplySender interface {
	SendReply(context.Context, GmailMessage, string) error
}

type replyThreadMeta struct {
	ThreadID   string
	Subject    string
	MessageID  string
	References string
}

type gmailReplyCacheKey struct {
	Client *GmailClient
	ID     string
}

type imapReplyCacheKey struct {
	Client *IMAPMailClient
	ID     string
}

var gmailReplyMetaCache sync.Map
var imapReplyMetaCache sync.Map

// splitReply numbers a long agent answer before it reaches this delivery
// layer. That was useful when each chunk was sent as a separate email, but the
// Google Voice email reply gateway only forwards the first logical line of an
// email body. A multi-line answer therefore arrived as tiny fragments such as
// "1/2 Today's notable Gmail:" and "2/2 on the way." while the middle of the
// answer disappeared.
//
// Keep the old splitter for compatibility with the bridge, but join its parts
// back together here before anything leaves FlipAi. The map is keyed by the
// original inbound Google Voice email, so simultaneous conversations cannot
// contaminate one another.
type voiceReplyAssembly struct {
	total int
	parts map[int]string
}

var voiceReplyAssemblyMu sync.Mutex
var voiceReplyAssemblies = map[string]*voiceReplyAssembly{}

func parseNumberedVoiceReply(body string) (part, total int, piece string, ok bool) {
	body = strings.TrimSpace(body)
	space := strings.IndexByte(body, ' ')
	if space <= 0 {
		return 0, 0, "", false
	}
	prefix := body[:space]
	slash := strings.IndexByte(prefix, '/')
	if slash <= 0 || slash == len(prefix)-1 {
		return 0, 0, "", false
	}
	part, errPart := strconv.Atoi(prefix[:slash])
	total, errTotal := strconv.Atoi(prefix[slash+1:])
	if errPart != nil || errTotal != nil || total < 2 || total > 10 || part < 1 || part > total {
		return 0, 0, "", false
	}
	return part, total, strings.TrimSpace(body[space+1:]), true
}

func assembleNumberedVoiceReply(original GmailMessage, body string) (string, bool) {
	part, total, piece, numbered := parseNumberedVoiceReply(body)
	if !numbered {
		return body, true
	}
	key := strings.TrimSpace(original.ID)
	if key == "" {
		// Without a stable message id there is no safe way to associate later
		// parts, so prefer delivering the text over accidentally swallowing it.
		return body, true
	}

	voiceReplyAssemblyMu.Lock()
	defer voiceReplyAssemblyMu.Unlock()

	assembly := voiceReplyAssemblies[key]
	if part == 1 {
		// A normal short answer could itself begin with text such as "1/2 cup".
		// A real first chunk produced by the bridge's default 300-character
		// splitter is much longer, so do not buffer a tiny natural-language line.
		if len([]rune(body)) < 120 {
			return body, true
		}
		assembly = &voiceReplyAssembly{total: total, parts: make(map[int]string, total)}
		voiceReplyAssemblies[key] = assembly
	} else if assembly == nil {
		// An unexpected "2/2 ..." should still be delivered rather than held
		// forever. Only a first part is allowed to start an assembly.
		return body, true
	}
	if assembly.total != total {
		delete(voiceReplyAssemblies, key)
		return body, true
	}
	assembly.parts[part] = piece
	if len(assembly.parts) < total {
		return "", false
	}

	pieces := make([]string, 0, total)
	for i := 1; i <= total; i++ {
		p, exists := assembly.parts[i]
		if !exists {
			return "", false
		}
		pieces = append(pieces, p)
	}
	delete(voiceReplyAssemblies, key)
	return strings.TrimSpace(strings.Join(pieces, " ")), true
}

// Google Voice's @txt.voice.google.com reply gateway treats a newline as the
// end of the SMS text. Flatten formatting only at the transport boundary so a
// complete multi-line ChatGPT answer becomes one logical outbound message.
// Bullets and punctuation remain in the text; only whitespace is collapsed.
func googleVoiceGatewayBody(body string) string {
	return strings.Join(strings.Fields(body), " ")
}

func sendThreadedVoiceReply(ctx context.Context, client MailClient, original GmailMessage, body string) error {
	assembled, ready := assembleNumberedVoiceReply(original, body)
	if !ready {
		return nil
	}
	body = googleVoiceGatewayBody(assembled)
	if body == "" {
		return errors.New("empty Google Voice reply")
	}
	if sender, ok := client.(threadedReplySender); ok {
		return sender.SendReply(ctx, original, body)
	}
	// Test doubles and third-party implementations keep the old interface. The
	// built-in Gmail and IMAP clients both implement SendReply, so production
	// Google Voice delivery never uses this compatibility fallback.
	target := googleVoiceReplyTarget(original)
	if target == "" {
		return errors.New("could not find a safe Google Voice reply address")
	}
	return client.SendText(ctx, target, body)
}

// routeGeneratedImageReply sends an already-generated Codex image through the
// real Google Voice web UI. Google Voice's email reply gateway accepts text but
// silently drops image attachments, so image replies must bypass Gmail. The
// image is marked consumed after one UI attempt so a split SMS reply never
// retries the same picture on every text part. If the UI attempt fails, the
// caller receives the text reply over the normal Gmail path plus a short notice
// that the image itself could not be delivered.
func routeGeneratedImageReply(ctx context.Context, original GmailMessage, body string) (handled bool, fallbackBody string) {
	image, imageKey := generatedImageForVoiceReplyResolved(original, body)
	if image == nil {
		// Image-generation requests used to fail silently here: the user received
		// the agent's caption, which made it look as though Google Voice had lost
		// an MMS even when FlipAi had never obtained an image asset. Keep progress
		// heartbeats quiet, but make a final extraction failure explicit so the
		// next live test identifies the failing layer without opening logs.
		if !isTransientVoiceReply(body) && looksLikeImageGenerationRequest(original.Body) {
			fallbackBody = strings.TrimSpace(body)
			if fallbackBody == "" {
				fallbackBody = "I generated the image."
			}
			fallbackBody += "\n\nFlipAi could not locate the generated image asset."
			return false, fallbackBody
		}
		return false, body
	}
	err := sendGoogleVoiceImageMMS(ctx, original, body, image)
	markGeneratedImageDelivered(imageKey)
	if err == nil {
		return true, body
	}
	fallbackBody = strings.TrimSpace(body)
	if fallbackBody == "" {
		fallbackBody = "I generated the image."
	}
	fallbackBody += "\n\nFlipAi could not deliver the image through Google Voice MMS."
	return false, fallbackBody
}

func cleanReplyHeader(v string) string {
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	return strings.TrimSpace(v)
}

func appendReplyReference(references, messageID string) string {
	references, messageID = cleanReplyHeader(references), cleanReplyHeader(messageID)
	if references == "" {
		return messageID
	}
	if messageID == "" || strings.Contains(references, messageID) {
		return references
	}
	return references + " " + messageID
}

func buildThreadedReplyMessage(from, to string, meta replyThreadMeta, body string) (string, error) {
	addr, err := safeGoogleVoiceReplyAddress(to)
	if err != nil {
		return "", err
	}
	// The gateway stops at the first newline. Normalize again here because the
	// image-delivery fallback can append a notice after sendThreadedVoiceReply
	// has already done its transport normalization.
	body = googleVoiceGatewayBody(body)
	if body == "" {
		return "", errors.New("empty reply")
	}
	messageID := cleanReplyHeader(meta.MessageID)
	if messageID == "" {
		return "", errors.New("original Google Voice email has no Message-ID; refusing standalone reply")
	}
	subject := cleanReplyHeader(meta.Subject)
	if subject == "" {
		return "", errors.New("original Google Voice email has no subject; refusing standalone reply")
	}
	references := appendReplyReference(meta.References, messageID)

	var b strings.Builder
	if from = cleanReplyHeader(from); from != "" {
		b.WriteString("From: " + from + "\r\n")
	}
	b.WriteString("To: " + addr + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("In-Reply-To: " + messageID + "\r\n")
	b.WriteString("References: " + references + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")
	return b.String(), nil
}

type gmailMetadataResponse struct {
	ThreadID string `json:"threadId"`
	Payload  struct {
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
	} `json:"payload"`
}

func gmailMetadataHeader(m gmailMetadataResponse, name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func (g *GmailClient) replyThreadMeta(ctx context.Context, original GmailMessage) (replyThreadMeta, error) {
	key := gmailReplyCacheKey{Client: g, ID: original.ID}
	if cached, ok := gmailReplyMetaCache.Load(key); ok {
		return cached.(replyThreadMeta), nil
	}
	q := url.Values{}
	q.Set("format", "metadata")
	q.Add("metadataHeaders", "Subject")
	q.Add("metadataHeaders", "Message-ID")
	q.Add("metadataHeaders", "References")
	var raw gmailMetadataResponse
	u := g.apiBase + "/users/me/messages/" + url.PathEscape(original.ID) + "?" + q.Encode()
	if err := g.apiGET(ctx, u, &raw); err != nil {
		return replyThreadMeta{}, fmt.Errorf("load original Gmail thread metadata: %w", err)
	}
	meta := replyThreadMeta{
		ThreadID:   strings.TrimSpace(raw.ThreadID),
		Subject:    gmailMetadataHeader(raw, "Subject"),
		MessageID:  gmailMetadataHeader(raw, "Message-ID"),
		References: gmailMetadataHeader(raw, "References"),
	}
	if meta.Subject == "" {
		meta.Subject = original.Subject
	}
	if meta.ThreadID == "" {
		return replyThreadMeta{}, errors.New("original Gmail message has no threadId; refusing standalone reply")
	}
	if cleanReplyHeader(meta.MessageID) == "" {
		return replyThreadMeta{}, errors.New("original Gmail message has no Message-ID; refusing standalone reply")
	}
	gmailReplyMetaCache.Store(key, meta)
	return meta, nil
}

func (g *GmailClient) SendReply(ctx context.Context, original GmailMessage, body string) error {
	target := googleVoiceReplyTarget(original)
	if target == "" {
		return errors.New("could not find a safe Google Voice reply address")
	}
	if handled, fallback := routeGeneratedImageReply(ctx, original, body); handled {
		return nil
	} else {
		body = fallback
	}
	meta, err := g.replyThreadMeta(ctx, original)
	if err != nil {
		return err
	}
	rawMessage, err := buildThreadedReplyMessage("", target, meta, body)
	if err != nil {
		return err
	}
	tok, err := g.accessToken(ctx)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(struct {
		Raw      string `json:"raw"`
		ThreadID string `json:"threadId"`
	}{Raw: base64.RawURLEncoding.EncodeToString([]byte(rawMessage)), ThreadID: meta.ThreadID})
	req, _ := http.NewRequestWithContext(ctx, "POST", g.apiBase+"/users/me/messages/send", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("Gmail threaded reply HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *IMAPMailClient) replyThreadMeta(ctx context.Context, original GmailMessage) (replyThreadMeta, error) {
	key := imapReplyCacheKey{Client: c, ID: original.ID}
	if cached, ok := imapReplyMetaCache.Load(key); ok {
		return cached.(replyThreadMeta), nil
	}
	if _, err := strconv.ParseUint(original.ID, 10, 64); err != nil {
		return replyThreadMeta{}, errors.New("invalid IMAP UID")
	}
	var raw []byte
	err := c.withSession(ctx, func(s *imapSession) error {
		_ = s.conn.SetDeadline(deadlineFor(ctx, 30_000_000_000))
		tag := s.nextTag()
		if _, err := fmt.Fprintf(s.conn, "%s UID FETCH %s (BODY.PEEK[HEADER.FIELDS (SUBJECT MESSAGE-ID REFERENCES)])\r\n", tag, original.ID); err != nil {
			return err
		}
		for {
			line, err := s.r.ReadString('\n')
			if err != nil {
				return err
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if m := imapLiteralRE.FindStringSubmatch(trimmed); len(m) == 2 {
				n, _ := strconv.Atoi(m[1])
				if n <= 0 || n > 1<<20 {
					return fmt.Errorf("unexpected IMAP header size %d", n)
				}
				raw = make([]byte, n)
				if _, err := io.ReadFull(s.r, raw); err != nil {
					return err
				}
				if _, err := s.r.ReadString('\n'); err != nil {
					return err
				}
				continue
			}
			if strings.HasPrefix(trimmed, tag+" ") {
				parts := strings.SplitN(trimmed, " ", 3)
				if len(parts) < 2 || !strings.EqualFold(parts[1], "OK") {
					return fmt.Errorf("IMAP UID FETCH headers failed: %s", trimmed)
				}
				return nil
			}
		}
	})
	if err != nil {
		return replyThreadMeta{}, err
	}
	if len(raw) == 0 {
		return replyThreadMeta{}, errors.New("IMAP message contained no reply headers")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return replyThreadMeta{}, fmt.Errorf("parse original reply headers: %w", err)
	}
	meta := replyThreadMeta{
		Subject:    original.Subject,
		MessageID:  msg.Header.Get("Message-ID"),
		References: msg.Header.Get("References"),
	}
	if meta.Subject == "" {
		meta.Subject = msg.Header.Get("Subject")
	}
	if cleanReplyHeader(meta.MessageID) == "" {
		return replyThreadMeta{}, errors.New("original IMAP message has no Message-ID; refusing standalone reply")
	}
	imapReplyMetaCache.Store(key, meta)
	return meta, nil
}

func (c *IMAPMailClient) sendThreadedOnce(sc *smtp.Client, addr, rawMessage string) error {
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
	if _, err := io.WriteString(w, rawMessage); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (c *IMAPMailClient) SendReply(ctx context.Context, original GmailMessage, body string) error {
	target := googleVoiceReplyTarget(original)
	if target == "" {
		return errors.New("could not find a safe Google Voice reply address")
	}
	if handled, fallback := routeGeneratedImageReply(ctx, original, body); handled {
		return nil
	} else {
		body = fallback
	}
	meta, err := c.replyThreadMeta(ctx, original)
	if err != nil {
		return err
	}
	rawMessage, err := buildThreadedReplyMessage(c.email, target, meta, body)
	if err != nil {
		return err
	}
	addr, err := safeGoogleVoiceReplyAddress(target)
	if err != nil {
		return err
	}

	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if sc := c.sendCli; sc != nil {
		if c.sendConn != nil {
			_ = c.sendConn.SetDeadline(deadlineFor(ctx, 30_000_000_000))
		}
		if err := sc.Noop(); err == nil {
			if err := c.sendThreadedOnce(sc, addr, rawMessage); err == nil {
				return nil
			}
			_ = sc.Reset()
		}
		c.dropSMTPLocked()
	}

	sc, conn, err := c.openSMTP(ctx)
	if err != nil {
		return err
	}
	if err := c.sendThreadedOnce(sc, addr, rawMessage); err != nil {
		_ = sc.Close()
		_ = conn.Close()
		return err
	}
	c.sendCli, c.sendConn = sc, conn
	return nil
}
