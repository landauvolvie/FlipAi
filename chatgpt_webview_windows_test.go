//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestChatGPTWebViewUsesDedicatedProfileAndPrivateLoopback(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"DataPath: chatGPTProfilePath(dataDir)",
		`net.Listen("tcp", "127.0.0.1:0")`,
		`r.Header.Get("X-FlipAi-Token")`,
		`--chatgpt-worker`,
		`-30000, -30000`,
		`NoActivate = true`,
		`data-message-author-role=\"assistant\"`,
		`data-testid=\"send-button\"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("ChatGPT WebView implementation lost %q", want)
		}
	}
}

func TestChatGPTWebViewDoesNotUseGlobalUIAutomation(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, forbidden := range []string{
		"sendkeys",
		"uiautomation",
		"setforegroundwindow",
		"input.dispatchmouseevent",
		"--remote-debugging-port",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("ChatGPT WebView must not use global/visible UI automation marker %q", forbidden)
		}
	}
}
