package main

import (
	"errors"
	"strings"
	"testing"
)

func TestSplitGoogleVoiceOutboundTextUses1500CharacterChunks(t *testing.T) {
	body := strings.Repeat("a", 2000)
	chunks := splitGoogleVoiceOutboundText(body)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if got := len([]rune(chunks[0])); got != 1500 {
		t.Fatalf("first chunk has %d characters, want 1500", got)
	}
	if got := len([]rune(chunks[1])); got != 500 {
		t.Fatalf("second chunk has %d characters, want 500", got)
	}
	if got := strings.Join(chunks, ""); got != body {
		t.Fatal("chunking changed the reply text")
	}
}

func TestSplitGoogleVoiceOutboundTextPreservesUnicode(t *testing.T) {
	body := strings.Repeat("🙂", 1501)
	chunks := splitGoogleVoiceOutboundText(body)
	if len(chunks) != 2 || len([]rune(chunks[0])) != 1500 || len([]rune(chunks[1])) != 1 {
		t.Fatalf("unicode chunk sizes = %v, want [1500 1]", []int{len([]rune(chunks[0])), len([]rune(chunks[1]))})
	}
	if strings.Join(chunks, "") != body {
		t.Fatal("unicode chunking changed the reply text")
	}
}

func TestSplitGoogleVoiceOutboundTextExactAndMultipleBoundaries(t *testing.T) {
	if chunks := splitGoogleVoiceOutboundText(strings.Repeat("x", 1500)); len(chunks) != 1 || len([]rune(chunks[0])) != 1500 {
		t.Fatalf("1500 characters should remain one chunk: %#v", chunks)
	}
	chunks := splitGoogleVoiceOutboundText(strings.Repeat("x", 3001))
	if len(chunks) != 3 || len([]rune(chunks[0])) != 1500 || len([]rune(chunks[1])) != 1500 || len([]rune(chunks[2])) != 1 {
		t.Fatalf("3001 characters did not split as 1500/1500/1")
	}
}

func TestSendGoogleVoiceTextChunksIsSequentialAndStopsOnFailure(t *testing.T) {
	body := strings.Repeat("z", 3500)
	sentinel := errors.New("send failed")
	var sent []string
	err := sendGoogleVoiceTextChunks(body, func(chunk string) error {
		sent = append(sent, chunk)
		if len(sent) == 2 {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got error %v, want sentinel", err)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d chunks after failure, want exactly 2 attempts", len(sent))
	}
	if len([]rune(sent[0])) != 1500 || len([]rune(sent[1])) != 1500 {
		t.Fatalf("attempted chunk sizes are not 1500/1500")
	}
}
