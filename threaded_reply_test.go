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
