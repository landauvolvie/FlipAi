from pathlib import Path


def replace_one(path, old, new):
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one match, found {count}")
    p.write_text(text.replace(old, new), encoding="utf-8")


# ---------------------------------------------------------------------------
# Config: keep the current defaults, but make routing tokens configurable.
# ---------------------------------------------------------------------------
replace_one(
    "config.go",
    '''\tTurnTimeoutMinutes int    `json:"turnTimeoutMinutes"`\n\tDefaultAgent       string `json:"defaultAgent"`\n''',
    '''\tTurnTimeoutMinutes int    `json:"turnTimeoutMinutes"`\n\tDefaultAgent       string `json:"defaultAgent"`\n\tCodexPrefix        string `json:"codexPrefix,omitempty"`\n\tClaudePrefix       string `json:"claudePrefix,omitempty"`\n\tNewSessionCommand  string `json:"newSessionCommand,omitempty"`\n''',
)
replace_one(
    "config.go",
    '''\t\tDefaultAgent: "C",\n\t\tGmail:        GmailConfig{CredentialsFile: filepath.Join(dataDir, "google-credentials.json"), PollSeconds: 1, SearchQuery: `subject:"new text message from" newer_than:2d`, SubjectPhrase: "new text message from"},\n''',
    '''\t\tDefaultAgent: "C", CodexPrefix: defaultCodexPrefix, ClaudePrefix: defaultClaudePrefix, NewSessionCommand: defaultNewSessionCommand,\n\t\tGmail:        GmailConfig{CredentialsFile: filepath.Join(dataDir, "google-credentials.json"), PollSeconds: 1, SearchQuery: `subject:"new text message from" newer_than:2d`, SubjectPhrase: "new text message from"},\n''',
)
replace_one(
    "config.go",
    '''\tif cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {\n\t\tcfg.DefaultAgent = "C"\n\t}\n\tif cfg.LocalToken == "" {\n''',
    '''\tif cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {\n\t\tcfg.DefaultAgent = "C"\n\t}\n\tcfg.CodexPrefix = normalizeCommandToken(cfg.CodexPrefix, defaultCodexPrefix)\n\tcfg.ClaudePrefix = normalizeCommandToken(cfg.ClaudePrefix, defaultClaudePrefix)\n\tcfg.NewSessionCommand = normalizeCommandToken(cfg.NewSessionCommand, defaultNewSessionCommand)\n\tif strings.EqualFold(cfg.CodexPrefix, cfg.ClaudePrefix) {\n\t\tcfg.CodexPrefix, cfg.ClaudePrefix = defaultCodexPrefix, defaultClaudePrefix\n\t}\n\tif cfg.LocalToken == "" {\n''',
)

# ---------------------------------------------------------------------------
# Bridge: route using configured prefixes and always use a threaded reply path.
# ---------------------------------------------------------------------------
old_parser = '''func parseRemoteCommand(raw string, cfg Config) (remoteCommand, error) {\n\traw = strings.TrimSpace(raw)\n\tif raw == "" {\n\t\treturn remoteCommand{}, errors.New("empty command")\n\t}\n\trest := raw\n\tif cfg.Security.RequireCode {\n\t\tf := strings.Fields(raw)\n\t\tif len(f) < 2 {\n\t\t\treturn remoteCommand{}, errors.New("missing SMS security code or command")\n\t\t}\n\t\tif !verifySecurityCode(cfg, f[0]) {\n\t\t\treturn remoteCommand{}, errors.New("invalid SMS security code")\n\t\t}\n\t\trest = strings.TrimSpace(strings.TrimPrefix(raw, f[0]))\n\t}\n\tup := strings.ToUpper(rest)\n\tif up == "STATUS" {\n\t\treturn remoteCommand{Status: true}, nil\n\t}\n\tif up == "C NEW" || up == "C: NEW" {\n\t\treturn remoteCommand{Agent: "C", New: true}, nil\n\t}\n\tif up == "A NEW" || up == "A: NEW" {\n\t\treturn remoteCommand{Agent: "A", New: true}, nil\n\t}\n\tagent := cfg.DefaultAgent\n\ttext := rest\n\tif strings.HasPrefix(up, "C:") {\n\t\tagent = "C"\n\t\ttext = strings.TrimSpace(rest[2:])\n\t} else if strings.HasPrefix(up, "A:") {\n\t\tagent = "A"\n\t\ttext = strings.TrimSpace(rest[2:])\n\t}\n\tif agent != "A" && agent != "C" {\n\t\tagent = "C"\n\t}\n\tif text == "" {\n\t\treturn remoteCommand{}, errors.New("empty command")\n\t}\n\treturn remoteCommand{Agent: agent, Text: text}, nil\n}\n'''
new_parser = '''func parseRemoteCommand(raw string, cfg Config) (remoteCommand, error) {\n\traw = strings.TrimSpace(raw)\n\tif raw == "" {\n\t\treturn remoteCommand{}, errors.New("empty command")\n\t}\n\trest := raw\n\tif cfg.Security.RequireCode {\n\t\tf := strings.Fields(raw)\n\t\tif len(f) < 2 {\n\t\t\treturn remoteCommand{}, errors.New("missing SMS security code or command")\n\t\t}\n\t\tif !verifySecurityCode(cfg, f[0]) {\n\t\t\treturn remoteCommand{}, errors.New("invalid SMS security code")\n\t\t}\n\t\trest = strings.TrimSpace(strings.TrimPrefix(raw, f[0]))\n\t}\n\tif strings.EqualFold(rest, "STATUS") {\n\t\treturn remoteCommand{Status: true}, nil\n\t}\n\n\tcodexPrefix := configuredCodexPrefix(cfg)\n\tclaudePrefix := configuredClaudePrefix(cfg)\n\tnewSession := configuredNewSessionCommand(cfg)\n\tdefaultAgent := cfg.DefaultAgent\n\tif defaultAgent != "A" && defaultAgent != "C" {\n\t\tdefaultAgent = "C"\n\t}\n\n\t// The new-session word is configurable too. It works by itself for the\n\t// configured default agent, or after either agent prefix. Existing installs\n\t// keep C/A/NEW because those remain the defaults.\n\tif strings.EqualFold(strings.TrimSpace(rest), newSession) {\n\t\treturn remoteCommand{Agent: defaultAgent, New: true}, nil\n\t}\n\tif isAgentNewSession(rest, codexPrefix, newSession) {\n\t\treturn remoteCommand{Agent: "C", New: true}, nil\n\t}\n\tif isAgentNewSession(rest, claudePrefix, newSession) {\n\t\treturn remoteCommand{Agent: "A", New: true}, nil\n\t}\n\n\tagent := defaultAgent\n\ttext := rest\n\tif tail, ok := stripAgentCommandPrefix(rest, codexPrefix); ok {\n\t\tagent, text = "C", tail\n\t} else if tail, ok := stripAgentCommandPrefix(rest, claudePrefix); ok {\n\t\tagent, text = "A", tail\n\t}\n\tif text == "" {\n\t\treturn remoteCommand{}, errors.New("empty command")\n\t}\n\treturn remoteCommand{Agent: agent, Text: text}, nil\n}\n'''
replace_one("bridge.go", old_parser, new_parser)
replace_one(
    "bridge.go",
    '''\t\tif err := b.gmail.SendText(ctx, target, p); err != nil {''',
    '''\t\tif err := sendThreadedVoiceReply(ctx, b.gmail, m, p); err != nil {''',
)
replace_one(
    "bridge.go",
    '''\tif err := b.gmail.SendText(ctx, target, truncate(line, b.cfg.GoogleVoice.ReplyMaxChars)); err != nil {''',
    '''\tif err := sendThreadedVoiceReply(ctx, b.gmail, m, truncate(line, b.cfg.GoogleVoice.ReplyMaxChars)); err != nil {''',
)

# ---------------------------------------------------------------------------
# UI status and Agents form.
# ---------------------------------------------------------------------------
replace_one(
    "ui_status.go",
    '''\tDefaultAgent     string\n\tDefaultAgentName string\n\tTurnTimeout      int\n''',
    '''\tDefaultAgent      string\n\tDefaultAgentName  string\n\tCodexPrefix       string\n\tClaudePrefix      string\n\tNewSessionCommand string\n\tTurnTimeout       int\n''',
)
replace_one(
    "ui_status.go",
    '''\t\tDefaultAgent:       cfg.DefaultAgent,\n\t\tTurnTimeout:        cfg.TurnTimeoutMinutes,\n''',
    '''\t\tDefaultAgent:       cfg.DefaultAgent,\n\t\tCodexPrefix:        configuredCodexPrefix(cfg),\n\t\tClaudePrefix:       configuredClaudePrefix(cfg),\n\t\tNewSessionCommand:  configuredNewSessionCommand(cfg),\n\t\tTurnTimeout:        cfg.TurnTimeoutMinutes,\n''',
)

replace_one(
    "ui_actions.go",
    '''\t\tif r.Form.Has("defaultAgent") {\n\t\t\tif v := strings.ToUpper(strings.TrimSpace(r.FormValue("defaultAgent"))); v == "A" || v == "C" {\n\t\t\t\tcfg.DefaultAgent = v\n\t\t\t}\n\t\t}\n\t\tif n, ok, err := formInt(r, "turnTimeout", 1, 600); err != nil {\n''',
    '''\t\tif r.Form.Has("defaultAgent") {\n\t\t\tif v := strings.ToUpper(strings.TrimSpace(r.FormValue("defaultAgent"))); v == "A" || v == "C" {\n\t\t\t\tcfg.DefaultAgent = v\n\t\t\t}\n\t\t}\n\t\tif r.Form.Has("codexPrefix") || r.Form.Has("claudePrefix") || r.Form.Has("newSessionCommand") {\n\t\t\tcodexPrefix, claudePrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredNewSessionCommand(*cfg)\n\t\t\tvar err error\n\t\t\tif r.Form.Has("codexPrefix") {\n\t\t\t\tcodexPrefix, err = validateCommandToken(r.FormValue("codexPrefix"), "Codex prefix")\n\t\t\t\tif err != nil {\n\t\t\t\t\treturn err\n\t\t\t\t}\n\t\t\t}\n\t\t\tif r.Form.Has("claudePrefix") {\n\t\t\t\tclaudePrefix, err = validateCommandToken(r.FormValue("claudePrefix"), "Claude prefix")\n\t\t\t\tif err != nil {\n\t\t\t\t\treturn err\n\t\t\t\t}\n\t\t\t}\n\t\t\tif strings.EqualFold(codexPrefix, claudePrefix) {\n\t\t\t\treturn fmt.Errorf("Codex and Claude prefixes must be different")\n\t\t\t}\n\t\t\tif r.Form.Has("newSessionCommand") {\n\t\t\t\tnewSession, err = validateCommandToken(r.FormValue("newSessionCommand"), "new-session command")\n\t\t\t\tif err != nil {\n\t\t\t\t\treturn err\n\t\t\t\t}\n\t\t\t}\n\t\t\tcfg.CodexPrefix, cfg.ClaudePrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, newSession\n\t\t}\n\t\tif n, ok, err := formInt(r, "turnTimeout", 1, 600); err != nil {\n''',
)

replace_one(
    "ui_pages.go",
    '''          <p class="hint">Used when a text has no C: or A: prefix.</p>''',
    '''          <p class="hint">Used when a text has no {{.S.CodexPrefix}}: or {{.S.ClaudePrefix}}: prefix.</p>''',
)
replace_one(
    "ui_pages.go",
    '''      </div>\n      <div class="field">\n        <label for="cwd">Shared working folder</label>''',
    '''      </div>\n      <div class="grid-3">\n        <div class="field">\n          <label for="codexPrefix">Codex SMS prefix</label>\n          <input id="codexPrefix" type="text" name="codexPrefix" value="{{.S.CodexPrefix}}" maxlength="24" required>\n          <p class="hint">Example: <b>{{.S.CodexPrefix}}: check the latest build</b>. Letters or numbers are fine.</p>\n        </div>\n        <div class="field">\n          <label for="claudePrefix">Claude SMS prefix</label>\n          <input id="claudePrefix" type="text" name="claudePrefix" value="{{.S.ClaudePrefix}}" maxlength="24" required>\n          <p class="hint">Example: <b>{{.S.ClaudePrefix}}: review this issue</b>. It must differ from the Codex prefix.</p>\n        </div>\n        <div class="field">\n          <label for="newSessionCommand">New-session command</label>\n          <input id="newSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>\n          <p class="hint">Use <b>{{.S.CodexPrefix}} {{.S.NewSessionCommand}}</b>, <b>{{.S.ClaudePrefix}} {{.S.NewSessionCommand}}</b>, or send it alone for the default agent.</p>\n        </div>\n      </div>\n      <div class="field">\n        <label for="cwd">Shared working folder</label>''',
)
replace_one(
    "ui_pages.go",
    '''\tif agent == "C" {\n\t\treturn "Handles C: messages"\n\t}\n\treturn "Handles A: messages"\n''',
    '''\tif agent == "C" {\n\t\treturn "Handles " + s.CodexPrefix + ": messages"\n\t}\n\treturn "Handles " + s.ClaudePrefix + ": messages"\n''',
)
replace_one(
    "ui_pages.go",
    '''\t\t{Icon: "cpu", Title: "Default agent", Value: s.DefaultAgentName, Tone: "brand", Sub: "Used without a C: or A: prefix",\n''',
    '''\t\t{Icon: "cpu", Title: "Default agent", Value: s.DefaultAgentName, Tone: "brand", Sub: "Used without a " + s.CodexPrefix + ": or " + s.ClaudePrefix + ": prefix",\n''',
)

# ---------------------------------------------------------------------------
# New helper: routing token validation and parsing.
# ---------------------------------------------------------------------------
Path("commands.go").write_text(r'''package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultCodexPrefix       = "C"
	defaultClaudePrefix      = "A"
	defaultNewSessionCommand = "NEW"
)

func cleanCommandToken(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimSpace(strings.TrimSuffix(v, ":"))
	return v
}

func validateCommandToken(v, label string) (string, error) {
	v = cleanCommandToken(v)
	if v == "" {
		return "", fmt.Errorf("%s cannot be empty", label)
	}
	if utf8.RuneCountInString(v) > 24 {
		return "", fmt.Errorf("%s must be 24 characters or fewer", label)
	}
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-.", r) {
			continue
		}
		return "", fmt.Errorf("%s may contain only letters, numbers, underscore, dash, or period", label)
	}
	return v, nil
}

func normalizeCommandToken(v, fallback string) string {
	if token, err := validateCommandToken(v, "command"); err == nil {
		return token
	}
	return fallback
}

func configuredCodexPrefix(cfg Config) string {
	return normalizeCommandToken(cfg.CodexPrefix, defaultCodexPrefix)
}

func configuredClaudePrefix(cfg Config) string {
	return normalizeCommandToken(cfg.ClaudePrefix, defaultClaudePrefix)
}

func configuredNewSessionCommand(cfg Config) string {
	return normalizeCommandToken(cfg.NewSessionCommand, defaultNewSessionCommand)
}

// stripAgentCommandPrefix recognizes the normal "PREFIX: text" routing form.
func stripAgentCommandPrefix(raw, prefix string) (string, bool) {
	raw = strings.TrimSpace(raw)
	i := strings.Index(raw, ":")
	if i <= 0 || !strings.EqualFold(strings.TrimSpace(raw[:i]), prefix) {
		return "", false
	}
	return strings.TrimSpace(raw[i+1:]), true
}

// isAgentNewSession keeps the historical "C NEW" and "C: NEW" forms while
// allowing both the prefix and NEW word to be customized.
func isAgentNewSession(raw, prefix, newSession string) bool {
	if tail, ok := stripAgentCommandPrefix(raw, prefix); ok {
		return strings.EqualFold(strings.TrimSpace(tail), newSession)
	}
	fields := strings.Fields(strings.TrimSpace(raw))
	return len(fields) == 2 && strings.EqualFold(fields[0], prefix) && strings.EqualFold(fields[1], newSession)
}
''', encoding="utf-8")

# ---------------------------------------------------------------------------
# New helper: real email replies to the original Google Voice notification.
# OAuth uses Gmail threadId + RFC reply headers. App Password/SMTP uses the
# same RFC headers. No production path falls back to a standalone email.
# ---------------------------------------------------------------------------
Path("threaded_reply.go").write_text(r'''package main

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

func sendThreadedVoiceReply(ctx context.Context, client MailClient, original GmailMessage, body string) error {
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
	body = strings.TrimSpace(body)
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
''', encoding="utf-8")

# ---------------------------------------------------------------------------
# Tests for configurable routing and RFC reply headers.
# ---------------------------------------------------------------------------
Path("commands_test.go").write_text(r'''package main

import "testing"

func TestParseRemoteCommandConfigurablePrefixes(t *testing.T) {
	cfg := Config{DefaultAgent: "A", CodexPrefix: "1", ClaudePrefix: "Z9", NewSessionCommand: "FRESH"}
	cases := []struct {
		raw    string
		agent  string
		text   string
		newRun bool
	}{
		{"1: inspect logs", "C", "inspect logs", false},
		{"z9: review issue", "A", "review issue", false},
		{"1 FRESH", "C", "", true},
		{"Z9: fresh", "A", "", true},
		{"fresh", "A", "", true},
		{"no prefix here", "A", "no prefix here", false},
	}
	for _, tc := range cases {
		rc, err := parseRemoteCommand(tc.raw, cfg)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		if rc.Agent != tc.agent || rc.Text != tc.text || rc.New != tc.newRun {
			t.Fatalf("parse %q = %+v, want agent=%q text=%q new=%v", tc.raw, rc, tc.agent, tc.text, tc.newRun)
		}
	}
}

func TestParseRemoteCommandLegacyDefaultsRemain(t *testing.T) {
	cfg := Config{DefaultAgent: "C"}
	rc, err := parseRemoteCommand("A: hello", cfg)
	if err != nil || rc.Agent != "A" || rc.Text != "hello" {
		t.Fatalf("legacy A prefix: rc=%+v err=%v", rc, err)
	}
	rc, err = parseRemoteCommand("C NEW", cfg)
	if err != nil || rc.Agent != "C" || !rc.New {
		t.Fatalf("legacy C NEW: rc=%+v err=%v", rc, err)
	}
}

func TestValidateCommandToken(t *testing.T) {
	if got, err := validateCommandToken(" 7: ", "prefix"); err != nil || got != "7" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := validateCommandToken("two words", "prefix"); err == nil {
		t.Fatal("expected spaces to be rejected")
	}
}
''', encoding="utf-8")

Path("threaded_reply_test.go").write_text(r'''package main

import (
	"strings"
	"testing"
)

func TestBuildThreadedReplyMessageUsesOriginalThreadHeaders(t *testing.T) {
	meta := replyThreadMeta{
		Subject:    "New text message from (845) 555-1212",
		MessageID:  "<original@google.com>",
		References: "<earlier@google.com>",
	}
	raw, err := buildThreadedReplyMessage("me@gmail.com", "abc@txt.voice.google.com", meta, "Claude finished")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Subject: New text message from (845) 555-1212\r\n",
		"In-Reply-To: <original@google.com>\r\n",
		"References: <earlier@google.com> <original@google.com>\r\n",
		"To: abc@txt.voice.google.com\r\n",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("threaded reply missing %q:\n%s", want, raw)
		}
	}
}

func TestBuildThreadedReplyRefusesStandaloneFallback(t *testing.T) {
	_, err := buildThreadedReplyMessage("", "abc@txt.voice.google.com", replyThreadMeta{Subject: "Voice"}, "done")
	if err == nil || !strings.Contains(err.Error(), "Message-ID") {
		t.Fatalf("expected missing Message-ID error, got %v", err)
	}
}
''', encoding="utf-8")
