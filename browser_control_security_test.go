package main

import (
	"os"
	"strings"
	"testing"
)

func TestBrowserControlEndpointsStayPrivate(t *testing.T) {
	files := []string{
		"chatgpt_webview_windows.go",
		"claude_chat_webview_windows.go",
		"gemini_chat_webview_windows.go",
		"grok_chat_webview_windows.go",
	}
	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			for _, want := range []string{
				`net.Listen("tcp", "127.0.0.1:0")`,
				`secureRandomToken(24)`,
				`Header.Get("X-FlipAi-Token")`,
				`http.MaxBytesReader`,
			} {
				if !strings.Contains(text, want) {
					t.Fatalf("%s lost browser-control security invariant %q", path, want)
				}
			}
			for _, forbidden := range []string{
				`net.Listen("tcp", ":0")`,
				`net.Listen("tcp", "0.0.0.0`,
				`--remote-debugging-port`,
				`--remote-allow-origins=*`,
			} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("%s exposes a browser-control surface through %q", path, forbidden)
				}
			}
		})
	}
}
