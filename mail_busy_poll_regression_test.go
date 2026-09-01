package main

import (
	"context"
	"testing"
	"time"
)

// Agent work can take minutes. The mailbox reader must keep detecting and
// queueing texts during that time instead of waiting for the current turn to
// finish and then for the fallback poll.
func TestMailboxCheckContinuesWhileAgentIsBusy(t *testing.T) {
	m := &pollMail{}
	cfg := defaultConfig(t.TempDir())
	b := NewBridge(cfg, t.TempDir()+"/state.json", State{GmailBaselineUnix: time.Now().Unix()}, m, nil, nil)
	b.mu.Lock()
	b.busy = true
	b.mu.Unlock()

	b.poll(context.Background())
	if got := m.count(); got != 1 {
		t.Fatalf("busy agent blocked Gmail mailbox check; List called %d times, want 1", got)
	}
}
