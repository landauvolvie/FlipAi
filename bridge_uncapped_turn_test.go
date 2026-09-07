package main

import (
	"os"
	"strings"
	"testing"
)

// The bridge itself must never impose an elapsed-time deadline on an agent
// turn. Provider-specific probes may have short checkpoints, but a real model
// task can legitimately keep running until it completes, fails, or FlipAi is
// actually shutting down.
func TestBridgeExecuteHasNoHardTurnDeadline(t *testing.T) {
	src, err := os.ReadFile("bridge.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	start := strings.Index(text, "func (b *Bridge) execute(")
	end := strings.Index(text[start:], "\n// heartbeat texts")
	if start < 0 || end < 0 {
		t.Fatal("could not locate Bridge.execute in bridge.go")
	}
	execute := text[start : start+end]

	for _, forbidden := range []string{
		"TurnTimeoutMinutes",
		"context.WithTimeout(parent",
		"90 * time.Minute",
	} {
		if strings.Contains(execute, forbidden) {
			t.Fatalf("Bridge.execute contains hard turn deadline marker %q", forbidden)
		}
	}
	if !strings.Contains(execute, "context.WithCancel(parent)") {
		t.Fatal("Bridge.execute must inherit cancellation from the app without adding an elapsed-time deadline")
	}
}
