package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A new-conversation command has to end the running session as well as clear
// the stored ids. Without that the next text lands in the old session and the
// "new conversation" the user was promised never happened.
func TestStartNewClaudeSessionResetsLiveSession(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	b := NewBridge(defaultConfig(dir), statePath, State{
		ClaudeSessionID:     "old-print-session",
		ClaudeLiveSessionID: "old-live-session",
	}, nil, nil, nil)

	live := NewClaudeLiveClient("claude", "", ClaudeConfig{}, "", "hook")
	live.cmd = &exec.Cmd{}
	live.socket = "/tmp/whatever"
	live.sessionID = "old-live-session"
	b.SetLiveClaude(live)

	name := b.startNewClaudeSession()
	if name == "" {
		t.Fatal("a new conversation must be given a name")
	}
	b.mu.Lock()
	gotPrint, gotLive := b.state.ClaudeSessionID, b.state.ClaudeLiveSessionID
	b.mu.Unlock()
	if gotPrint != "" || gotLive != "" {
		t.Errorf("both session ids must be cleared, got print=%q live=%q", gotPrint, gotLive)
	}
	if live.Running() {
		t.Error("the supervised live session must be stopped by a new-conversation command")
	}

	// The cleared state has to survive a crash before the next turn, exactly as
	// it already does for the per-message conversation.
	reloaded := loadState(statePath)
	if reloaded.ClaudeLiveSessionID != "" {
		t.Errorf("cleared live session id was not persisted, got %q", reloaded.ClaudeLiveSessionID)
	}
}

// runClaudeLive is the fallback gate. Anything short of a genuine Claude error
// has to report "not handled" so the caller runs the text through per-message
// mode instead of dropping it.
func TestRunClaudeLiveReportsFallback(t *testing.T) {
	b := NewBridge(defaultConfig(t.TempDir()), filepath.Join(t.TempDir(), "state.json"), State{}, nil, nil, nil)

	t.Run("no live client", func(t *testing.T) {
		if _, ok := b.runClaudeLive(context.Background(), "p", "s", "n"); ok {
			t.Error("with no live client the turn must fall back")
		}
	})

	t.Run("unreachable inbox", func(t *testing.T) {
		// An injected failing transport rather than a dead path, so the test
		// asserts the fallback rather than the platform's dial timeout.
		live := newFailingLiveClient(t)
		b.SetLiveClaude(live)

		if _, ok := b.runClaudeLive(context.Background(), "p", "s", "n"); ok {
			t.Error("an undeliverable SMS must fall back rather than be reported as answered")
		}
	})
}

func newHookTestApp(t *testing.T, token string, live *ClaudeLiveClient) *App {
	t.Helper()
	return &App{dataDir: t.TempDir(), hookToken: token, liveClaude: live}
}

// The hook endpoint is the one route a non-browser process may call, so its
// secret is the only thing stopping a local process from posting a fabricated
// reply that FlipAi would then text to the user's phone.
func TestClaudeHookEndpointRequiresItsSecret(t *testing.T) {
	app := newHookTestApp(t, "right-secret", nil)
	body := `{"hook_event_name":"Stop","prompt_id":"p1","last_assistant_message":"hi"}`

	for _, tc := range []struct {
		name, header string
		want         int
	}{
		{"correct secret", "right-secret", http.StatusNoContent},
		{"wrong secret", "wrong-secret", http.StatusForbidden},
		{"absent secret", "", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, claudeLiveHookPath, strings.NewReader(body))
			if tc.header != "" {
				r.Header.Set(claudeHookHeader, tc.header)
			}
			w := httptest.NewRecorder()
			app.claudeHookEndpoint(w, r)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}

	t.Run("GET is refused", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, claudeLiveHookPath, nil)
		r.Header.Set(claudeHookHeader, "right-secret")
		w := httptest.NewRecorder()
		app.claudeHookEndpoint(w, r)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", w.Code)
		}
	})

	t.Run("an empty secret refuses everything", func(t *testing.T) {
		// newHookToken fails closed rather than minting a guessable value, so
		// this is the state after a randomness failure.
		blank := newHookTestApp(t, "", nil)
		r := httptest.NewRequest(http.MethodPost, claudeLiveHookPath, strings.NewReader(body))
		w := httptest.NewRecorder()
		blank.claudeHookEndpoint(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})
}

// The hook helper's job is to add the two messaging values Claude Code exports
// only into its own environment, and to preserve every field it was given.
func TestClaudeHookPayloadDecodesWhatTheHelperSends(t *testing.T) {
	// Shaped exactly like what runClaudeHookCommand posts.
	forwarded := map[string]any{
		"hook_event_name":       claudeHookSessionStart,
		"session_id":            "abc123",
		"cwd":                   `C:\projects`,
		"some_future_field":     "ignored without failing",
		"flipaiMessagingSocket": `\\.\pipe\claude-abc`,
		"flipaiMessagingToken":  "inbox-token",
	}
	raw, err := json.Marshal(forwarded)
	if err != nil {
		t.Fatal(err)
	}
	var p claudeHookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("payload must decode even with unknown fields present: %v", err)
	}
	if p.Event != claudeHookSessionStart || p.SessionID != "abc123" {
		t.Errorf("decoded event/session = %q/%q", p.Event, p.SessionID)
	}
	if p.Socket != `\\.\pipe\claude-abc` || p.Token != "inbox-token" {
		t.Errorf("messaging values were lost: socket=%q token=%q", p.Socket, p.Token)
	}
}

// Print mode is the default and the fallback, so a config that says nothing
// about session mode must never start supervising a process.
func TestDefaultConfigStaysInPrintMode(t *testing.T) {
	dir := t.TempDir()
	if got := normalizeClaudeSessionMode(defaultConfig(dir).Claude.SessionMode); got != claudeSessionModePrint {
		t.Errorf("default session mode = %q, want %q", got, claudeSessionModePrint)
	}
	app := &App{dataDir: dir}
	live, support := app.newClaudeLiveClient(context.Background(), defaultConfig(dir))
	if live != nil {
		t.Error("print mode must not build a live client")
	}
	if support.OK {
		t.Error("print mode must not report live support")
	}
}

// The Agents page has to offer the mode and, when live mode is refused or
// degraded, say so. A silent fallback would leave the page claiming a browser
// view the user will never find.
func TestAgentsPageOffersSessionMode(t *testing.T) {
	a := newTestApp(t)
	body := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	for _, want := range []string{
		`name="claudeSessionMode"`,
		`value="print"`,
		`value="live"`,
		"Live session with Remote Control",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Agents page is missing %q", want)
		}
	}
	// Per-message is the default, so it must be the selected option.
	if !strings.Contains(body, `<option value="print" selected>`) {
		t.Error("per-message mode must be preselected on a fresh install")
	}
}

func TestAgentsSaveRoundTripsSessionMode(t *testing.T) {
	a := newTestApp(t)
	form := url.Values{}
	form.Set("claudePrefix", a.cfg.ClaudePrefix)
	form.Set("codexPrefix", a.cfg.CodexPrefix)
	form.Set("newSessionCommand", a.cfg.NewSessionCommand)
	form.Set("claudeSessionMode", "live")
	if rr := a.do(t, http.MethodPost, "/agents/save", form); rr.Code >= 400 {
		t.Fatalf("saving the Agents form failed: %d %s", rr.Code, rr.Body.String())
	}
	if got := a.reloadConfig(t).Claude.SessionMode; got != claudeSessionModeLive {
		t.Errorf("saved session mode = %q, want %q", got, claudeSessionModeLive)
	}

	// An unrecognised value must normalise rather than persist.
	form.Set("claudeSessionMode", "channels")
	if rr := a.do(t, http.MethodPost, "/agents/save", form); rr.Code >= 400 {
		t.Fatalf("saving the Agents form failed: %d %s", rr.Code, rr.Body.String())
	}
	if got := a.reloadConfig(t).Claude.SessionMode; got != claudeSessionModePrint {
		t.Errorf("an unknown mode must fall back to per-message, got %q", got)
	}
}

// A live-mode caveat has to reach the page, since that is where the user finds
// out the browser view is not coming.
func TestAgentsPageShowsLiveCaveat(t *testing.T) {
	a := newTestApp(t)
	cfg := a.cfg
	cfg.Claude.SessionMode = claudeSessionModeLive
	a.cfg = cfg
	a.liveSupport = claudeLiveSupport{OK: true, Reason: "Remote Control cannot use the stored Claude token"}

	body := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	if !strings.Contains(body, "Remote Control cannot use the stored Claude token") {
		t.Error("the Agents page must explain why live mode is degraded")
	}
}

// A vanished per-message transcript says nothing about the live session, so the
// recovery path must not tear down the session the user may be watching in the
// browser. Only the explicit new-conversation command does that.
func TestResetPrintSessionLeavesLiveSessionAlone(t *testing.T) {
	dir := t.TempDir()
	b := NewBridge(defaultConfig(dir), filepath.Join(dir, "state.json"), State{
		ClaudeSessionID:     "dead-print-session",
		ClaudeLiveSessionID: "healthy-live-session",
	}, nil, nil, nil)

	live := NewClaudeLiveClient("claude", "", ClaudeConfig{}, "", "hook")
	live.cmd = &exec.Cmd{}
	live.socket = "/tmp/whatever"
	b.SetLiveClaude(live)

	if name := b.resetPrintClaudeSession(); name == "" {
		t.Fatal("the replacement per-message conversation must be named")
	}
	b.mu.Lock()
	gotPrint, gotLive := b.state.ClaudeSessionID, b.state.ClaudeLiveSessionID
	b.mu.Unlock()
	if gotPrint != "" {
		t.Errorf("the dead per-message id must be cleared, got %q", gotPrint)
	}
	if gotLive != "healthy-live-session" {
		t.Errorf("the live session id must survive, got %q", gotLive)
	}
	if !live.Running() {
		t.Error("the live session must keep running when only the per-message transcript vanished")
	}
}
