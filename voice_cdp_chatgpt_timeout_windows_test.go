//go:build windows

package main

import (
	"fmt"
	"testing"
)

func TestWebViewDevToolsCallTimeoutKeepsVoiceShortAndBrowserChatsLong(t *testing.T) {
	ordinary := map[string]any{
		"expression":    "(()=>true)()",
		"returnByValue": true,
		"awaitPromise":  true,
	}
	if got := webViewDevToolsCallTimeout("Runtime.evaluate", ordinary); got != voiceDevToolsTimeout {
		t.Fatalf("ordinary WebView eval timeout = %v, want %v", got, voiceDevToolsTimeout)
	}

	turns := map[string]string{
		"ChatGPT": fmt.Sprintf(chatGPTTurnJS, chatGPTJSString("hello")),
		"Gemini":  fmt.Sprintf(geminiChatTurnJS, geminiChatJSString("hello")),
		"Grok":    fmt.Sprintf(grokChatTurnJS, grokChatJSString("hello")),
	}
	for name, expression := range turns {
		t.Run(name, func(t *testing.T) {
			turn := map[string]any{
				"expression":    expression,
				"returnByValue": true,
				"awaitPromise":  true,
			}
			if got := webViewDevToolsCallTimeout("Runtime.evaluate", turn); got != chatGPTTurnDevToolsTimeout {
				t.Fatalf("%s turn timeout = %v, want %v", name, got, chatGPTTurnDevToolsTimeout)
			}
		})
	}
}
