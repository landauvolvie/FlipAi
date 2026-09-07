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
	if start < 0 {
		t.Fatal("could not locate Bridge.execute in bridge.go")
	}
	// Only inspect the agent-turn portion of execute. Final Google Voice
	// delivery intentionally has its own short network timeout so a completed
	// model turn cannot hang forever while sending the reply.
	relEnd := strings.Index(text[start:], "\n\t// Delivery is unconditional")
	if relEnd < 0 {
		t.Fatal("could not locate the delivery boundary in Bridge.execute")
	}
	executeTurn := text[start : start+relEnd]

	for _, forbidden := range []string{
		"TurnTimeoutMinutes",
		"context.WithTimeout(parent",
		"90 * time.Minute",
	} {
		if strings.Contains(executeTurn, forbidden) {
			t.Fatalf("Bridge.execute agent-turn section contains hard turn deadline marker %q", forbidden)
		}
	}
	if !strings.Contains(executeTurn, "context.WithCancel(parent)") {
		t.Fatal("Bridge.execute must inherit cancellation from the app without adding an elapsed-time deadline")
	}
}
