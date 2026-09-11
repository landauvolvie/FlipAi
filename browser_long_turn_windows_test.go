//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBrowserLongTurnProviderFromTurnExpressions(t *testing.T) {
	cases := []struct {
		expression string
		want       string
	}{
		{fmt.Sprintf(chatGPTTurnJS, chatGPTJSString("do a long task")), "chatgpt"},
		{fmt.Sprintf(claudeChatTurnJS, claudeChatJSString("do a long task")), "claude-chat"},
		{fmt.Sprintf(geminiChatTurnJS, geminiChatJSString("do a long task")), "gemini"},
		{fmt.Sprintf(grokChatTurnJS, grokChatJSString("do a long task")), "grok"},
		{fmt.Sprintf(copilotChatTurnJS, copilotChatJSString("do a long task")), "copilot"},
	}
	for _, tc := range cases {
		if got := browserLongTurnProviderFromExpression(tc.expression); got != tc.want {
			t.Fatalf("provider = %q, want %q", got, tc.want)
		}
	}
}

func TestBrowserTurnValueFromDevToolsRecognizesNinetySecondCheckpoint(t *testing.T) {
	pageValue, _ := json.Marshal(browserTurnResultValue{OK: false, Detail: "ChatGPT did not produce an assistant response within 90 seconds."})
	envelope, _ := json.Marshal(map[string]any{"result": map[string]any{"value": json.RawMessage(pageValue)}})
	value, ok := browserTurnValueFromDevTools(string(envelope))
	if !ok {
		t.Fatal("could not decode DevTools page value")
	}
	if value.OK || !browserLongTurnTimeoutDetail(value.Detail) {
		t.Fatalf("decoded value = %#v, want timeout checkpoint", value)
	}
}

func TestBrowserLongTurnSnapshotDoesNotScrapeThoughtOrProgressUI(t *testing.T) {
	for _, forbidden := range []string{
		`[role="status"]`,
		`[aria-live="polite"]`,
		`[data-testid*="status" i]`,
		`[class*="progress" i]`,
		`progressNodes`,
	} {
		if strings.Contains(browserLongTurnSnapshotJS, forbidden) {
			t.Fatalf("long-turn snapshot still scrapes progress UI via %q", forbidden)
		}
	}
}
