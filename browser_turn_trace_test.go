package main

import (
	"context"
	"strings"
	"testing"
)

// A browser turn crosses three processes, and for a long time the only thing
// that survived the trip was one sentence written after the turn had already
// failed. "It did not work" could mean the worker was never reachable, the
// prompt was never typed, the model never answered, or the answer was found and
// discarded -- and telling those apart meant guessing, release after release.
//
// Every driver reports its own steps, and every provider carries them out.
func TestEveryDriverReportsWhatItDid(t *testing.T) {
	for _, file := range []string{
		"chatgpt_webview_windows.go", "claude_chat_webview_windows.go",
		"copilot_chat_webview_windows.go", "gemini_chat_webview_windows.go",
		"grok_chat_webview_windows.go", "muse_chat_webview_windows.go",
	} {
		src := readGoSource(t, file)
		for _, want := range []string{
			"const T=[],t0=Date.now();", // the trace itself
			"composer-found",            // did the page offer somewhere to type
			"mark('typed')",             // was the prompt actually typed
			"send-",                     // how it was sent
			"mark('deadline",            // what the page looked like when it gave up
			"Trace",                     // the worker carries it out
			`"trace": got.Trace`,
		} {
			if want == `"trace": got.Trace` && strings.Contains(src, `"trace":got.Trace`) {
				continue
			}
			if !strings.Contains(src, want) {
				t.Errorf("%s does not report %q, so a failed turn cannot say where it stopped", file, want)
			}
		}
		// Inside the turn script itself, a result without its trace is a result
		// nobody can diagnose. Other scripts in the same file (the experience
		// picker, the Code-mode start) are not turns and are not covered here.
		for _, m := range jsConstRE.FindAllStringSubmatch(src, -1) {
			name, js := m[1], m[2]
			if !strings.Contains(js, "__FLIPAI_BROWSER_TURN__") {
				continue
			}
			if strings.Contains(js, "{ok:false,detail:") || strings.Contains(js, "{ok:true,reply:") {
				t.Errorf("%s: %s returns a turn result with no trace attached", file, name)
			}
		}
	}
}

// The steps have to reach the activity log against the turn's own message, or
// they are only in a place nobody looks.
func TestBrowserTurnStepsReachTheActivityLog(t *testing.T) {
	bridge := readGoSource(t, "bridge.go")
	if !strings.Contains(bridge, "withBrowserTurnSteps(ctx,") {
		t.Fatal("the bridge never attaches a step sink, so no browser turn step is ever written")
	}
	if !strings.Contains(bridge, `"agent-step"`) {
		t.Fatal("browser turn steps are not written under their own activity stage")
	}
	for _, file := range []string{
		"sms_sticky_chatgpt.go", "sms_claude_chat.go", "sms_copilot_chat.go",
		"sms_gemini_chat.go", "sms_grok_chat.go", "sms_muse_chat.go",
	} {
		src := readGoSource(t, file)
		for _, want := range []string{"browserTurnReady(", "browserTurnAnswered(", "browserTurnWatching("} {
			if !strings.Contains(src, want) {
				t.Errorf("%s does not call %s, so that stage of the turn is invisible", file, want)
			}
		}
	}
}

func TestBrowserTurnStepIsSafeWithoutASink(t *testing.T) {
	// The SMS layers report unconditionally; a turn started outside the bridge
	// must not panic because nobody is listening.
	browserTurnStep(context.Background(), "info", "no sink here")
	browserTurnStep(nil, "info", "no context at all") //nolint:staticcheck // deliberately nil

	var got []string
	ctx := withBrowserTurnSteps(context.Background(), func(level, step string) {
		got = append(got, level+": "+step)
	})
	browserTurnStep(ctx, "info", "first")
	browserTurnStep(ctx, "warn", "  ")
	browserTurnStep(ctx, "error", "second")
	if len(got) != 2 || got[0] != "info: first" || got[1] != "error: second" {
		t.Fatalf("unexpected steps: %v", got)
	}
}

func TestBrowserPageTraceSaysSomethingWhenTheDriverSaysNothing(t *testing.T) {
	if got := browserPageTrace("Muse", "   "); !strings.Contains(got, "no steps") {
		t.Fatalf("an empty trace must still report something: %q", got)
	}
	if got := browserPageTrace("Muse", "composer-found @0.4s"); !strings.Contains(got, "composer-found") {
		t.Fatalf("the trace was not carried through: %q", got)
	}
}
