package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBrowserLongTurnTimeoutDetail(t *testing.T) {
	for _, detail := range []string{
		"ChatGPT did not produce an assistant response within 90 seconds.",
		"Gemini started answering but did not finish within 90 seconds.",
		"Grok did not produce a new response within 90 seconds.",
	} {
		if !browserLongTurnTimeoutDetail(detail) {
			t.Fatalf("expected legacy checkpoint detail to be recognized: %q", detail)
		}
	}
	if browserLongTurnTimeoutDetail("Network error. Please try again.") {
		t.Fatal("a real provider error must not be treated as a long-turn checkpoint")
	}
}

func TestBrowserProgressIsNeverForwarded(t *testing.T) {
	for _, in := range []string{
		"Thinking…",
		"Some chrome\nGenerating your image",
		"Finishing up the report",
		strings.Repeat("Thinking through every private intermediate detail. ", 20),
	} {
		if got := sanitizeBrowserProgress(in); got != "" {
			t.Fatalf("sanitizeBrowserProgress(%q) = %q, want empty", in, got)
		}
	}
}

func TestWaitForBrowserLongTurnReturnsFinalWithoutProgressCallbacks(t *testing.T) {
	dataDir := t.TempDir()
	provider := "G"
	if err := saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnPending, Progress: "Thinking…"}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnPending, Progress: "Finishing up…"})
		time.Sleep(40 * time.Millisecond)
		_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnDone, Reply: "Finished result"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var progress []string
	reply, err := waitForBrowserLongTurn(ctx, dataDir, provider, func(v string) { progress = append(progress, v) })
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if reply != "Finished result" {
		t.Fatalf("reply = %q, want Finished result", reply)
	}
	if len(progress) != 0 {
		t.Fatalf("progress callbacks = %#v, want none", progress)
	}
	state, err := loadBrowserLongTurnState(dataDir, provider)
	if err != nil {
		t.Fatal(err)
	}
	if state.Progress != "" {
		t.Fatalf("persisted progress = %q, want empty", state.Progress)
	}
}

func TestWaitForBrowserLongTurnFailsOnlyFromFailedState(t *testing.T) {
	dataDir := t.TempDir()
	if err := saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: "M", Status: browserLongTurnFailed, Detail: "Network error"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := waitForBrowserLongTurn(ctx, dataDir, "M", nil); err == nil || !strings.Contains(err.Error(), "Network error") {
		t.Fatalf("failed state returned %v, want Network error", err)
	}
}
