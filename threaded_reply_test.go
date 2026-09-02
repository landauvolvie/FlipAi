package main

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

func TestGoogleVoiceGatewayBodyKeepsAllMultilineContent(t *testing.T) {
	got := googleVoiceGatewayBody("Today's notable Gmail:\n\n• VoIP.ms ticket escalated\n• Low balance: $7.78\n• Dell order is on the way.")
	want := "Today's notable Gmail: • VoIP.ms ticket escalated • Low balance: $7.78 • Dell order is on the way."
	if got != want {
		t.Fatalf("gateway body = %q, want %q", got, want)
	}
}

func TestNumberedVoiceReplyIsReassembledBeforeDelivery(t *testing.T) {
	m := GmailMessage{ID: "voice-message-1"}
	firstText := "Today's notable Gmail: • VoIP.ms ticket escalated to the dev team. • VoIP.ms low balance alert: $7.78 remaining. • Google Flights fare changed. • Amex payment received."
	first := "1/2 " + firstText
	if got, ready := assembleNumberedVoiceReply(m, first); ready || got != "" {
		t.Fatalf("first part should be buffered; got %q ready=%v", got, ready)
	}
	secondText := "eBay Dell order was scanned and is on the way. • Robinhood statement available."
	got, ready := assembleNumberedVoiceReply(m, "2/2 "+secondText)
	if !ready {
		t.Fatal("second part did not complete the reply")
	}
	want := firstText + " " + secondText
	if got != want {
		t.Fatalf("assembled reply = %q, want %q", got, want)
	}
}

func TestShortNaturalFractionIsNotMistakenForSplitReply(t *testing.T) {
	m := GmailMessage{ID: "voice-message-2"}
	body := "1/2 cup sugar is enough."
	got, ready := assembleNumberedVoiceReply(m, body)
	if !ready || got != body {
		t.Fatalf("natural fraction should pass through; got %q ready=%v", got, ready)
	}
}
