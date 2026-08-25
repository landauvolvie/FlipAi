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
