package main

import (
	"os"
	"strings"
	"testing"
)

// A disconnect/reconnect replaces Grok's private WebView worker. An in-flight
// turn must be bound to the worker that accepted it; otherwise the old request
// can remain stuck forever while FlipAi keeps sending heartbeat texts.
func TestGrokTurnCancelsWhenBrowserWorkerChanges(t *testing.T) {
	raw, err := os.ReadFile("sms_grok_chat.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, want := range []string{
		"now.ControlPort != port",
		"now.ControlToken != token",
		"!now.Connected",
		"turnCancel()",
		"Grok Chat was disconnected or restarted while this message was running",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("Grok stale-turn cancellation is missing %q", want)
		}
	}
}
