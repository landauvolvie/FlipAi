package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// browserTurnDrivers maps each browser agent's page driver to the turn script
// constant it sends into the page.
var browserTurnDrivers = map[string]struct {
	file     string
	constant string
	provider string
}{
	"ChatGPT Chat":           {"chatgpt_webview_windows.go", "chatGPTTurnJS", "chatgpt"},
	"Claude Chat":            {"claude_chat_webview_windows.go", "claudeChatTurnJS", "claude-chat"},
	"Microsoft Copilot Chat": {"copilot_chat_webview_windows.go", "copilotChatTurnJS", "copilot"},
	"Gemini Chat":            {"gemini_chat_webview_windows.go", "geminiChatTurnJS", "gemini"},
	"Grok Chat":              {"grok_chat_webview_windows.go", "grokChatTurnJS", "grok"},
	"Muse":                   {"muse_chat_webview_windows.go", "museChatTurnJS", "muse"},
}

func readTurnJS(t *testing.T, file, constant string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	open := "const " + constant + " = `"
	i := strings.Index(src, open)
	if i < 0 {
		t.Fatalf("%s: %s not found", file, constant)
	}
	i += len(open)
	j := strings.Index(src[i:], "`")
	if j < 0 {
		t.Fatalf("%s: %s is not terminated", file, constant)
	}
	return src[i : i+j]
}

// Every driver decided a turn was finished only when the page showed no
// stop-like control. "Working" is inferred from the page's own buttons, so a
// provider that leaves such a control on screen after the answer is complete --
// voice mode, a stale streaming affordance, a renamed button -- reads as
// working forever. The model's answer was then sitting finished in the browser
// while FlipAi texted "still working" until the turn was abandoned, which is
// exactly what the user saw for ChatGPT and Gemini.
//
// Every driver must therefore have a completion path that does not consult
// stop() at all: an answer that has stopped changing is a finished answer.
func TestEveryBrowserDriverCanFinishWithAStuckStopControl(t *testing.T) {
	// Each escape is a settle window: 250ms polls counted in `stable`, or an
	// elapsed `quietFor` in milliseconds.
	escapes := map[string]string{
		"ChatGPT Chat":           "stable>=32",
		"Claude Chat":            "stable>=32",
		"Microsoft Copilot Chat": "stable>=32",
		"Muse":                   "stable>=32",
		"Gemini Chat":            "quietFor>=8000",
		"Grok Chat":              "quietFor>=7000",
	}
	for name, driver := range browserTurnDrivers {
		t.Run(name, func(t *testing.T) {
			js := readTurnJS(t, driver.file, driver.constant)
			escape, ok := escapes[name]
			if !ok {
				t.Fatalf("no settle escape recorded for %s", name)
			}
			if !strings.Contains(js, escape) {
				t.Fatalf("%s can only finish a turn while stop() is absent; a stuck stop control strands a completed answer", name)
			}
			// The escape is worthless if it is itself gated on stop().
			for _, line := range strings.Split(js, "\n") {
				if strings.Contains(line, escape) && strings.Contains(line, "stop()") &&
					!strings.Contains(line, "!stop()&&") {
					t.Fatalf("%s: the settle escape is gated on stop(): %q", name, strings.TrimSpace(line))
				}
			}
		})
	}
}

// A turn expression that resolves to no provider gets no long-turn
// continuation, so the SMS side waits out its whole extended window on a state
// file nothing will ever write and then fails. Muse was in exactly that state.
func TestEveryBrowserTurnExpressionResolvesToItsLongTurnProvider(t *testing.T) {
	raw, err := os.ReadFile("browser_long_turn_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, "func browserLongTurnProviderFromExpression")
	end := strings.Index(src[start:], "\nfunc ")
	if start < 0 || end < 0 {
		t.Fatal("browserLongTurnProviderFromExpression not found")
	}
	body := src[start : start+end]

	// Rebuild the matcher from its own source: each case's phrases, in order,
	// followed by the provider it returns.
	caseRE := regexp.MustCompile(`(?s)case (.*?):\s*\n\s*return "([a-z-]+)"`)
	phraseRE := regexp.MustCompile(`strings\.Contains\(s, "([^"]+)"\)`)
	type rule struct {
		phrases  []string
		provider string
	}
	var rules []rule
	for _, m := range caseRE.FindAllStringSubmatch(body, -1) {
		var phrases []string
		for _, p := range phraseRE.FindAllStringSubmatch(m[1], -1) {
			phrases = append(phrases, p[1])
		}
		if len(phrases) > 0 {
			rules = append(rules, rule{phrases: phrases, provider: m[2]})
		}
	}
	if len(rules) != len(browserTurnDrivers) {
		t.Fatalf("matcher has %d provider rules, but %d browser drivers exist", len(rules), len(browserTurnDrivers))
	}

	for name, driver := range browserTurnDrivers {
		t.Run(name, func(t *testing.T) {
			js := strings.ToLower(readTurnJS(t, driver.file, driver.constant))
			got := ""
			for _, r := range rules {
				for _, phrase := range r.phrases {
					if strings.Contains(js, phrase) {
						got = r.provider
						break
					}
				}
				if got != "" {
					break
				}
			}
			if got != driver.provider {
				t.Fatalf("%s turn expression resolves to %q, want %q; a turn with no provider gets no long-turn continuation", name, got, driver.provider)
			}
		})
	}
}

// The long-turn watcher and the waits around it must all terminate. Each of
// these bounds a state in which the sender is being texted "still working".
func TestBrowserTurnWaitsAreAllBounded(t *testing.T) {
	if browserLongTurnSettledSamples <= 0 {
		t.Fatal("a settled answer is never delivered while the page claims to be working")
	}
	if browserLongTurnWatchCap <= browserLongTurnMaxWait {
		t.Fatalf("the watcher cap (%v) must outlast the SMS wait it feeds (%v)", browserLongTurnWatchCap, browserLongTurnMaxWait)
	}
	if browserChatGeneratedImageMaxWait <= 0 {
		t.Fatal("the generated-image wait is unbounded, so an image that never arrives pins the agent queue forever")
	}

	src, err := os.ReadFile("browser_chat_generated_image_wait.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "waitForCapturedBrowserChatReturnedMedia(ctx, 0)") {
		t.Fatal("the generated-image wait went back to waiting with no cap")
	}
}

// A page checkpoint that does not come back in time is not a dead turn: the
// model is usually still answering. Without starting the continuation there,
// the answer lands in the page with nothing watching for it.
func TestDevToolsTimeoutStillWatchesTheBrowserTurn(t *testing.T) {
	raw, err := os.ReadFile("voice_cdp_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	i := strings.Index(src, "case <-time.After(timeout):")
	if i < 0 {
		t.Fatal("the DevTools call timeout branch was not found")
	}
	branch := src[i:]
	if j := strings.Index(branch, "\n\t}\n}"); j > 0 {
		branch = branch[:j]
	}
	if !strings.Contains(branch, "continueBrowserLongTurn") {
		t.Fatal("a timed-out browser turn is abandoned with nothing watching the page for its answer")
	}
}

// Every browser agent reloads its saved session when FlipAi starts, all at
// once. A text queued while FlipAi was down is dispatched seconds after the
// bridge comes up, met a browser that was still restoring, and burned the turn
// -- and with the per-agent queue, one burned turn delays every text behind it.
func TestBrowserTurnsWaitLongEnoughForASessionRestore(t *testing.T) {
	if browserChatTurnReadyWait < time.Minute {
		t.Fatalf("browser readiness wait is %v; a saved session routinely takes longer than that to restore", browserChatTurnReadyWait)
	}
	for _, file := range []string{
		"sms_sticky_chatgpt.go", "sms_claude_chat.go", "sms_gemini_chat.go",
		"sms_grok_chat.go", "sms_copilot_chat.go", "sms_muse_chat.go",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		if !strings.Contains(src, "browserChatTurnReadyWait") {
			t.Fatalf("%s does not use the shared browser readiness wait", file)
		}
		if strings.Contains(src, "context.WithTimeout(ctx, 15*time.Second)") ||
			strings.Contains(src, "context.WithTimeout(ctx, 15 * time.Second)") {
			t.Fatalf("%s went back to a 15-second readiness wait, which is shorter than a session restore", file)
		}
	}
}
