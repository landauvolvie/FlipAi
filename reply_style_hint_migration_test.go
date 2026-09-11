package main

import "testing"

func TestDefaultReplyStyleHintIsEmpty(t *testing.T) {
	if defaultReplyStyleHint != "" {
		t.Fatalf("default reply style hint = %q, want empty", defaultReplyStyleHint)
	}
	if got := defaultConfig(t.TempDir()).GoogleVoice.ReplyStyleHint; got != "" {
		t.Fatalf("fresh config reply style hint = %q, want empty", got)
	}
}

func TestRetiredReplyStyleHintsMigrateToNoInstruction(t *testing.T) {
	for _, legacy := range []string{"", retiredConciseReplyStyleHint, legacyReplyStyleHintSMSV1, legacyReplyStyleHintSMSV0} {
		if got := migrateLegacyReplyStyleHint(legacy); got != "" {
			t.Fatalf("retired hint %q migrated to %q, want empty", legacy, got)
		}
	}
}

func TestCustomReplyStyleHintIsPreserved(t *testing.T) {
	const custom = "Answer in one sentence unless I ask for details."
	if got := migrateLegacyReplyStyleHint(custom); got != custom {
		t.Fatalf("custom hint changed to %q", got)
	}
}
