package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// claudeLiveHookPath is the loopback route the hook helper posts to. It sits
// outside the authenticated page routes because its caller is a child process
// rather than the browser window, and it carries its own per-run secret.
const claudeLiveHookPath = "/claude/hook"

// hookURL is the address the hook helper posts back to.
func (a *App) hookURL(cfg Config) string {
	listen := strings.TrimSpace(cfg.Listen)
	if listen == "" {
		listen = "127.0.0.1:8765"
	}
	if strings.HasPrefix(listen, ":") {
		listen = "127.0.0.1" + listen
	}
	return "http://" + listen + claudeLiveHookPath
}

// claudeHookEndpoint receives one hook event from a live session.
//
// It is deliberately strict and quiet: a wrong or missing secret gets 403 with
// no detail, and a payload FlipAi is not waiting for is accepted and dropped.
// Someone typing in the browser view produces exactly that shape many times a
// day, and it is not an error.
func (a *App) claudeHookEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	want, live := a.hookToken, a.liveClaude
	a.mu.Unlock()

	if want == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get(claudeHookHeader)), []byte(want)) != 1 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var p claudeHookPayload
	if json.Unmarshal(raw, &p) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if live != nil && !live.Deliver(p) && p.Event == claudeHookStop {
		// A finished turn FlipAi did not start. Recording it keeps the Activity
		// log honest about a session that has two writers.
		if b := a.currentBridge(); b != nil {
			b.event("info", "agent", "Live Claude session answered a message typed outside SMS", "", "A", "")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// newClaudeLiveClient builds the supervised live-session client, or returns nil
// with the reason when live mode cannot run on this machine.
//
// The preflight is done here, once, at host start rather than per turn: it
// spends a subprocess on `claude --version` and an auth check, and the answer
// cannot change without a restart that this same path runs again.
func (a *App) newClaudeLiveClient(ctx context.Context, cfg Config) (*ClaudeLiveClient, claudeLiveSupport) {
	var support claudeLiveSupport
	if normalizeClaudeSessionMode(cfg.Claude.SessionMode) != claudeSessionModeLive {
		return nil, support
	}
	token, _ := loadClaudeToken(claudeTokenPath(a.dataDir))
	probe := NewClaudeClientWithToken(cfg.ClaudePath, cfg.claudeWorkingDir(), cfg.Claude, token)

	vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	version := probe.Version(vctx)
	cancel()

	actx, acancel := context.WithTimeout(ctx, 30*time.Second)
	st, authErr := probe.authStatus(actx)
	acancel()
	if authErr != nil {
		// An auth check that cannot run is not proof of anything, so fall back
		// rather than guessing. Print mode runs its own check per turn anyway.
		support.Reason = "Live session mode could not check the Claude sign-in on this machine (" +
			truncate(authErr.Error(), 200) + "). FlipAi is using per-message mode."
		return nil, support
	}

	support = evaluateClaudeLiveSupport(version, strings.TrimSpace(token) != "", st)
	if !support.OK {
		return nil, support
	}

	exe, err := os.Executable()
	if err != nil {
		support.OK = false
		support.Reason = "Live session mode could not find the FlipAi executable to run its Claude hook: " + err.Error()
		return nil, support
	}

	a.mu.Lock()
	hookToken := a.hookToken
	a.mu.Unlock()

	hookCmd := claudeHookCommandLine(exe, a.hookURL(cfg), hookToken)
	return NewClaudeLiveClient(cfg.ClaudePath, cfg.claudeWorkingDir(), cfg.Claude, token, hookCmd), support
}

// startClaudeLive attaches live mode to a freshly built bridge, logging the
// outcome either way. A refusal is logged at warn: the user asked for live mode
// in Settings, so silently running something else would be a lie the UI would
// then repeat.
func (a *App) startClaudeLive(ctx context.Context, cfg Config, b *Bridge) {
	live, support := a.newClaudeLiveClient(ctx, cfg)

	a.mu.Lock()
	a.liveClaude, a.liveSupport = live, support
	a.mu.Unlock()

	if normalizeClaudeSessionMode(cfg.Claude.SessionMode) != claudeSessionModeLive {
		return
	}
	if live == nil {
		log.Printf("Claude live session mode unavailable: %s", support.Reason)
		b.event("warn", "agent", "Live session mode unavailable, using per-message mode: "+truncate(support.Reason, 220), "", "A", "")
		return
	}
	b.SetLiveClaude(live)
	if support.RemoteControl {
		log.Printf("Claude live session mode active with Remote Control")
		b.event("success", "agent", "Live session mode active; the SMS session is viewable at claude.ai/code", "", "A", "")
		return
	}
	log.Printf("Claude live session mode active without Remote Control: %s", support.Reason)
	b.event("warn", "agent", "Live session mode active, but without the browser view: "+truncate(support.Reason, 220), "", "A", "")
}

// stopClaudeLive ends the supervised session. The host calls it on shutdown and
// on a settings restart so a mode switch never leaves a session behind holding
// the working folder.
func (a *App) stopClaudeLive() {
	a.mu.Lock()
	live := a.liveClaude
	a.liveClaude = nil
	a.mu.Unlock()
	live.Stop()
}

func (a *App) currentBridge() *Bridge {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bridge
}
