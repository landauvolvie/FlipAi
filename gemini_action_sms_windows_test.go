//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestGeminiSMSExcludesGmailActionCardChrome(t *testing.T) {
	for _, want := range []string{
		"replyText",
		`.markdown-main-panel`,
		`pendingGmailSend`,
		`actionScope`,
		`Gemini submitted the pending email Send action.`,
		`/^(yes|yes please|yes send it|send it|confirm|go ahead|do it)$/`,
	} {
		if !strings.Contains(geminiChatTurnJS, want) {
			t.Fatalf("Gemini SMS action handling lost %q", want)
		}
	}
	for _, chrome := range []string{`button,[role="button"]`, `[role="toolbar"]`, `[data-testid*="action" i]`} {
		if !strings.Contains(geminiChatTurnJS, chrome) {
			t.Fatalf("Gemini reply extraction no longer strips action-card UI via %q", chrome)
		}
	}
}

func TestGeminiComposerSendDoesNotUsePendingActionSend(t *testing.T) {
	if !strings.Contains(geminiChatTurnJS, `!actionScope(b)`) {
		t.Fatal("Gemini composer Send lookup can select a pending Gmail action Send button")
	}
}
