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
// Google Voice SMS WebView. Sender is always a normalized 10-digit phone number
// before an authorized message is written to the spool. Thread is the exact
// Google Voice Messages path observed on that same conversation row; replies
// fail closed if that identity can no longer be verified.
type directGoogleVoiceSMS struct {
	ID     string    `json:"id"`
	Sender string    `json:"sender"`
	Thread string    `json:"thread,omitempty"`
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
	// Direct SMS owns a completely separate browser/profile from calling. The
	// old test checked the call browser, which is how v0.46.34 could report a
	// healthy SMS connection while the SMS renderer itself was dead.
	if err := platformEnsureGoogleVoiceSMSWorker(g.dataDir); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		s := loadGoogleVoiceSMSRuntime(g.dataDir)
		fresh := !s.LastProbeAt.IsZero() && time.Since(s.LastProbeAt) < 8*time.Second
		if s.Running && s.Connected && s.SignedIn && s.ListenerRunning && s.Ready && fresh {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			if s.LastError != "" {
				return errors.New(s.LastError)
			}
			if !s.Connected || !s.SignedIn {
				return errors.New("Google Voice SMS is not signed in; open Connections, press Connect under Google Voice SMS, and sign in in the window FlipAi opens")
			}
			return errors.New("Google Voice SMS is signed in but its Messages listener is not ready")
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

// normalizeGoogleVoiceSMSThread accepts only a same-site Messages path. Never
// preserve a contact name or arbitrary URL as a reply locator.
func normalizeGoogleVoiceSMSThread(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	const origin = "https://voice.google.com"
	if strings.HasPrefix(lower, origin+"/") {
		raw = raw[len(origin):]
	} else if strings.Contains(lower, "://") {
		return ""
	}
	if !strings.HasPrefix(raw, "/") || strings.ContainsAny(raw, "\\\r\n\t") || strings.Contains(raw, "..") {
		return ""
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	if !strings.Contains(strings.ToLower(raw), "/messages/") {
		return ""
	}
	return raw
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
			m.Sender = normalizeUSPhone(m.Sender)
			m.Thread = normalizeGoogleVoiceSMSThread(m.Thread)
			if m.Sender != "" {
				out = append(out, m)
			}
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
		// This is an internal trusted envelope: the sender was normalized and
		// authorized from Google Voice row identity metadata before it reached the
		// spool. Reusing the existing parser keeps security-code/routing behavior
		// identical to Gmail after this transport-specific gate.
		phrase := "new text message from"
		if cfg, cfgErr := loadConfig(filepath.Join(g.dataDir, "bridge.json"), g.dataDir); cfgErr == nil {
			if p := strings.TrimSpace(cfg.GoogleVoice.RequiredSubjectPhrase); p != "" {
				phrase = p
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
	// One-off sends have no inbound thread to bind to; the page sender therefore
	// accepts only an exact phone-number suggestion and refuses ambiguity.
	return requestGoogleVoiceText(ctx, g.dataDir, phone, body)
}

// directReplyIdentity binds a reply to both identities captured from the inbound
// event: the structured sender envelope and the exact Google Voice thread path.
// A mismatch is a hard failure; FlipAi never falls back to a contact name.
func (g *GoogleVoiceSMSClient) directReplyIdentity(original GmailMessage) (phone, thread string, err error) {
	for _, candidate := range []string{original.ReplyTo, original.From} {
		if n, ok := senderFromVoiceAddress(candidate); ok {
			phone = n
			break
		}
	}
	if phone == "" {
		return "", "", errors.New("could not determine the Google Voice SMS sender")
	}
	msgs, err := g.readAll()
	if err != nil {
		return "", "", err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.ID != original.ID {
			continue
		}
		if normalizeUSPhone(m.Sender) != phone {
			return "", "", errors.New("Google Voice reply blocked: inbound phone number does not match its stored conversation")
		}
		thread = normalizeGoogleVoiceSMSThread(m.Thread)
		if thread == "" {
			return "", "", errors.New("Google Voice reply blocked: exact inbound conversation thread is unavailable")
		}
		return phone, thread, nil
	}
	return "", "", errors.New("Google Voice reply blocked: original inbound message is not in the trusted direct-SMS spool")
}

func (g *GoogleVoiceSMSClient) SendReply(ctx context.Context, original GmailMessage, body string) error {
	phone, thread, err := g.directReplyIdentity(original)
	if err != nil {
		return err
	}
	return requestGoogleVoiceTextThread(ctx, g.dataDir, phone, thread, body)
}

func directGoogleVoiceSMSID(sender, body string, at time.Time) string {
	bucket := at.UTC().Truncate(time.Second).Format(time.RFC3339)
	sum := sha256.Sum256([]byte(sender + "\x00" + strings.TrimSpace(body) + "\x00" + bucket))
	return "gv-" + hex.EncodeToString(sum[:10])
}

func directGoogleVoiceActivity(dataDir string) *ActivityLog {
	return activityLogForStatePath(filepath.Join(dataDir, "state.json"))
}

// appendDirectGoogleVoiceSMS is called by the dedicated SMS WebView binding. A
// pre-v0.46.35 fallback binding still exists in the untouched calling window;
// ignore it there so the call process can never become a second SMS reader.
//
// This is also the first security gate. Every inbound DOM event is logged, then
// authorization is decided exclusively from the normalized phone number. A
// blocked/unresolved sender never reaches the spool, queue, agent, or reply path.
func appendDirectGoogleVoiceSMS(dataDir, payload string) error {
	if len(os.Args) > 1 && strings.EqualFold(os.Args[1], "--google-voice") {
		return nil
	}
	var m directGoogleVoiceSMS
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return err
	}
	m.Sender = normalizeUSPhone(m.Sender)
	m.Thread = normalizeGoogleVoiceSMSThread(m.Thread)
	m.Body = strings.TrimSpace(m.Body)
	if m.Body == "" {
		return nil
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
	idSender := m.Sender
	if idSender == "" {
		idSender = "unresolved"
	}
	if strings.TrimSpace(m.ID) == "" {
		m.ID = directGoogleVoiceSMSID(idSender, m.Body, m.At)
	} else {
		m.ID = "gv-" + strings.TrimPrefix(strings.TrimSpace(m.ID), "gv-")
	}

	activity := directGoogleVoiceActivity(dataDir)
	activity.Add("info", "google_voice", "Google Voice SMS received", m.Sender, "", m.ID)

	if m.Sender == "" {
		activity.Add("warn", "security", "Blocked Google Voice SMS: exact sender phone number could not be resolved; no reply sent", "", "", m.ID)
		return nil
	}
	if m.Thread == "" {
		activity.Add("warn", "security", "Blocked Google Voice SMS: exact conversation thread could not be verified; no reply sent", m.Sender, "", m.ID)
		return nil
	}

	cfg, err := loadConfig(filepath.Join(dataDir, "bridge.json"), dataDir)
	if err != nil {
		activity.Add("error", "security", "Blocked Google Voice SMS: permissions could not be loaded; no reply sent", m.Sender, "", m.ID)
		return nil
	}
	if cfg.Gmail.Method != GmailMethodGoogleVoice {
		activity.Add("info", "google_voice", "Google Voice SMS ignored because direct Google Voice is not the selected text transport", m.Sender, "", m.ID)
		return nil
	}
	agent, phone, found := agentForSender(cfg, m.Sender)
	if !found {
		activity.Add("warn", "security", "Blocked Google Voice SMS: phone number is not allowed; no reply sent", m.Sender, "", m.ID)
		return nil
	}
	if !phone.AllowsSMS() {
		activity.Add("warn", "security", "Blocked Google Voice SMS: phone number is allowed for calls only; no reply sent", m.Sender, "", m.ID)
		return nil
	}
	// Keep the legacy aggregate allowlist as a second independent assertion.
	// loadConfig rebuilds it from the per-agent phone permissions, so disagreement
	// means configuration is inconsistent and the safe action is to refuse.
	if !allowedPhone(cfg.GoogleVoice.AllowedFrom, m.Sender) {
		activity.Add("warn", "security", "Blocked Google Voice SMS: phone-number permission check did not agree; no reply sent", m.Sender, "", m.ID)
		return nil
	}
	activity.Add("success", "security", "Google Voice SMS phone number verified and allowed", m.Sender, agent, m.ID)

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
