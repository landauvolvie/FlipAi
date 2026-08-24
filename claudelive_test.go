package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNormalizeClaudeSessionMode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"live", claudeSessionModeLive},
		{"LIVE", claudeSessionModeLive},
		{"  Live  ", claudeSessionModeLive},
		{"print", claudeSessionModePrint},
		{"", claudeSessionModePrint},
		{"remote-control", claudeSessionModePrint},
		{"anything else", claudeSessionModePrint},
	} {
		if got := normalizeClaudeSessionMode(tc.in); got != tc.want {
			t.Errorf("normalizeClaudeSessionMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClaudeVersionAtLeast(t *testing.T) {
	for _, tc := range []struct {
		have string
		want bool
	}{
		{"2.1.241 (Claude Code)", true},
		{"2.1.234 (Claude Code)", true},
		{"2.1.233 (Claude Code)", false},
		{"2.0.999", false},
		{"2.2.0", true},
		{"3.0.0", true},
		{"1.9.9", false},
		{"2.1.240-beta.1", true},
		{"", false},
		{"not a version", false},
		{"2.1", false},
	} {
		if got := claudeVersionAtLeast(tc.have, claudeLiveMinVersion); got != tc.want {
			t.Errorf("claudeVersionAtLeast(%q) = %v, want %v", tc.have, got, tc.want)
		}
	}
}

// The support matrix is the whole point of the preflight, so every branch is
// asserted rather than only the happy one.
func TestEvaluateClaudeLiveSupport(t *testing.T) {
	ok := ClaudeAuthStatus{LoggedIn: true, AuthMethod: "claude.ai", ApiProvider: "firstParty"}

	t.Run("version too old blocks live mode entirely", func(t *testing.T) {
		s := evaluateClaudeLiveSupport("2.1.100 (Claude Code)", false, ok)
		if s.OK {
			t.Fatal("live mode must be refused below the minimum version")
		}
		if !strings.Contains(s.Reason, claudeLiveMinVersion) {
			t.Errorf("reason should name the required version, got %q", s.Reason)
		}
	})

	t.Run("unknown version is treated as too old", func(t *testing.T) {
		if evaluateClaudeLiveSupport("", false, ok).OK {
			t.Fatal("an unreadable version must not enable live mode")
		}
	})

	t.Run("API billing is refused", func(t *testing.T) {
		st := ClaudeAuthStatus{LoggedIn: true, AuthMethod: "apiKey", ApiProvider: "firstParty"}
		s := evaluateClaudeLiveSupport("2.1.241", false, st)
		if s.OK {
			t.Fatal("API/Console billing must be refused in live mode as elsewhere")
		}
	})

	t.Run("stored token runs live but without Remote Control", func(t *testing.T) {
		s := evaluateClaudeLiveSupport("2.1.241", true, ok)
		if !s.OK {
			t.Fatal("a stored token must not disable live mode; it only blocks the browser view")
		}
		if s.RemoteControl {
			t.Fatal("a setup-token cannot establish Remote Control")
		}
		if !strings.Contains(s.Reason, "setup-token") {
			t.Errorf("reason should explain the token limitation, got %q", s.Reason)
		}
	})

	t.Run("signed out runs live but without Remote Control", func(t *testing.T) {
		st := ClaudeAuthStatus{LoggedIn: false, ApiProvider: "firstParty"}
		s := evaluateClaudeLiveSupport("2.1.241", false, st)
		if !s.OK || s.RemoteControl {
			t.Fatalf("want live without Remote Control, got OK=%v RC=%v", s.OK, s.RemoteControl)
		}
	})

	t.Run("full-scope login gets everything", func(t *testing.T) {
		s := evaluateClaudeLiveSupport("2.1.241 (Claude Code)", false, ok)
		if !s.OK || !s.RemoteControl {
			t.Fatalf("want OK and Remote Control, got OK=%v RC=%v (%s)", s.OK, s.RemoteControl, s.Reason)
		}
		if s.Reason != "" {
			t.Errorf("a fully working setup should report no caveat, got %q", s.Reason)
		}
	})
}

func TestClaudeLiveSettings(t *testing.T) {
	raw, err := claudeLiveSettings(ClaudeConfig{PermissionMode: "bypassPermissions"}, "hook.exe")
	if err != nil {
		t.Fatalf("claudeLiveSettings: %v", err)
	}
	var got struct {
		CrossSessionInbound string `json:"crossSessionInbound"`
		Permissions         struct {
			DefaultMode string `json:"defaultMode"`
		} `json:"permissions"`
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("settings are not valid JSON: %v\n%s", err, raw)
	}
	// Without this the session holds every SMS for an approval nobody can give.
	if got.CrossSessionInbound != "accept" {
		t.Errorf("crossSessionInbound = %q, want accept", got.CrossSessionInbound)
	}
	if got.Permissions.DefaultMode != "bypassPermissions" {
		t.Errorf("defaultMode = %q, want bypassPermissions", got.Permissions.DefaultMode)
	}
	for _, event := range []string{claudeHookSessionStart, claudeHookUserPrompt, claudeHookStop} {
		entries, ok := got.Hooks[event]
		if !ok || len(entries) == 0 || len(entries[0].Hooks) == 0 {
			t.Fatalf("no hook registered for %s", event)
		}
		if entries[0].Hooks[0].Command != "hook.exe" {
			t.Errorf("%s command = %q", event, entries[0].Hooks[0].Command)
		}
	}
	if _, err := claudeLiveSettings(ClaudeConfig{}, "  "); err == nil {
		t.Error("a missing hook command must be refused: without it no reply can come back")
	}
}

func TestClaudeLiveArgs(t *testing.T) {
	args := claudeLiveArgs(ClaudeConfig{UseChrome: true}, "FlipAi SMS test", "{}")
	joined := strings.Join(args, " ")
	if args[0] != "remote-control" {
		t.Errorf("first arg = %q, want remote-control", args[0])
	}
	// Single-session mode is what makes this one conversation rather than a pool.
	if !strings.Contains(joined, "--spawn session") {
		t.Errorf("args must pin single-session mode, got %q", joined)
	}
	if !strings.Contains(joined, "--name FlipAi SMS test") {
		t.Errorf("args must carry the session name, got %q", joined)
	}
	if !strings.Contains(joined, "--chrome") {
		t.Errorf("args must honour the Chrome toggle, got %q", joined)
	}
	if strings.Contains(strings.Join(claudeLiveArgs(ClaudeConfig{}, "n", "{}"), " "), "--chrome") {
		t.Error("--chrome must not be passed when the toggle is off")
	}
}

func TestClaudeLiveMarkerRoundTrip(t *testing.T) {
	id := "abcd1234"
	prompt := claudeLiveMarker(id) + "\n<sms_command>\nhello\n</sms_command>"
	got, ok := claudeLiveMarkerID(prompt)
	if !ok || got != id {
		t.Fatalf("marker round-trip = %q,%v want %q,true", got, ok, id)
	}
	// A turn typed in the browser carries no marker, which is how FlipAi knows
	// not to text its answer to the user's phone.
	if _, ok := claudeLiveMarkerID("what is in my working directory?"); ok {
		t.Error("a browser-typed prompt must not look like a FlipAi turn")
	}
	if _, ok := claudeLiveMarkerID("<flipai_turn id=\"\"/>"); ok {
		t.Error("an empty marker id must not be accepted")
	}
}

func TestClaudeInboxFrame(t *testing.T) {
	frame, err := claudeInboxFrame("tok", "FlipAi SMS", "hello")
	if err != nil {
		t.Fatalf("claudeInboxFrame: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(frame), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want an auth line and a message line, got %d lines: %q", len(lines), frame)
	}
	var auth struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &auth); err != nil {
		t.Fatalf("auth line is not JSON: %v", err)
	}
	// Native Windows closes any connection whose first line is not a valid auth
	// line, so this ordering is load-bearing rather than cosmetic.
	if auth.Type != "auth" || auth.Token != "tok" {
		t.Errorf("auth line = %+v, want type=auth token=tok", auth)
	}
	var msg map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &msg); err != nil {
		t.Fatalf("message line is not JSON: %v", err)
	}
	if msg["text"] != "hello" {
		t.Errorf("message text = %v, want hello", msg["text"])
	}
	if _, err := claudeInboxFrame("", "s", "t"); err == nil {
		t.Error("a frame with no token must be refused rather than sent and dropped")
	}
}

func TestNormalizeClaudeInboxAddr(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"uds:/tmp/sock", "/tmp/sock"},
		{"/tmp/sock", "/tmp/sock"},
		{`\\.\pipe\claude-abc`, `\\.\pipe\claude-abc`},
		{`npipe:\\.\pipe\claude-abc`, `\\.\pipe\claude-abc`},
		{"  uds:/tmp/sock  ", "/tmp/sock"},
		{"", ""},
	} {
		if got := normalizeClaudeInboxAddr(tc.in); got != tc.want {
			t.Errorf("normalizeClaudeInboxAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClaudeHookCommandLineQuotesPaths(t *testing.T) {
	got := claudeHookCommandLine(`C:\Program Files\FlipAi\FlipAi.exe`, "http://127.0.0.1:8765/claude/hook", "secret")
	if !strings.HasPrefix(got, `"C:\Program Files\FlipAi\FlipAi.exe"`) {
		t.Errorf("an install path with a space must be quoted, got %s", got)
	}
	if !strings.Contains(got, "--claude-hook") || !strings.Contains(got, `"secret"`) {
		t.Errorf("command line is missing its arguments: %s", got)
	}
}

func TestIsClaudeLiveUnavailable(t *testing.T) {
	if !isClaudeLiveUnavailable(liveUnavailable("nope")) {
		t.Error("a live-mode refusal must be recognised so the turn can fall back")
	}
	if isClaudeLiveUnavailable(context.DeadlineExceeded) {
		t.Error("an ordinary error must not be mistaken for a live-mode refusal, or every failure would run twice")
	}
}

// newTestLiveClient returns a client that believes it already has a running
// session, with the transport replaced by a capture channel.
//
// Substituting the transport is what keeps these tests meaningful on Windows.
// The real writer there is a named pipe, which a Unix-socket fake cannot stand
// in for, and Go on Windows will happily create that socket rather than skip —
// so a socket-based fake fails on the platform FlipAi actually ships on. The
// delivery and correlation logic under test is platform-independent, so the
// transport is the only part that needs to be.
func newTestLiveClient(t *testing.T, token string) (*ClaudeLiveClient, chan string) {
	t.Helper()
	c := NewClaudeLiveClient("claude", "", ClaudeConfig{}, "", "hook")
	ready := make(chan struct{})
	close(ready)
	c.cmd = &exec.Cmd{}
	c.ready = ready
	c.socket = "inbox-under-test"
	c.msgToken = token
	c.sessionID = "session-under-test"

	frames := make(chan string, 4)
	c.writeInbox = func(addr string, frame []byte) error {
		frames <- string(frame)
		return nil
	}
	return c, frames
}

// newFailingLiveClient stands in for a session whose inbox cannot be reached.
func newFailingLiveClient(t *testing.T) *ClaudeLiveClient {
	t.Helper()
	c, _ := newTestLiveClient(t, "tok")
	c.writeInbox = func(addr string, frame []byte) error {
		return errors.New("inbox is gone")
	}
	return c
}

// inboxMessageText decodes the message line of a delivered frame. The frame is
// JSON on the wire, so the prompt inside it is escaped; reading the marker off
// the raw bytes would test the escaping rather than the delivery.
func inboxMessageText(t *testing.T, frame string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("frame has no message line: %q", frame)
	}
	var msg struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &msg); err != nil {
		t.Fatalf("message line is not JSON: %v", err)
	}
	return msg.Text
}

// The full live turn: FlipAi delivers an SMS into the session's inbox, Claude
// Code reports the prompt id through UserPromptSubmit, and the Stop hook
// carries the answer back to the waiting turn.
func TestLiveClientRunDeliversAndCorrelates(t *testing.T) {
	c, frames := newTestLiveClient(t, "inbox-token")

	type result struct {
		reply string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		reply, err := c.Run(context.Background(), "FlipAi SMS test", "+15555550123", "<sms_command>\nstatus?\n</sms_command>")
		done <- result{reply, err}
	}()

	var frame string
	select {
	case frame = <-frames:
	case <-time.After(5 * time.Second):
		t.Fatal("the SMS was never written to the session inbox")
	}
	if !strings.Contains(frame, `"type":"auth"`) || !strings.Contains(frame, "inbox-token") {
		t.Fatalf("frame is missing its auth line: %q", frame)
	}
	markerID, ok := claudeLiveMarkerID(inboxMessageText(t, frame))
	if !ok {
		t.Fatalf("delivered frame carries no correlation marker: %q", frame)
	}

	// Claude Code reports the prompt it accepted, then the answer.
	if !c.Deliver(claudeHookPayload{Event: claudeHookUserPrompt, PromptID: "p1", UserPrompt: claudeLiveMarker(markerID) + "\nstatus?"}) {
		t.Fatal("UserPromptSubmit for a FlipAi turn should be claimed")
	}
	if !c.Deliver(claudeHookPayload{Event: claudeHookStop, PromptID: "p1", LastAssistantMessage: "All good."}) {
		t.Fatal("Stop for a FlipAi turn should be claimed")
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("live turn failed: %v", got.err)
		}
		if got.reply != "All good." {
			t.Errorf("reply = %q, want %q", got.reply, "All good.")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the live turn never received its reply")
	}
}

// A turn typed at claude.ai/code must not be texted to the user's phone. This
// is the reason the correlation marker exists at all.
func TestLiveClientIgnoresBrowserTypedTurns(t *testing.T) {
	c, frames := newTestLiveClient(t, "inbox-token")

	done := make(chan error, 1)
	go func() {
		_, err := c.Run(context.Background(), "n", "+15555550123", "sms prompt")
		done <- err
	}()
	select {
	case <-frames:
	case <-time.After(5 * time.Second):
		t.Fatal("the SMS was never delivered")
	}

	// Someone types in the browser while the SMS turn is still running.
	if c.Deliver(claudeHookPayload{Event: claudeHookUserPrompt, PromptID: "browser", UserPrompt: "what changed today?"}) {
		t.Error("an unmarked prompt must not be claimed by a waiting SMS turn")
	}
	if c.Deliver(claudeHookPayload{Event: claudeHookStop, PromptID: "browser", LastAssistantMessage: "Here is the browser answer."}) {
		t.Error("the answer to a browser prompt must not be claimed by a waiting SMS turn")
	}

	select {
	case err := <-done:
		t.Fatalf("the SMS turn resolved from a browser turn: err=%v", err)
	case <-time.After(300 * time.Millisecond):
		// Correct: still waiting for its own answer.
	}
}

func TestLiveClientRunFallsBackWhenInboxIsUnreachable(t *testing.T) {
	c := newFailingLiveClient(t)
	_, err := c.Run(context.Background(), "n", "s", "prompt")
	if err == nil {
		t.Fatal("delivering into a dead inbox must fail")
	}
	// The whole safety story depends on this being a fallback rather than a
	// hard failure: the user still gets their reply through per-message mode.
	if !isClaudeLiveUnavailable(err) {
		t.Errorf("want a live-unavailable error so the turn falls back, got %v", err)
	}
}

func TestLiveClientStopReleasesWaitingTurns(t *testing.T) {
	c, frames := newTestLiveClient(t, "tok")
	done := make(chan error, 1)
	go func() {
		_, err := c.Run(context.Background(), "n", "s", "prompt")
		done <- err
	}()
	select {
	case <-frames:
	case <-time.After(5 * time.Second):
		t.Fatal("the SMS was never delivered")
	}
	c.Stop()
	select {
	case err := <-done:
		if !isClaudeLiveUnavailable(err) {
			t.Errorf("a turn cut off by Stop must fall back, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop left a turn waiting forever")
	}
}

func TestLiveClientSessionStartRecordsInbox(t *testing.T) {
	c := NewClaudeLiveClient("claude", "", ClaudeConfig{}, "", "hook")
	c.ready = make(chan struct{})
	if c.Deliver(claudeHookPayload{Event: claudeHookSessionStart, SessionID: "s1"}) {
		t.Error("a SessionStart with no inbox address must not mark the session ready")
	}
	if !c.Deliver(claudeHookPayload{Event: claudeHookSessionStart, SessionID: "s1", Socket: "/tmp/x", Token: "t"}) {
		t.Fatal("SessionStart with an inbox should be accepted")
	}
	if c.SessionID() != "s1" {
		t.Errorf("SessionID = %q, want s1", c.SessionID())
	}
	select {
	case <-c.ready:
	default:
		t.Error("the session should be marked ready once it reports its inbox")
	}
}
