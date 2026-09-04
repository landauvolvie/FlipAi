package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const GmailMethodGoogleVoice = "google_voice"

// directGoogleVoiceSMS is a message observed inside FlipAi's own signed-in
// Google Voice WebView. It is written to a small local spool so the existing
// Bridge can consume it through the same MailClient contract as Gmail.
type directGoogleVoiceSMS struct {
	ID     string    `json:"id"`
	Sender string    `json:"sender"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

type GoogleVoiceSMSClient struct {
	dataDir string
	mu      sync.Mutex
}

func NewGoogleVoiceSMSClient(dataDir string) *GoogleVoiceSMSClient {
	return &GoogleVoiceSMSClient{dataDir: dataDir}
}

func (g *GoogleVoiceSMSClient) Authorized() bool {
	return g != nil && strings.TrimSpace(g.dataDir) != ""
}

func (g *GoogleVoiceSMSClient) Test(ctx context.Context) error {
	if g == nil || strings.TrimSpace(g.dataDir) == "" {
		return errors.New("Google Voice SMS is not configured")
	}
	if err := platformOpenGoogleVoice(g.dataDir, false); err != nil {
		// If the host is running before sign-in, the interactive tray may already
		// own the browser. In that case its runtime state is the authoritative
		// answer rather than the host's inability to open a desktop window.
		rt := loadVoiceRuntime(g.dataDir)
		if !rt.BrowserRunning {
			return err
		}
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		rt := loadVoiceRuntime(g.dataDir)
		if rt.BrowserRunning && rt.SignedIn {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			if rt.BrowserRunning {
				return errors.New("Google Voice is open inside FlipAi but is not signed in")
			}
			return errors.New("Google Voice did not become ready inside FlipAi")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func googleVoiceSMSSpoolPath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms.jsonl")
}

func (g *GoogleVoiceSMSClient) readAll() ([]directGoogleVoiceSMS, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, err := os.Open(googleVoiceSMSSpoolPath(g.dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []directGoogleVoiceSMS
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 16*1024), 512*1024)
	for s.Scan() {
		var m directGoogleVoiceSMS
		if json.Unmarshal(s.Bytes(), &m) == nil && m.ID != "" && m.Sender != "" && m.Body != "" {
			out = append(out, m)
		}
	}
	return out, s.Err()
}

func (g *GoogleVoiceSMSClient) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	msgs, err := g.readAll()
	if err != nil {
		return nil, err
	}
	// Gmail returns newest first and Bridge walks the list backwards so older
	// messages execute first. Preserve that ordering for the direct transport.
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].At.After(msgs[j].At) })
	seen := make(map[string]struct{}, len(msgs))
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if _, ok := seen[m.ID]; ok {
			continue
		}
		seen[m.ID] = struct{}{}
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func (g *GoogleVoiceSMSClient) Get(ctx context.Context, id string) (GmailMessage, error) {
	if err := ctx.Err(); err != nil {
		return GmailMessage{}, err
	}
	msgs, err := g.readAll()
	if err != nil {
		return GmailMessage{}, err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.ID != id {
			continue
		}
		// This is an internal trusted envelope: the sender came from the signed-in
		// Google Voice page, not from an email body. Reusing the existing parser
		// keeps the allowlist/security/routing path exactly the same as Gmail.
		phrase := "new text message from"
		if _, cfgPath, _, _, pathErr := appPaths(); pathErr == nil {
			if cfg, cfgErr := loadConfig(cfgPath, g.dataDir); cfgErr == nil {
				if p := strings.TrimSpace(cfg.GoogleVoice.RequiredSubjectPhrase); p != "" {
					phrase = p
				}
			}
		}
		return GmailMessage{
			ID:                    m.ID,
			Subject:               phrase + " " + m.Sender,
			From:                  "Google Voice <voice-noreply@google.com>",
			ReplyTo:               "flipai." + m.Sender + ".direct@txt.voice.google.com",
			AuthenticationResults: "dkim=pass header.d=google.com",
			Body:                  "Google Voice\n" + m.Body,
			Snippet:               m.Body,
			InternalDate:          m.At,
		}, nil
	}
	return GmailMessage{}, fmt.Errorf("Google Voice SMS %q is no longer in the local spool", id)
}

func (g *GoogleVoiceSMSClient) SendText(ctx context.Context, to, body string) error {
	phone := normalizeUSPhone(to)
	if phone == "" {
		if n, ok := senderFromVoiceAddress(to); ok {
			phone = n
		}
	}
	if phone == "" {
		return errors.New("could not determine the Google Voice SMS recipient")
	}
	return requestGoogleVoiceText(ctx, g.dataDir, phone, body)
}

func (g *GoogleVoiceSMSClient) SendReply(ctx context.Context, original GmailMessage, body string) error {
	phone := ""
	for _, candidate := range []string{original.ReplyTo, original.From} {
		if n, ok := senderFromVoiceAddress(candidate); ok {
			phone = n
			break
		}
	}
	if phone == "" {
		return errors.New("could not determine the Google Voice SMS sender")
	}
	return requestGoogleVoiceText(ctx, g.dataDir, phone, body)
}

func directGoogleVoiceSMSID(sender, body string, at time.Time) string {
	bucket := at.UTC().Truncate(time.Second).Format(time.RFC3339)
	sum := sha256.Sum256([]byte(sender + "\x00" + strings.TrimSpace(body) + "\x00" + bucket))
	return "gv-" + hex.EncodeToString(sum[:10])
}

// appendDirectGoogleVoiceSMS is called only by the WebView binding in the
// Google Voice process. It validates the small JSON payload again before it is
// allowed anywhere near the Bridge.
func appendDirectGoogleVoiceSMS(dataDir, payload string) error {
	var m directGoogleVoiceSMS
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return err
	}
	m.Sender = normalizeUSPhone(m.Sender)
	m.Body = strings.TrimSpace(m.Body)
	if m.Sender == "" || m.Body == "" {
		return errors.New("Google Voice SMS payload is missing sender or body")
	}
	if strings.HasPrefix(strings.ToLower(m.Body), "you:") {
		return nil
	}
	if len([]rune(m.Body)) > 12000 {
		return errors.New("Google Voice SMS payload is unexpectedly large")
	}
	if m.At.IsZero() || time.Since(m.At) > 10*time.Minute || time.Until(m.At) > time.Minute {
		m.At = time.Now()
	}
	if strings.TrimSpace(m.ID) == "" {
		m.ID = directGoogleVoiceSMSID(m.Sender, m.Body, m.At)
	} else {
		m.ID = "gv-" + strings.TrimPrefix(strings.TrimSpace(m.ID), "gv-")
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	path := googleVoiceSMSSpoolPath(dataDir)
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
