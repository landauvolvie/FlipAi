package main

import (
	"strings"
	"testing"
)

// Gemini stopped sending anything at all -- "FlipAi filled the Gemini prompt
// box but the Send button never became ready" -- because it was the last
// driver whose only way to submit was a button it could name by selector. When
// the site renamed or disabled that control, the prompt was typed and then
// abandoned, and the message never reached the model.
//
// Every browser driver must be able to submit the way a person does.
func TestEveryBrowserDriverCanSendWithoutASendButton(t *testing.T) {
	for _, file := range []string{
		"chatgpt_webview_windows.go",
		"claude_chat_webview_windows.go",
		"copilot_chat_webview_windows.go",
		"gemini_chat_webview_windows.go",
		"grok_chat_webview_windows.go",
		"muse_chat_webview_windows.go",
	} {
		source := readGoSource(t, file)
		if !strings.Contains(source, "KeyboardEvent") {
			t.Errorf("%s has no Enter-key fallback: a renamed or disabled Send control strands the prompt in the composer", file)
		}
		if !strings.Contains(source, "requestSubmit") {
			t.Errorf("%s never tries the composer's own form submit", file)
		}
		if strings.Contains(source, "Send button never became ready") {
			t.Errorf("%s still gives up when it cannot name a Send button", file)
		}
	}
}

// The source cards under an answer are page furniture. The linked phrases
// inside a sentence are the answer. Removing every element the page labelled a
// citation or a source took both, and sentences arrived on the phone with
// words missing.
func TestBrowserDriversOnlyStripBlockLevelSourceCards(t *testing.T) {
	for _, file := range []string{
		"chatgpt_webview_windows.go",
		"copilot_chat_webview_windows.go",
		"muse_chat_webview_windows.go",
	} {
		source := readGoSource(t, file)
		// A comma immediately before the attribute means the selector matches it
		// on any element at all. The block-level forms built by joinSel are
		// always preceded by their tag name, and the refKeys entries by a quote.
		for _, unqualified := range []string{
			`,[class*="citation" i]`,
			`,[class*="source" i]`,
			`,[class*="reference" i]`,
			`,[class*="card" i]`,
			`,[data-testid*="citation" i]`,
			`,[data-testid*="source" i]`,
			`,[data-testid*="card" i]`,
		} {
			if strings.Contains(source, unqualified) {
				t.Errorf("%s matches %q on any element, which deletes linked phrases out of the answer; qualify it with a block-level tag", file, unqualified)
			}
		}
		if !strings.Contains(source, "refBlockSel") {
			t.Errorf("%s does not restrict source-card removal to block-level containers", file)
		}
	}
}
