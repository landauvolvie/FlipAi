//go:build windows

package main

import (
	"fmt"
	"testing"
)

func TestWebViewDevToolsCallTimeoutKeepsVoiceShortAndChatGPTLong(t *testing.T) {
	ordinary := map[string]any{
		"expression":    "(()=>true)()",
		"returnByValue": true,
		"awaitPromise":  true,
	}
	if got := webViewDevToolsCallTimeout("Runtime.evaluate", ordinary); got != voiceDevToolsTimeout {
		t.Fatalf("ordinary WebView eval timeout = %v, want %v", got, voiceDevToolsTimeout)
	}

	turn := map[string]any{
		"expression":    fmt.Sprintf(chatGPTTurnJS, chatGPTJSString("hello")),
		"returnByValue": true,
		"awaitPromise":  true,
	}
	if got := webViewDevToolsCallTimeout("Runtime.evaluate", turn); got != chatGPTTurnDevToolsTimeout {
		t.Fatalf("ChatGPT turn timeout = %v, want %v", got, chatGPTTurnDevToolsTimeout)
	}
}
