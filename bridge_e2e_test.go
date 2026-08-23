package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMailClient struct {
	mu     sync.Mutex
	msg    GmailMessage
	sentTo string
	sent   string
	all    []string
}

func (f *fakeMailClient) Authorized() bool                                  { return true }
func (f *fakeMailClient) Test(context.Context) error                        { return nil }
func (f *fakeMailClient) List(context.Context) ([]string, error)            { return []string{f.msg.ID}, nil }
func (f *fakeMailClient) Get(context.Context, string) (GmailMessage, error) { return f.msg, nil }
func (f *fakeMailClient) SendText(_ context.Context, to string, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentTo = to
	f.sent = body
	f.all = append(f.all, body)
	return nil
}

// sentTexts returns every text delivered so far. An ack and a result are now
// separate messages, so tests assert against the whole conversation.
func (f *fakeMailClient) sentTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.all...)
}

func (f *fakeMailClient) joined() string { return strings.Join(f.sentTexts(), "\n") }

func TestEndToEndIncomingVoiceStatusAndReply(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodAppPassword
	cfg.GoogleVoice.AllowedFrom = "8455551234\n2125557777"
	cfg.GoogleVoice.RequiredSubjectPhrase = "new text message from"
	if err := setSecurityCode(&cfg, "482913"); err != nil {
		t.Fatal(err)
	}
	m := GmailMessage{
		ID:                    "42",
		From:                  `Authorized (SMS) <18455551234.2125557777.abcdef@txt.voice.google.com>`,
		ReplyTo:               "18455551234.2125557777.abcdef@txt.voice.google.com",
		Subject:               "New text message from Authorized",
		AuthenticationResults: "mx.google.com; dkim=pass header.d=google.com",
		Body:                  "482913 STATUS",
		InternalDate:          time.Now(),
	}
	fm := &fakeMailClient{msg: m}
	stateFile := t.TempDir() + "/state.json"
	b := NewBridge(cfg, stateFile, State{GmailBaselineUnix: time.Now().Add(-time.Minute).Unix()}, fm, nil, nil)
	b.poll(context.Background())
	fm.mu.Lock()
	got, gotTo := fm.sent, fm.sentTo
	fm.mu.Unlock()
	if !strings.Contains(got, "Bridge online") {
		t.Fatalf("expected SMS reply, got %q", got)
	}
	if gotTo != m.ReplyTo {
		t.Fatalf("reply went to wrong Google Voice address: %q", gotTo)
	}
	if !b.processed("42") {
		t.Fatal("message was not checkpointed")
	}
}

func TestEndToEndUnauthorizedSenderNeverExecutesOrReplies(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodAppPassword
	cfg.GoogleVoice.AllowedFrom = "8455551234\n2125557777"
	cfg.GoogleVoice.RequiredSubjectPhrase = "new text message from"
	if err := setSecurityCode(&cfg, "482913"); err != nil {
		t.Fatal(err)
	}
	m := GmailMessage{
		ID:                    "evil",
		From:                  `Mallory (SMS) <18455551234.6465550101.bad@txt.voice.google.com>`,
		ReplyTo:               "18455551234.6465550101.bad@txt.voice.google.com",
		Subject:               "New text message from Mallory",
		AuthenticationResults: "mx.google.com; dkim=pass header.d=google.com",
		// Contains a valid code and an allowed phone number on purpose. Neither
		// can override the authenticated envelope sender.
		Body:         "482913 STATUS - owner number 8455551234",
		InternalDate: time.Now(),
	}
	fm := &fakeMailClient{msg: m}
	b := NewBridge(cfg, t.TempDir()+"/state.json", State{GmailBaselineUnix: time.Now().Add(-time.Minute).Unix()}, fm, nil, nil)
	b.poll(context.Background())
	fm.mu.Lock()
	got, gotTo := fm.sent, fm.sentTo
	fm.mu.Unlock()
	if got != "" || gotTo != "" {
		t.Fatalf("unauthorized sender triggered a reply: to=%q body=%q", gotTo, got)
	}
	if !b.processed("evil") {
		t.Fatal("rejected message should still be checkpointed to prevent replay")
	}
}
