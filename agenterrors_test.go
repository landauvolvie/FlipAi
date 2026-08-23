package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The exact shape seen in the field: exit status 1 plus a full JSON result
// object. Truncated into a 300-character SMS this was unreadable.
const realClaudeOAuthFailure = `exit status 1: {"type":"result","subtype":"success","is_error":true,` +
	`"api_error_status":null,"duration_ms":1421,"result":"Failed to authenticate: ` +
	`OAuth session expired and could not be refreshed","stop_reason":"stop_sequence",` +
	`"session_id":"3c9a8756-d6ad-4bca-a5cf-28c333c3a00d","total_cost_usd":0,` +
	`"usage":{"input_tokens":0},"terminal_reason":"api_error","fast_mode_state":"off"}`

func TestExpiredClaudeSessionBecomesOneActionableSentence(t *testing.T) {
	got := friendlyAgentError(errors.New(realClaudeOAuthFailure))

	if strings.Contains(got, "{") || strings.Contains(got, "session_id") {
		t.Fatalf("raw JSON still leaks into the reply: %q", got)
	}
	if !strings.Contains(got, "setup-token") {
		t.Fatalf("message does not tell the user how to fix it: %q", got)
	}
	// Must survive an SMS intact rather than being cut mid-instruction.
	if n := len([]rune(got)); n > 300 {
		t.Fatalf("message is %d runes; it would be truncated in one text", n)
	}
}

func TestFriendlyAgentErrorCoversKnownFailures(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"resume Codex thread abc: no rollout found for thread id abc (-32600)", "C NEW"},
		{"Codex is not authenticated with Sign in with ChatGPT", "signed in with ChatGPT"},
		{"Not logged in. Please run /login", "not signed in"},
		{"Claude is authenticated for API/Console billing (api_key)", "API/Console billing"},
		{"Codex app-server is not running", "background service"},
	}
	for _, c := range cases {
		got := friendlyAgentMessage(c.raw)
		if !strings.Contains(got, c.want) {
			t.Errorf("friendlyAgentMessage(%q) = %q, want it to mention %q", c.raw, got, c.want)
		}
		if got == c.raw {
			t.Errorf("%q was not translated at all", c.raw)
		}
	}
}

// An unfamiliar failure must never be swallowed or replaced with a guess.
func TestUnknownAgentErrorPassesThroughUnchanged(t *testing.T) {
	raw := "some brand new failure nobody has seen before"
	if got := friendlyAgentMessage(raw); got != raw {
		t.Fatalf("unknown error was rewritten: %q", got)
	}
	if friendlyAgentError(nil) != "" {
		t.Fatal("nil error should map to an empty string")
	}
}

func TestClaudeTokenRoundTripsAndValidates(t *testing.T) {
	dir := t.TempDir()
	path := claudeTokenPath(dir)

	if hasClaudeToken(path) {
		t.Fatal("a fresh install should have no stored token")
	}
	const token = "sk-ant-oat01-EXAMPLEEXAMPLEEXAMPLEEXAMPLE"
	if err := saveClaudeToken(path, token); err != nil {
		t.Fatal(err)
	}
	if !hasClaudeToken(path) {
		t.Fatal("token was not stored")
	}
	got, err := loadClaudeToken(path)
	if err != nil || got != token {
		t.Fatalf("round trip failed: %q %v", got, err)
	}
	// Never left world-readable on disk. Windows does not implement POSIX
	// permission bits — Go reports 0666 there whatever mode os.WriteFile was
	// given — so this only asserts on platforms where the bits mean something.
	// On Windows the real protection is DPAPI, exercised by the round trip above:
	// the bytes on disk are encrypted to this user account.
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != 0600 {
			t.Fatalf("token file mode is %v, want 0600", fi.Mode().Perm())
		}
	}
	if err := clearClaudeToken(path); err != nil {
		t.Fatal(err)
	}
	if hasClaudeToken(path) {
		t.Fatal("token survived being cleared")
	}
	// Clearing twice must not error.
	if err := clearClaudeToken(path); err != nil {
		t.Fatalf("clearing an absent token should be a no-op: %v", err)
	}
}

// A newline would let a pasted value smuggle a second environment variable into
// the Claude child process.
func TestClaudeTokenRejectsMalformedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tok.dat")
	for _, bad := range []string{
		"",
		"   ",
		"short",
		"sk-ant-oat01-EXAMPLEEXAMPLEEXAMPLE\nANTHROPIC_API_KEY=leak",
		"sk-ant-oat01-EXAMPLE EXAMPLE-with-space",
	} {
		if err := saveClaudeToken(path, bad); err == nil {
			t.Errorf("malformed token %q was accepted", bad)
		}
	}
	if hasClaudeToken(path) {
		t.Fatal("a rejected token must not be written to disk")
	}
}

func TestStoredTokenIsInjectedIntoClaudeEnvironment(t *testing.T) {
	const token = "sk-ant-oat01-EXAMPLEEXAMPLEEXAMPLEEXAMPLE"

	withToken := NewClaudeClientWithToken(os.Args[0], "", ClaudeConfig{}, token)
	env := withToken.childEnv()
	found := 0
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") {
			found++
			if e != "CLAUDE_CODE_OAUTH_TOKEN="+token {
				t.Fatalf("wrong token injected: %q", e)
			}
		}
	}
	if found != 1 {
		t.Fatalf("expected exactly one CLAUDE_CODE_OAUTH_TOKEN entry, got %d", found)
	}

	// With no token stored, behaviour is exactly as before: normal CLI login.
	plain := NewClaudeClient(os.Args[0], "", ClaudeConfig{})
	for _, e := range plain.childEnv() {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") {
			t.Fatalf("token variable appeared without a stored token: %q", e)
		}
	}
}

// An inherited machine-level token must not beat the one the user configured.
func TestStoredTokenOverridesInheritedEnvironment(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-INHERITEDINHERITEDINHERITED")
	c := NewClaudeClientWithToken(os.Args[0], "", ClaudeConfig{}, "sk-ant-oat01-CONFIGUREDCONFIGUREDCONFIG")

	seen := []string{}
	for _, e := range c.childEnv() {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") {
			seen = append(seen, e)
		}
	}
	if len(seen) != 1 || !strings.HasSuffix(seen[0], "CONFIGUREDCONFIGUREDCONFIG") {
		t.Fatalf("configured token did not win: %v", seen)
	}
}
