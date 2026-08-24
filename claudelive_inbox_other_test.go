//go:build !windows

package main

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The transport itself, against a real listener. This covers what the injected
// fake in claudelive_test.go deliberately does not: that writeClaudeInbox
// connects, writes the whole frame, and closes.
//
// It is limited to non-Windows because Windows delivers over a named pipe, and
// Go has no standard-library named-pipe listener to test it against. The
// Windows transport is therefore exercised only on a real machine; everything
// above it is covered on every platform.
func TestWriteClaudeInboxDeliversToARealListener(t *testing.T) {
	addr := filepath.Join(t.TempDir(), "inbox.sock")
	ln, err := net.Listen("unix", addr)
	if err != nil {
		t.Skipf("unix sockets unavailable here: %v", err)
	}
	defer ln.Close()

	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64<<10)
		n, _ := conn.Read(buf)
		got <- string(buf[:n])
	}()

	frame, err := claudeInboxFrame("tok", "FlipAi SMS", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeInbox("uds:"+addr, frame); err != nil {
		t.Fatalf("writeClaudeInbox: %v", err)
	}

	select {
	case delivered := <-got:
		if delivered != string(frame) {
			t.Errorf("delivered %q, want %q", delivered, frame)
		}
		if !strings.HasPrefix(delivered, `{"token"`) && !strings.HasPrefix(delivered, `{"type":"auth"`) {
			t.Errorf("the auth line must come first, got %q", delivered)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was delivered to the listener")
	}
}

func TestWriteClaudeInboxRejectsAnEmptyAddress(t *testing.T) {
	if err := writeClaudeInbox("  ", []byte("x")); err == nil {
		t.Error("an empty inbox address must be refused rather than dialled")
	}
}
