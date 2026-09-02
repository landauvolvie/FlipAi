package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatGPTV04612SignedInStateMigratesToDurableConnection(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"running":false,"visible":false,"signedIn":true,"conversationId":"old-chat"}`
	if err := os.WriteFile(chatGPTRuntimePath(dir), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s := loadChatGPTRuntime(dir)
	if !s.Connected {
		t.Fatalf("v0.46.12 signed-in profile was not migrated to durable Connected state: %+v", s)
	}
	if s.ConversationID != "old-chat" {
		t.Fatalf("migration lost conversation id: %+v", s)
	}
}

func TestChatGPTTemporaryPageSignOutDoesNotForgetSavedConnection(t *testing.T) {
	dir := t.TempDir()
	mutateChatGPTRuntime(dir, func(s *ChatGPTWebRuntime) {
		s.Connected = true
		s.SignedIn = true
	})
	mutateChatGPTRuntime(dir, func(s *ChatGPTWebRuntime) {
		s.SignedIn = false
		s.LastEvent = "session-restoring"
	})
	s := loadChatGPTRuntime(dir)
	if !s.Connected {
		t.Fatalf("loading/restart transition forgot the saved ChatGPT connection: %+v", s)
	}
	if s.SignedIn {
		t.Fatalf("live SignedIn should still describe the current page, got %+v", s)
	}
}

// This is the exact race visible in the user's v0.46.12 Activity log: the
// private control server was ready, the first health answer was signed-out, and
// the saved session became signed-in only a fraction of a second later. A turn
// must wait through those early false answers instead of returning an instant
// "not signed in" failure.
func TestWaitForChatGPTReadyWaitsThroughHiddenWebViewStartup(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	const token = "test-private-control-token"
	var probes atomic.Int32
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-FlipAi-Token") != token {
			http.Error(w, "bad token", http.StatusForbidden)
			return
		}
		n := probes.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "signedIn": n >= 4})
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	dir := t.TempDir()
	port := ln.Addr().(*net.TCPAddr).Port
	mutateChatGPTRuntime(dir, func(s *ChatGPTWebRuntime) {
		s.Connected = true
		s.Running = true
		s.ControlPort = port
		s.ControlToken = token
		s.SignedIn = false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	started := time.Now()
	got, err := waitForChatGPTReady(ctx, dir)
	if err != nil {
		t.Fatalf("readiness wait failed instead of surviving startup race: %v", err)
	}
	if probes.Load() < 4 {
		t.Fatalf("readiness returned before the simulated saved session was restored; probes=%d", probes.Load())
	}
	if !got.Connected || !got.SignedIn {
		t.Fatalf("restored session was not recorded ready: %+v", got)
	}
	if time.Since(started) < 500*time.Millisecond {
		t.Fatalf("readiness did not actually wait through false health answers: %s", time.Since(started))
	}
}

func TestChatGPTAgentsPaneKeepsPersistentConnectionStatusAvailable(t *testing.T) {
	body := chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))
	for _, want := range []string{
		"private persistent browser session", "Saved connection", "Live session",
		"Connection details", "Restoring",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("persistent ChatGPT UI missing %q", want)
		}
	}
}
