package main

import (
	"os"
	"strings"
	"testing"
)

func TestBrowserConnectionTestsNeverSendSyntheticPrompts(t *testing.T) {
	files := []string{
		"chatgpt_webview_windows.go",
		"claude_chat_webview_windows.go",
		"gemini_chat_webview_windows.go",
		"grok_chat_webview_windows.go",
		"copilot_chat_webview_windows.go",
		"muse_chat_webview_windows.go",
	}
	forbidden := []string{
		`turn(rw, r, "Reply with exactly: FLIPAI_OK"`,
		`turn(rw,r,"Reply with exactly: FLIPAI_OK"`,
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		s := string(raw)
		for _, marker := range forbidden {
			if strings.Contains(s, marker) {
				t.Fatalf("%s still sends a synthetic FLIPAI_OK prompt from its test endpoint", path)
			}
		}
		if !strings.Contains(s, `HandleFunc("/test"`) {
			t.Fatalf("%s lost its test endpoint", path)
		}
	}
}
