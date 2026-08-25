//go:build windows

package main

import "testing"

func TestVoiceAppWindowSearchIgnoresBrowserTabs(t *testing.T) {
	// A browser tab open on the agent's website carries the same words as the
	// desktop app's own window. Driving the browser would silently do nothing.
	for _, title := range []string{
		"ChatGPT - Google Chrome",
		"Claude — Mozilla Firefox",
		"ChatGPT - Microsoft Edge",
	} {
		if !browserWindowTitle(title) {
			t.Errorf("%q was not recognized as a browser window", title)
		}
	}
	for _, title := range []string{"ChatGPT", "Claude", "Claude Desktop"} {
		if browserWindowTitle(title) {
			t.Errorf("%q was mistaken for a browser window", title)
		}
	}
}

func TestOnlyOneHostAndOneWatchdogCanRun(t *testing.T) {
	// Two hosts mean two mailbox pollers, each with its own record of what it
	// has already handled, so one SMS reaches the agent twice.
	release, owner, err := acquireHostInstance()
	if err != nil {
		t.Fatalf("acquiring the host instance failed: %v", err)
	}
	if !owner {
		t.Skip("a FlipAi host is already running on this machine")
	}
	defer release()

	if _, second, err := acquireHostInstance(); err != nil || second {
		t.Fatalf("a second host claimed ownership (owner=%v, err=%v)", second, err)
	}

	// Releasing has to hand ownership back, or a restarted host could never
	// start again.
	release()
	again, owner, err := acquireHostInstance()
	if err != nil || !owner {
		t.Fatalf("ownership was not released (owner=%v, err=%v)", owner, err)
	}
	again()
}

func TestFlipAiWindowLookupIgnoresTheTrayHelperWindow(t *testing.T) {
	// The tray process owns a hidden window with the very same title as the
	// desktop window. A title-only search matched that one, so opening FlipAi
	// restored an empty frame and never created the real window.
	if flipAiWindowClass != "webview" {
		t.Fatalf("window class = %q; it must match the class the WebView binding registers", flipAiWindowClass)
	}
	if flipAiWindowClass == "" || flipAiWindowTitle == "" {
		t.Fatal("the window search must be scoped by both class and title")
	}

	// Nothing of this package's own is running, so the lookup must find nothing
	// rather than latch onto some other window that happens to be called FlipAi.
	if h := flipAiWindowHWND(); h != 0 {
		t.Fatalf("found a FlipAi window (%d) with none open", h)
	}
	if platformFlipAiWindowOpen() {
		t.Fatal("reported a FlipAi window open with none open")
	}
}
