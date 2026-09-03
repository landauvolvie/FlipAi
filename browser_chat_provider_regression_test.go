package main

import (
	"os"
	"strings"
	"testing"
)

func TestGeminiWebViewPreservesMultilineSMSPrompt(t *testing.T) {
	b, err := os.ReadFile("gemini_chat_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"split('\\n')", "insertLineBreak", "data:input"} {
		if !strings.Contains(s, want) {
			t.Fatalf("Gemini WebView is missing multiline prompt safeguard %q", want)
		}
	}
}

func TestGrokWebViewUsesGrokSignInAndMutableResponseDetection(t *testing.T) {
	b, err := os.ReadFile("grok_chat_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"div.ProseMirror", "loginPage", "beforeCount", "beforeLast", "offsetParent!==null"} {
		if !strings.Contains(s, want) {
			t.Fatalf("Grok WebView is missing regression safeguard %q", want)
		}
	}
	if strings.Contains(s, "const grokChatSignedInJS = `(()=>{const c=document.querySelector('rich-textarea") {
		t.Fatal("Grok signed-in probe regressed to Gemini's rich-textarea selector")
	}
}
