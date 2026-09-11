package main

import (
	"os"
	"strings"
	"testing"
)

func TestBridgeQueueDepthIsProviderLocal(t *testing.T) {
	b := NewBridge(Config{}, "", State{}, nil, nil, nil)
	if got := b.enqueue(bridgeJob{cmd: remoteCommand{Agent: "X"}}); got != 1 {
		t.Fatalf("first Grok depth = %d, want 1", got)
	}
	if got := b.enqueue(bridgeJob{cmd: remoteCommand{Agent: "G"}}); got != 1 {
		t.Fatalf("first ChatGPT depth = %d, want 1; another provider must not count as ahead", got)
	}
	if got := b.enqueue(bridgeJob{cmd: remoteCommand{Agent: "X"}}); got != 2 {
		t.Fatalf("second Grok depth = %d, want 2", got)
	}
	if b.agentMutex("X") == b.agentMutex("G") {
		t.Fatal("different providers unexpectedly share one execution mutex")
	}
	if b.agentMutex("G") != b.agentMutex("G") {
		t.Fatal("same provider did not reuse its execution mutex")
	}
}

func TestLiveBridgeUsesProviderIsolatedDispatcher(t *testing.T) {
	raw, err := os.ReadFile("bridge.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"b.dispatchQueue(ctx)", "b.dispatchQueuedJob(ctx, j)", "b.agentMutex(rc.Agent)", "pendingByAgent"} {
		if !strings.Contains(s, want) {
			t.Fatalf("bridge.go lost provider-isolation marker %q", want)
		}
	}
	for _, forbidden := range []string{"b.runMu.Lock()", "line += \" \" + truncate(step"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("bridge.go still contains global/intermediate behavior %q", forbidden)
		}
	}
}
