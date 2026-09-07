package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
// Google Voice Messages locator observed on that same conversation row; replies
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
	if err := platformEnsureGoogleVoiceSMSWorker(g.dataDir); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		s := loadGoogleVoiceSMSRuntime(g.dataDir)
		if googleVoiceSMSConnected(s) {
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

func asciiDigitsOnly(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func googleVoiceSMSItemPhone(item string) string {
	const prefix = "t.+1"
	if !strings.HasPrefix(item, prefix) {
		return ""
	}
	phone := strings.TrimPrefix(item, prefix)
	if len(phone) != 10 || !asciiDigitsOnly(phone) {
		return ""
	}
	return phone
}

// normalizeGoogleVoiceSMSThread accepts only a same-site Google Voice Messages
// conversation locator. Current Google Voice identifies ordinary 1:1 threads
// as /u/N/messages?itemId=t.%2B1XXXXXXXXXX. The itemId is preserved because it
// is both the exact reply target and an independent phone-number identity check.
// A legacy /u/N/messages/<id> path remains accepted for old spool entries.
func normalizeGoogleVoiceSMSThread(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\\\r\n\t") || strings.Contains(raw, "..") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		if !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Host, "voice.google.com") {
			return ""
		}
	} else if u.Host != "" {
		return ""
	}
	if u.Fragment != "" || !strings.HasPrefix(u.Path, "/") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "u" || !asciiDigitsOnly(parts[1]) || parts[2] != "messages" {
		return ""
	}
	if len(parts) == 3 {
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q) != 1 {
			return ""
		}
		items, ok := q["itemId"]
		if !ok || len(items) != 1 || googleVoiceSMSItemPhone(items[0]) == "" {
			return ""
		}
		return "/u/" + parts[1] + "/messages?itemId=" + url.QueryEscape(items[0])
	}
	if len(parts) != 4 || strings.TrimSpace(parts[3]) == "" || u.RawQuery != "" {
		return ""
	}
	return "/u/" + parts[1] + "/messages/" + parts[3]
}

func googleVoiceSMSThreadPhone(thread string) string {
	thread = normalizeGoogleVoiceSMSThread(thread)
	if thread == "" {
		return ""
	}
	u, err := url.Parse(thread)
	if err != nil {
		return ""
	}
	return googleVoiceSMSItemPhone(u.Query().Get("itemId"))
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
	return requestGoogleVoiceText(ctx, g.dataDir, phone, body)
}

// directReplyIdentity binds a reply to both identities captured from the inbound
// event: the structured sender envelope and the exact Google Voice thread. When
// the thread itself carries an itemId phone, all three identities must agree.
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
		if threadPhone := googleVoiceSMSThreadPhone(thread); threadPhone != "" && threadPhone != phone {
			return "", "", errors.New("Google Voice reply blocked: conversation itemId phone does not match the inbound sender")
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
	// Ack/progress texts share this same exact-thread sender. Never let one of
	// those transient updates consume a generated browser image that arrived a
	// few milliseconds before the final answer; the final reply owns the media
	// slot and sends it as MMS.
	if !isTransientVoiceReply(body) {
		if media := takeCapturedBrowserChatReturnedMedia(); media != nil {
			fallbackBody, image := prepareBrowserChatReturnedMediaReply(body, media)
			if image != nil {
				// Use FlipAi's normal hidden Google Voice image-send surface. If
				// Google Voice rejects the asset or this install has no usable image
				// sender session, keep the text reply and give the user the exact
				// browser conversation instead of dropping the generated media.
				if err := sendGoogleVoiceImageMMS(ctx, original, body, image); err == nil {
					return nil
				}
				body = appendBrowserMediaConversationLink(body, media.ConversationURL)
			} else {
				body = fallbackBody
			}
		}
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

// appendDirectGoogleVoiceSMS is the first security gate. Every inbound event is
// logged, then authorization is decided exclusively from the normalized phone
// number -- never from a contact name, and never from a number written inside
// the message text.
//
// The conversation's own t.+1XXXXXXXXXX identity is the sender. That number is
// also the exact address the reply is delivered to, so the number FlipAi
// authorizes and the number FlipAi answers are the same value by construction:
// forging the accompanying metadata cannot get an unauthorized conversation
// answered, because the reply would still go to the conversation.
//
// Sender metadata that travels alongside the conversation is deliberately not
// consulted. The Voice web service reports a "did" on each item, which is the
// Google Voice number on this account's own side rather than the person
// texting; treating it as the sender made every real text disagree with its own
// conversation and blocked all of them.
func appendDirectGoogleVoiceSMS(dataDir, payload string) error {
	if len(os.Args) > 1 && strings.EqualFold(os.Args[1], "--google-voice") {
		return nil
	}
	var m directGoogleVoiceSMS
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return err
	}
	m.Thread = normalizeGoogleVoiceSMSThread(m.Thread)
	threadPhone := googleVoiceSMSThreadPhone(m.Thread)
	if threadPhone != "" {
		m.Sender = threadPhone
	} else {
		m.Sender = normalizeUSPhone(m.Sender)
	}
	m.Body = strings.TrimSpace(m.Body)
	if m.Body == "" {
		return nil
	}
	// A "You:" prefix used to mean the conversation list was showing FlipAi's own
	// last message. Nothing reads the conversation list any more -- direction
	// comes from the item itself, and the ledger below catches anything
	// mislabelled -- so that heuristic now only swallows a real text that
	// happens to begin with the word, silently and with nothing logged.
	//
	// Second, independent loop guard. Even if a conversation item were ever
	// mislabelled as inbound, FlipAi will not answer text it just sent.
	if googleVoiceSMSWasSentRecently(dataDir, m.Sender, m.Body) {
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
	// A conversation FlipAi cannot address by phone number -- a group thread, or
	// an older locator without one -- can neither be attributed to a sender nor
	// replied to, so it fails closed here rather than later.
	if threadPhone == "" {
		activity.Add("warn", "security", "Blocked Google Voice SMS: this conversation has no exact phone-number identity; no reply sent", m.Sender, "", m.ID)
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
