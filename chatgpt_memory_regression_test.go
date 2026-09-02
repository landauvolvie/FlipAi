package main

import (
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatGPTBrowserStillOpenDoesNotRunAuthenticatedPageHealth(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var authenticated atomic.Bool
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-FlipAi-Token") != "" {
			authenticated.Store(true)
			// Model a busy Runtime.evaluate call. The liveness probe must never
			// enter this path or it recreates the duplicate-WebView regression.
			time.Sleep(900 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "FlipAi token required", http.StatusForbidden)
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	mutateChatGPTRuntime(dir, func(s *ChatGPTWebRuntime) {
		s.Running = true
		s.ControlPort = port
		s.ControlToken = "secret"
	})

	started := time.Now()
	if !chatGPTBrowserStillOpen(dir) {
		t.Fatal("live ChatGPT worker was reported dead")
	}
	if elapsed := time.Since(started); elapsed > 400*time.Millisecond {
		t.Fatalf("liveness probe took %v; it should not wait on the renderer", elapsed)
	}
	if authenticated.Load() {
		t.Fatal("liveness probe used the authenticated health path and could run page JavaScript")
	}
}
