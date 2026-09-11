package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDirectGoogleVoiceDisablesLegacyBridgePartCap(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	raw := []byte(`{"gmail":{"method":"google_voice"},"googleVoice":{"replyMaxChars":300,"maxReplyParts":4}}`)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.GoogleVoice.ReplyMaxChars != directGoogleVoiceLogicalReplyMaxRunes || cfg.GoogleVoice.MaxReplyParts != 1 {
		t.Fatalf("direct Voice still uses legacy bridge splitting: max=%d parts=%d", cfg.GoogleVoice.ReplyMaxChars, cfg.GoogleVoice.MaxReplyParts)
	}
	full := strings.Repeat("1234567890", 2000)
	parts := splitReply(full, cfg.GoogleVoice.ReplyMaxChars, cfg.GoogleVoice.MaxReplyParts)
	if len(parts) != 1 || parts[0] != full {
		t.Fatalf("direct Voice lost text before its 1500-character transport splitter: %d parts, %d/%d runes", len(parts), len([]rune(parts[0])), len([]rune(full)))
	}
}

func TestNonDirectMailKeepsLegacyReplySettings(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	raw := []byte(`{"gmail":{"method":"app_password"},"googleVoice":{"replyMaxChars":500,"maxReplyParts":7}}`)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.GoogleVoice.ReplyMaxChars != 500 || cfg.GoogleVoice.MaxReplyParts != 7 {
		t.Fatalf("non-direct mail settings were unexpectedly rewritten: max=%d parts=%d", cfg.GoogleVoice.ReplyMaxChars, cfg.GoogleVoice.MaxReplyParts)
	}
}
