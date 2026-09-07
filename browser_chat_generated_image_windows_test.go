//go:build windows

package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGeneratedImageTurnExpressionIsRecognized(t *testing.T) {
	expression := fmt.Sprintf(chatGPTTurnJS, chatGPTJSString("Generate me a picture of flying birds"))
	if !isBrowserChatTurnExpression(expression) {
		t.Fatal("expected ChatGPT browser turn expression")
	}
	if !browserChatPromptRequestsGeneratedImage(expression) {
		t.Fatal("expected generated-image request to survive inside browser expression")
	}
}

func TestBrowserReturnedMediaExpressionUsesRequestedWait(t *testing.T) {
	expression := browserChatReturnedMediaExpression(2 * time.Minute)
	if !strings.Contains(expression, browserChatReturnedMediaMarker) {
		t.Fatal("media collector marker missing")
	}
	if !strings.Contains(expression, "const until=Date.now()+120000;") {
		t.Fatal("media collector did not receive the long wait")
	}
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}
	if got := webViewDevToolsCallTimeout("Runtime.evaluate", params); got != browserChatReturnedMediaDevToolsTimeout {
		t.Fatalf("media collector timeout = %v, want %v", got, browserChatReturnedMediaDevToolsTimeout)
	}
}
