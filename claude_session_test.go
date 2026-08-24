package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newClaudeTestBridge builds a bridge wired to the stub Claude CLI, with its
// own state file so persistence can be asserted across restarts.
func newClaudeTestBridge(t *testing.T) *Bridge {
	t.Helper()
	dir := t.TempDir()
	cfg := defaultConfig(dir)
	claude := NewClaudeClient(os.Args[0], "", cfg.Claude)
	return NewBridge(cfg, filepath.Join(dir, "state.json"), State{}, nil, nil, claude)
}

// A conversation whose transcript is gone must not poison the bridge. Claude
// Code answers a deleted, emptied, corrupted, or aged-out session with the same
// line, so one detector covers every case.
func TestClaudeSessionIsGoneRecognisesEveryVanishedTranscript(t *testing.T) {
	for _, msg := range []string{
		"No conversation found with session ID: 7cb99244-c56d-4d63-9c35-52987a9692ba",
		"claude code failed: no conversation found with session id abc",
	} {
		if !claudeSessionIsGone(errFromString(msg)) {
			t.Errorf("%q must be recognised as a vanished conversation", msg)
		}
	}
	for _, msg := range []string{
		"Not logged in. Please run /login",
		"Claude reported an error: rate limited",
		"",
	} {
		if claudeSessionIsGone(errFromString(msg)) {
			t.Errorf("%q must not be treated as a vanished conversation", msg)
		}
	}
	if claudeSessionIsGone(nil) {
		t.Error("nil must not be a vanished conversation")
	}
}

// The whole point of the recovery: a dead session is replaced automatically,
// the command still runs, and the reply says the old context was lost.
func TestStaleClaudeSessionRecoversAndRetriesTheCommand(t *testing.T) {
	t.Setenv("FLIPAI_TEST_CLAUDE_SESSION_GONE", "1")
	b := newClaudeTestBridge(t)
	b.state.ClaudeSessionID = "dead-session-id"
	b.state.ClaudeSessionName = "FlipAi SMS 2026-08-24 10:00:00"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := b.runClaude(ctx, "check my sales", "+15551212")
	if err != nil {
		t.Fatalf("a vanished conversation must be recovered, not returned as a failure: %v", err)
	}
	if !strings.HasPrefix(out, claudeSessionLost) {
		t.Errorf("the reply must say the old conversation was unavailable: %q", out)
	}
	// The retry has to actually run the command, not just report the loss.
	if !strings.Contains(out, "Fresh Claude session.") {
		t.Errorf("the command must still run on the new session: %q", out)
	}
	// And the dead id must be replaced, so the next text is not stuck.
	id, name := b.claudeSession()
	if id != "claude_session_recovered" {
		t.Errorf("the recovered session id must be stored, got %q", id)
	}
	if id == "dead-session-id" {
		t.Error("the dead session id was left in place; every later text would fail the same way")
	}
	if name == "FlipAi SMS 2026-08-24 10:00:00" {
		t.Error("a recovered conversation must get its own name, not reuse the dead one")
	}
}

// Recovery must be one retry, not a loop: if the fresh session also fails the
// turn reports the failure rather than spinning.
func TestClaudeRecoveryRetriesOnlyOnce(t *testing.T) {
	t.Setenv("FLIPAI_TEST_CLAUDE_PRINT_FAIL", "1")
	b := newClaudeTestBridge(t)
	b.state.ClaudeSessionID = "dead-session-id"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := b.runClaude(ctx, "hello", "+15551212"); err == nil {
		t.Fatal("a turn that fails for a real reason must return that failure")
	}
}

// A healthy conversation must be left alone — the recovery must not fire on an
// ordinary turn and silently discard context.
func TestHealthyClaudeSessionIsResumedNotReplaced(t *testing.T) {
	b := newClaudeTestBridge(t)
	b.state.ClaudeSessionID = "claude_session_test"
	b.state.ClaudeSessionName = "FlipAi SMS 2026-08-24 10:00:00"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := b.runClaude(ctx, "hello", "+15551212")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, claudeSessionLost) {
		t.Errorf("a healthy conversation must not be reported as lost: %q", out)
	}
	if id, name := b.claudeSession(); id != "claude_session_test" || name != "FlipAi SMS 2026-08-24 10:00:00" {
		t.Errorf("a healthy conversation must keep its id and name, got %q / %q", id, name)
	}
}

// Every new-session command must produce a distinct name. Claude Code refuses
// an ambiguous --resume <name> with "matches N sessions", so reusing one string
// would break the documented resume handle after the first A NEW.
func TestEveryNewClaudeSessionGetsAUniqueName(t *testing.T) {
	b := newClaudeTestBridge(t)
	seen := map[string]bool{}
	// Back-to-back, with no sleeps: two new-session commands inside the same
	// second must still get different names.
	for i := 0; i < 200; i++ {
		name := b.startNewClaudeSession()
		if name == "" {
			t.Fatal("a new conversation must be named")
		}
		if seen[name] {
			t.Fatalf("name %q was reused after %d new sessions; --resume by name would become ambiguous", name, i)
		}
		seen[name] = true
		if id, _ := b.claudeSession(); id != "" {
			t.Fatalf("a new-session command must clear the stored id, got %q", id)
		}
	}
	if !strings.HasPrefix(b.state.ClaudeSessionName, claudeSessionPrefix) {
		t.Errorf("a session name must stay recognisable as FlipAi's: %q", b.state.ClaudeSessionName)
	}
	// Two names minted in the same instant must still differ.
	now := time.Now()
	if newClaudeSessionName(now) == newClaudeSessionName(now) {
		t.Error("names minted at the same instant collided")
	}
}

// After a new-session command, the next text opens a fresh conversation and
// every text after that continues it.
func TestNewSessionCommandThenAllLaterTextsContinueThatSession(t *testing.T) {
	b := newClaudeTestBridge(t)
	b.state.ClaudeSessionID = "old-session"
	b.startNewClaudeSession()
	if id, _ := b.claudeSession(); id != "" {
		t.Fatalf("the old conversation must be dropped, got %q", id)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := b.runClaude(ctx, "first message", "+15551212"); err != nil {
		t.Fatal(err)
	}
	id, name := b.claudeSession()
	if id != "claude_session_test" {
		t.Fatalf("the first text after a new-session command must create and store a session, got %q", id)
	}
	// Every later text resumes that same conversation, with the same name.
	for i := 0; i < 3; i++ {
		if _, err := b.runClaude(ctx, "later message", "+15551212"); err != nil {
			t.Fatal(err)
		}
		gotID, gotName := b.claudeSession()
		if gotID != id || gotName != name {
			t.Fatalf("later texts must continue the same conversation: %q/%q became %q/%q", id, name, gotID, gotName)
		}
	}
}

// The active conversation has to survive a FlipAi restart and a Windows reboot,
// both of which mean a brand-new process reading state.json from disk.
func TestClaudeSessionSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	cfg := defaultConfig(dir)
	claude := NewClaudeClient(os.Args[0], "", cfg.Claude)

	first := NewBridge(cfg, statePath, State{}, nil, nil, claude)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := b1Run(ctx, first); err != nil {
		t.Fatal(err)
	}
	wantID, wantName := first.claudeSession()
	if wantID == "" {
		t.Fatal("the first turn should have stored a session")
	}

	// A restart is a new process: state comes back only from state.json.
	restarted := NewBridge(cfg, statePath, loadState(statePath), nil, nil, claude)
	gotID, gotName := restarted.claudeSession()
	if gotID != wantID {
		t.Errorf("session id did not survive a restart: %q became %q", wantID, gotID)
	}
	if gotName != wantName {
		t.Errorf("session name did not survive a restart: %q became %q", wantName, gotName)
	}
}

func b1Run(ctx context.Context, b *Bridge) (string, error) {
	return b.runClaude(ctx, "hello", "+15551212")
}

func errFromString(s string) error {
	if s == "" {
		return nil
	}
	return &stringError{s}
}

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }
