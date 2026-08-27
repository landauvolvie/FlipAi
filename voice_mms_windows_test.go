//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestGoogleVoiceImageRecipientPrefersTrustedReplyAddress(t *testing.T) {
	m := GmailMessage{
		ReplyTo: "18453842803.18453241813.token@txt.voice.google.com",
		From:    "voice-noreply@google.com",
		Subject: "New text message from (999) 555-0101",
	}
	if got := googleVoiceImageRecipient(m); got != "8453241813" {
		t.Fatalf("recipient=%q, want 8453241813", got)
	}
}

func TestGoogleVoiceImageRecipientFallsBackToSubject(t *testing.T) {
	m := GmailMessage{
		From:    "voice-noreply@google.com",
		Subject: "New text message from (845) 324-1813",
	}
	if got := googleVoiceImageRecipient(m); got != "8453241813" {
		t.Fatalf("recipient=%q, want 8453241813", got)
	}
}

func TestGoogleVoiceMMSAutomationCoversDocumentedImageControl(t *testing.T) {
	if !strings.Contains(strings.ToLower(voicePrepareMMSJS), "select image") {
		t.Fatal("MMS automation no longer recognizes Google Voice's documented Select image control")
	}
	if !strings.Contains(voiceImageInputObjectJS, `input[type="file"]`) {
		t.Fatal("MMS automation no longer locates the Google Voice file input")
	}
}
