package main

import (
	"regexp"
	"strings"
	"testing"
)

// A page driver's timeout detail is a contract, not prose. When a turn reaches
// the end of its budget the model is usually still answering, and FlipAi is
// meant to hand off to the continuation that keeps watching the page. It knows
// to do that only by recognizing the detail text.
//
// Rewording a driver's timeout message broke exactly that: every driver said
// something new, nothing matched, and a turn that was still being answered
// would have been reported as a hard failure instead of being waited out.
func TestEveryDriverTimeoutIsRecognizedAsACheckpoint(t *testing.T) {
	// Every single-quoted message a driver can return, so a second branch of a
	// ternary is covered as surely as the first.
	literalRE := regexp.MustCompile(`'([^'\n]*)'`)
	checked := 0
	for _, file := range []string{
		"chatgpt_webview_windows.go", "claude_chat_webview_windows.go",
		"copilot_chat_webview_windows.go", "gemini_chat_webview_windows.go",
		"grok_chat_webview_windows.go", "muse_chat_webview_windows.go",
	} {
		src := readGoSource(t, file)
		seen := 0
		for _, m := range literalRE.FindAllStringSubmatch(src, -1) {
			detail := m[1]
			lower := strings.ToLower(detail)
			if !strings.Contains(lower, "did not finish") && !strings.Contains(lower, "did not produce") {
				continue
			}
			seen++
			checked++
			if !browserLongTurnTimeoutDetail(detail) {
				t.Errorf("%s reports %q at its deadline, and FlipAi does not recognize that as a checkpoint, so a turn still being answered is reported as failed", file, detail)
			}
		}
		if seen == 0 {
			t.Errorf("%s has no recognizable deadline message; this guard is not covering it", file)
		}
	}
	if checked == 0 {
		t.Fatal("no driver timeout details were found; this guard is not testing anything")
	}
	t.Logf("checked %d driver timeout details", checked)
}
