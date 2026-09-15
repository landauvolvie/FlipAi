package main

import (
	"strings"
	"testing"
)

// ChatGPT worked, and then one release later it stopped and stayed stopped,
// while the same build's other agents were fine. The logs show why: from that
// build on, a DevTools call was no longer reported as failed the moment it was
// issued, so a navigation's call waited for a completion handler that the
// navigation itself had destroyed -- burning the caller's deadline and then
// holding the browser's single protocol channel while every readiness probe
// after the experience switch queued up behind it and died.
//
// A navigation is handed off, never waited on.
func TestEveryPageNavigationIsHandedOffAndNotWaitedOn(t *testing.T) {
	for _, file := range []string{
		"chatgpt_webview_windows.go",
		"copilot_chat_webview_windows.go",
		"gemini_chat_webview_windows.go",
		"grok_chat_webview_windows.go",
		"muse_chat_webview_windows.go",
	} {
		source := readGoSource(t, file)
		if strings.Contains(source, "location.href='http") {
			t.Errorf("%s navigates with a raw expression; use browserPageNavigateJS so the call is handed off instead of awaited", file)
		}
		if !strings.Contains(source, "browserPageNavigateJS") {
			t.Errorf("%s never navigates through browserPageNavigateJS", file)
		}
	}

	dispatcher := readGoSource(t, "voice_cdp_windows.go")
	if !strings.Contains(dispatcher, "isBrowserPageNavigateExpression") {
		t.Fatal("the DevTools layer does not recognize a navigation, so it still waits for a callback the navigation destroyed")
	}
	if !strings.Contains(dispatcher, "d.handOff(method, body)") {
		t.Fatal("a navigation is not handed off")
	}
}

func TestBrowserPageNavigateJSCarriesTheMarker(t *testing.T) {
	expr := browserPageNavigateJS("https://chatgpt.com/")
	if !isBrowserPageNavigateExpression(expr) {
		t.Fatalf("navigation expression is not recognizable: %q", expr)
	}
	if !strings.Contains(expr, "location.href='https://chatgpt.com/'") {
		t.Fatalf("navigation expression does not navigate: %q", expr)
	}
	if isBrowserPageNavigateExpression("(()=>document.title)()") {
		t.Fatal("an ordinary probe was mistaken for a navigation")
	}
}

// A driver must never climb out of the conversation while lifting a matched
// block to its message. When the match was the conversation box itself, the
// walk ran to <html> and the whole page was texted as the answer.
func TestDriversNeverLiftPastTheConversationBox(t *testing.T) {
	for _, file := range []string{
		"chatgpt_webview_windows.go",
		"copilot_chat_webview_windows.go",
		"muse_chat_webview_windows.go",
	} {
		source := readGoSource(t, file)
		if !strings.Contains(source, "if(!n||!box||n===box||!box.contains(n))return n;") {
			t.Errorf("%s can lift a match past the conversation box", file)
		}
		if !strings.Contains(source, "dropContainers") {
			t.Errorf("%s keeps a named match that wraps every message, so the whole thread can be sent as one reply", file)
		}
		if !strings.Contains(source, "newestPart") {
			t.Errorf("%s has no guard against sending the conversation instead of this turn's answer", file)
		}
	}
}
