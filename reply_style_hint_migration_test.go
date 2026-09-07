package main

import "testing"

func TestDefaultReplyStyleHintDoesNotMentionSMSOrPlainText(t *testing.T) {
	const want = "Please keep your reply short and to the point."
	if defaultReplyStyleHint != want {
		t.Fatalf("default reply style hint = %q, want %q", defaultReplyStyleHint, want)
	}
}

func TestLegacyReplyStyleHintsMigrateToConciseDefault(t *testing.T) {
	for _, legacy := range []string{"", legacyReplyStyleHintSMSV1, legacyReplyStyleHintSMSV0} {
		if got := migrateLegacyReplyStyleHint(legacy); got != defaultReplyStyleHint {
			t.Fatalf("legacy hint %q migrated to %q, want %q", legacy, got, defaultReplyStyleHint)
		}
	}
}

func TestCustomReplyStyleHintIsPreserved(t *testing.T) {
	const custom = "Answer in one sentence unless I ask for details."
	if got := migrateLegacyReplyStyleHint(custom); got != custom {
		t.Fatalf("custom hint changed to %q", got)
	}
}
