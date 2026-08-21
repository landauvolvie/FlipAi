package main

import (
	"strings"
	"testing"
)

func TestNormalizeAllowedPhoneList(t *testing.T) {
	got, err := normalizeAllowedPhoneList("(845) 604-3655\n+1 212-555-1212,8456043655")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "2125551212" || got[1] != "8456043655" {
		t.Fatalf("unexpected list: %#v", got)
	}
	if _, err := normalizeAllowedPhoneList("911"); err == nil {
		t.Fatal("expected invalid short number to be rejected")
	}
}

func TestGoogleVoiceSenderUsesStructuredEnvelope(t *testing.T) {
	m := GmailMessage{Subject: "New text message from Alice", From: `Alice (SMS) <18453241813.8456043655.abcdef@txt.voice.google.com>`}
	sender, ok := googleVoiceSender(m, "new text message from")
	if !ok || sender != "8456043655" {
		t.Fatalf("sender=%q ok=%v", sender, ok)
	}
}

func TestGoogleVoiceSenderUsesReplyToWhenFromIsNoreply(t *testing.T) {
	m := GmailMessage{
		Subject: "New text message from Alice",
		From:    "Google Voice <voice-noreply@google.com>",
		ReplyTo: `18453241813.8456043655.abcdef@txt.voice.google.com`,
	}
	sender, ok := googleVoiceSender(m, "new text message from")
	if !ok || sender != "8456043655" {
		t.Fatalf("sender=%q ok=%v", sender, ok)
	}
}

func TestUnauthorizedBodyCannotSpoofAllowedNumber(t *testing.T) {
	m := GmailMessage{
		Subject: "New text message from Mallory",
		From:    `Mallory (SMS) <18453241813.6465550101.abcdef@txt.voice.google.com>`,
		Body:    "My message mentions the allowed number 845-604-3655",
	}
	sender, ok := googleVoiceSender(m, "new text message from")
	if !ok || sender != "6465550101" {
		t.Fatalf("sender=%q ok=%v", sender, ok)
	}
	if allowedPhone("8456043655\n2125551212", sender) {
		t.Fatal("unauthorized sender was accepted")
	}
}

func TestVoiceNoreplySubjectFallback(t *testing.T) {
	m := GmailMessage{Subject: "New text message from (845) 604-3655", From: "Google Voice <voice-noreply@google.com>"}
	sender, ok := googleVoiceSender(m, "new text message from")
	if !ok || sender != "8456043655" {
		t.Fatalf("sender=%q ok=%v", sender, ok)
	}
}

func TestParseGoogleVoiceRejectsUnauthorizedSenderEvenIfBodyMentionsAllowed(t *testing.T) {
	m := GmailMessage{
		Subject:               "New text message from Mallory",
		From:                  `Mallory (SMS) <18453241813.6465550101.abcdef@txt.voice.google.com>`,
		AuthenticationResults: "mx.google.com; dkim=pass header.d=google.com",
		Body:                  "482913 C: the allowed number is 8456043655",
	}
	if _, _, ok := parseGoogleVoiceBody(m, "8456043655\n2125551212", "new text message from"); ok {
		t.Fatal("unauthorized sender passed because body mentioned allowed number")
	}
}

func TestAgentPromptTargetsExactAuthenticatedSender(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.GoogleVoice.AllowedFrom = "8456043655\n2125551212"
	b := NewBridge(cfg, t.TempDir()+"/state.json", State{}, nil, nil, nil)
	p := b.composePrompt("do the job", "C", "2125551212")
	if !strings.Contains(p, "THIS EXACT PHONE NUMBER") || strings.Count(p, "2125551212") < 2 {
		t.Fatalf("prompt does not strongly target sender: %s", p)
	}
	if strings.Contains(p, "8456043655") {
		t.Fatalf("prompt leaked another allowed destination: %s", p)
	}
}
