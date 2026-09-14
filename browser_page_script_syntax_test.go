package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every script FlipAi injects into a provider page, and the Go constant that
// holds it. A `%s` placeholder is the prompt, substituted before injection.
var browserPageScripts = map[string]struct{ file, constant string }{
	"ChatGPT turn":     {"chatgpt_webview_windows.go", "chatGPTTurnJS"},
	"Claude Chat turn": {"claude_chat_webview_windows.go", "claudeChatTurnJS"},
	"Copilot turn":     {"copilot_chat_webview_windows.go", "copilotChatTurnJS"},
	"Gemini turn":      {"gemini_chat_webview_windows.go", "geminiChatTurnJS"},
	"Grok turn":        {"grok_chat_webview_windows.go", "grokChatTurnJS"},
	"Muse turn":        {"muse_chat_webview_windows.go", "museChatTurnJS"},
}

// A page driver is JavaScript, and a JavaScript syntax error in one is
// invisible to every Go test in this repository: it compiles, it ships, and it
// fails only in the browser, as the indistinguishable-from-everything-else
// sentence "the <provider> page script failed".
//
// That is exactly what happened. A stability escape was appended to the
// Copilot and Muse drivers directly after a `}` with no separator, so both
// scripts were `SyntaxError: Unexpected token 'if'` and neither provider
// answered a single message, while the same edit with a `;` left ChatGPT and
// Claude Chat working. Parse each driver before it can ship again.
func TestEveryBrowserPageScriptParses(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	dir := t.TempDir()
	for name, script := range browserPageScripts {
		t.Run(name, func(t *testing.T) {
			js := readTurnJS(t, script.file, script.constant)
			// The prompt is injected as a JSON string literal; any valid literal
			// parses the same, so a placeholder stands in for the real one.
			js = strings.ReplaceAll(js, "%s", `"a prompt"`)
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".mjs")
			if err := os.WriteFile(path, []byte(js), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("node", "--check", path).CombinedOutput()
			if err != nil {
				t.Fatalf("%s does not parse, so it can only ever fail in the browser:\n%s", script.constant, out)
			}
		})
	}
}

// A page-script failure has to say what failed. Reporting only "the page
// script failed" made a syntax error in our own driver look identical to a
// site redesign, a signed-out session or a changed selector.
func TestPageScriptFailureNamesTheException(t *testing.T) {
	details := json.RawMessage(`{
		"text": "Uncaught",
		"lineNumber": 41,
		"exception": {
			"className": "SyntaxError",
			"description": "SyntaxError: Unexpected token 'if'\n    at <anonymous>:42:3"
		}
	}`)
	err := browserPageScriptError("Microsoft Copilot", details)
	if err == nil {
		t.Fatal("a page-script exception produced no error")
	}
	got := err.Error()
	for _, want := range []string{"Microsoft Copilot", "SyntaxError", "Unexpected token", "line 42"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the failure does not mention %q: %s", want, got)
		}
	}
	if strings.Contains(got, "at <anonymous>") {
		t.Fatalf("the whole stack was forwarded instead of the identifying line: %s", got)
	}

	// With nothing usable to report, the old sentence is still the right one.
	if got := browserPageScriptError("Gemini", nil).Error(); got != "the Gemini page script failed" {
		t.Fatalf("unexpected fallback: %s", got)
	}
}

// Each provider must report its own name, so the Activity log says which
// browser broke.
func TestEveryProviderReportsItsOwnPageScriptFailure(t *testing.T) {
	for file, provider := range map[string]string{
		"chatgpt_webview_windows.go":      "ChatGPT",
		"claude_chat_webview_windows.go":  "Claude",
		"grok_chat_webview_windows.go":    "Grok",
		"muse_chat_webview_windows.go":    "Muse",
		"copilot_chat_webview_windows.go": "Microsoft Copilot",
		"gemini_chat_webview_windows.go":  "Gemini",
	} {
		src := readGoSource(t, file)
		if !strings.Contains(src, `browserPageScriptError("`+provider+`", got.ExceptionDetails)`) {
			t.Fatalf("%s discards the page exception instead of reporting it", file)
		}
	}
}
