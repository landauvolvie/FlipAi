package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMailClient struct {
	mu   sync.Mutex
	msg  GmailMessage
	sent string
}

func (f *fakeMailClient) Authorized() bool                                  { return true }
func (f *fakeMailClient) Test(context.Context) error                        { return nil }
func (f *fakeMailClient) List(context.Context) ([]string, error)            { return []string{f.msg.ID}, nil }
func (f *fakeMailClient) Get(context.Context, string) (GmailMessage, error) { return f.msg, nil }
func (f *fakeMailClient) SendText(_ context.Context, _ string, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = body
	return nil
}

func TestEndToEndIncomingVoiceStatusAndReply(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodAppPassword
	cfg.GoogleVoice.AllowedFrom = "8455551234"
	cfg.GoogleVoice.RequiredSubjectPhrase = "new text message from"
	cfg.GoogleVoice.GmailReplyFallback = true
	cfg.GoogleVoice.SendReplyViaAgentBrowser = false
	if err := setSecurityCode(&cfg, "482913"); err != nil {
		t.Fatal(err)
	}
	m := GmailMessage{
		ID:                    "42",
		From:                  "Google Voice <voice-noreply@google.com>",
		ReplyTo:               "18455551234.abc@txt.voice.google.com",
		Subject:               "New text message from (845) 555-1234",
		AuthenticationResults: "mx.google.com; dkim=pass header.d=google.com",
		Body:                  "482913 STATUS",
		InternalDate:          time.Now(),
	}
	fm := &fakeMailClient{msg: m}
	stateFile := t.TempDir() + "/state.json"
	b := NewBridge(cfg, stateFile, State{GmailBaselineUnix: time.Now().Add(-time.Minute).Unix()}, fm, nil, nil)
	b.poll(context.Background())
	fm.mu.Lock()
	got := fm.sent
	fm.mu.Unlock()
	if !strings.Contains(got, "Bridge online") {
		t.Fatalf("expected SMS reply, got %q", got)
	}
	if !b.processed("42") {
		t.Fatal("message was not checkpointed")
	}
}
