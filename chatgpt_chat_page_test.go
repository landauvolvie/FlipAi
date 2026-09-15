package main

import (
	"strings"
	"testing"
	"time"
)

// ChatGPT has more than one page with a box you can type in. FlipAi's browser
// had drifted onto the Scheduled page, where it typed a text message into
// "Schedule a task", sent it with Enter, and read that page's list of
// recommended tasks back as the answer -- which is how "Let me know when a new
// Dell with Intel, 32 GB RAM, touchscreen, and built-in 5G appears" was texted
// as the reply to "hi".
//
// The turn's own trace showed it exactly: composer-found at 0.0s, typed, sent,
// and then "named=0 blocks=5" -- no assistant messages on the page at all,
// because the page was not a conversation.
func TestChatGPTOnlyTypesIntoAConversation(t *testing.T) {
	src := readGoSource(t, "chatgpt_webview_windows.go")

	// The surfaces that are not chat, named in both the driver and the worker.
	for _, surface := range []string{"codex", "tasks", "gpts", "explore", "library", "settings"} {
		if strings.Count(src, surface) < 2 {
			t.Errorf("%q is not excluded in both the page driver and the worker's page check", surface)
		}
	}
	if !strings.Contains(src, "const nonChatPath=()=>") {
		t.Fatal("the page driver does not know which ChatGPT pages are not a conversation")
	}
	if !strings.Contains(src, "chatGPTOnChatPageJS") {
		t.Fatal("the worker has no way to ask whether the page is a conversation")
	}
	if !strings.Contains(src, "waitForChatPage(5 * time.Second)") {
		t.Fatal("a turn no longer waits for a conversation before typing into the page")
	}

	// A generic box is never enough on its own: the named chat composer, or a
	// page that is at least a conversation.
	if !strings.Contains(src, "const namedComposer=()=>") {
		t.Fatal("the driver does not prefer the named chat composer")
	}
	if strings.Contains(src, `const composer=()=>queryAll('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"][data-virtualkeyboard],[contenteditable="true"],textarea')[0]||null;`) {
		t.Fatal("the driver still accepts any box on any ChatGPT page as the composer")
	}

	// The page it is on belongs in the trace: a log that named the path would
	// have identified this in one round instead of several.
	if !strings.Contains(src, "mark('path='+(location.pathname||'/'))") {
		t.Fatal("the trace does not record which ChatGPT page the turn ran on")
	}
}

// A sign-in probe is a guess about someone else's page. When the guess goes
// stale, the worker is running and the account is signed in, and every turn is
// still refused for the whole readiness window with advice to reconnect a
// connection that was never broken. Muse spent ninety seconds doing that.
func TestAStaleSignInProbeDoesNotRefuseTheTurnForever(t *testing.T) {
	if !acceptBrowserWorker(true, true, true, 0) {
		t.Fatal("a signed-in worker must be accepted immediately")
	}
	if acceptBrowserWorker(false, true, true, browserReadyGrace-time.Millisecond) {
		t.Fatal("the sign-in probe must be given its grace before being overruled")
	}
	if !acceptBrowserWorker(false, true, true, browserReadyGrace) {
		t.Fatal("a live, answering worker must be allowed to run the turn once the grace is up")
	}
	if acceptBrowserWorker(false, false, false, 10*browserReadyGrace) {
		t.Fatal("a worker that is not running must never be accepted")
	}
	if acceptBrowserWorker(false, true, false, 10*browserReadyGrace) {
		t.Fatal("a worker that is not answering must never be accepted")
	}
}

// Every provider's readiness wait must report what is missing and honour the
// grace, or one of them silently keeps the old behaviour.
func TestEveryProviderReadinessWaitSaysWhatIsMissing(t *testing.T) {
	for _, file := range []string{
		"chatgpt_webview.go", "claude_chat_webview.go", "copilot_chat_webview.go",
		"gemini_chat_webview.go", "grok_chat_webview.go", "muse_chat_webview.go",
	} {
		src := readGoSource(t, file)
		for _, want := range []string{"acceptBrowserWorker(", "noteBrowserWorkerWaiting(", "noteBrowserWorkerAccepted("} {
			if !strings.Contains(src, want) {
				t.Errorf("%s does not call %s, so a turn it refuses cannot say why", file, want)
			}
		}
	}
}
