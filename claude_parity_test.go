package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hasFlagValue reports whether args contains flag immediately followed by value.
func hasFlagValue(args []string, flag, value string) bool {
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// An SMS turn must reach as far for Claude as it already does for Codex.
// applyCodexRequestDefaults gives every Codex turn approvalPolicy "never" and
// sandbox "dangerFullAccess"; the Claude equivalent is bypassPermissions.
// acceptEdits auto-approves file edits only, so Chrome — whose tools are MCP
// tools — was refused on an unattended turn that nobody can approve.
func TestClaudeSMSTurnRequestsFullAccessLikeCodex(t *testing.T) {
	args := claudeTurnArgs(ClaudeConfig{}, "", "do something")
	if !hasFlagValue(args, "--permission-mode", claudeFullAccess) {
		t.Fatalf("an unconfigured Claude SMS turn must request full access, got %v", args)
	}

	codex, _ := applyCodexRequestDefaults("turn/start", map[string]any{"threadId": "t1"}).(map[string]any)
	if codex["approvalPolicy"] != "never" {
		t.Fatalf("Codex parity baseline changed: %v", codex)
	}
}

// A mode the user deliberately narrowed to must survive. Only the value the old
// forced rewrite produced is migrated.
func TestClaudeNarrowerPermissionModesArePreserved(t *testing.T) {
	for _, mode := range []string{"plan", "acceptEdits", "dontAsk", "default"} {
		args := claudeTurnArgs(ClaudeConfig{PermissionMode: mode}, "", "x")
		if !hasFlagValue(args, "--permission-mode", mode) {
			t.Fatalf("mode %q was not passed through: %v", mode, args)
		}
	}
	// Values Claude Code does not accept fall back to full access rather than
	// being handed to the CLI verbatim.
	for _, junk := range []string{"", "auto", "nonsense"} {
		if got := normalizeClaudePermissionMode(junk); got != claudeFullAccess {
			t.Fatalf("normalize(%q) = %q, want %q", junk, got, claudeFullAccess)
		}
	}
}

// Every install on disk reads "acceptEdits" because older builds rewrote the
// field on load, so that value carries no user intent and is migrated once.
func TestLoadConfigMigratesForcedAcceptEditsToFullAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.json")
	write := func(mode string) Config {
		if err := os.WriteFile(path, []byte(`{"claude":{"permissionMode":"`+mode+`","useChrome":true}}`), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(path, dir)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	if got := write("acceptEdits").Claude.PermissionMode; got != claudeFullAccess {
		t.Fatalf("acceptEdits should migrate to %q, got %q", claudeFullAccess, got)
	}
	if got := write("plan").Claude.PermissionMode; got != "plan" {
		t.Fatalf("a deliberately narrowed mode must survive, got %q", got)
	}
	if got := write("bypassPermissions").Claude.PermissionMode; got != claudeFullAccess {
		t.Fatalf("full access must no longer be downgraded, got %q", got)
	}
}

// Codex hands a finished thread back so Codex Desktop can open it. Claude Code
// has no such handoff — sessions are listed per working folder — so the SMS
// session is named to stay identifiable in the /resume picker.
func TestClaudeSMSSessionIsNamedForResumePicker(t *testing.T) {
	fresh := claudeTurnArgs(ClaudeConfig{}, "", "x")
	if !hasFlagValue(fresh, "--name", claudeSessionName) {
		t.Fatalf("a new SMS session must be named: %v", fresh)
	}
	// Naming is repeated on resume so a session created by an older build gets
	// the label too.
	resumed := claudeTurnArgs(ClaudeConfig{}, "sess-1", "x")
	if !hasFlagValue(resumed, "--name", claudeSessionName) || !hasFlagValue(resumed, "--resume", "sess-1") {
		t.Fatalf("a resumed SMS session must keep its name: %v", resumed)
	}
}

func TestClaudeChromeFlagFollowsConfig(t *testing.T) {
	if hasFlag(claudeTurnArgs(ClaudeConfig{UseChrome: false}, "", "x"), "--chrome") {
		t.Fatal("--chrome must not be passed when the toggle is off")
	}
	if !hasFlag(claudeTurnArgs(ClaudeConfig{UseChrome: true}, "", "x"), "--chrome") {
		t.Fatal("--chrome must be passed when the toggle is on")
	}
}

// A refused tool is reported inside an otherwise successful run, so without
// reading permission_denials a blocked turn looks like a normal answer.
func TestClaudePermissionDenialsAreReadable(t *testing.T) {
	raw := `{"type":"result","is_error":false,"result":"I do not have permission to control Chrome.",
	"session_id":"s1","permission_denials":[
	  {"tool_use_id":"a","tool_name":"chrome__tabs_context_mcp"},
	  {"tool_use_id":"b","tool_name":"chrome__tabs_context_mcp"},
	  {"tool_use_id":"c","tool_name":"Bash"}]}`
	var r claudeResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	got := r.deniedToolNames()
	want := []string{"chrome__tabs_context_mcp", "Bash"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("denied tools = %v, want %v", got, want)
	}
}

// The note shares a length-capped SMS with the real answer, and it must only
// recommend widening the mode when widening is actually the fix.
func TestClaudeDenialNoteIsShortAndAccurate(t *testing.T) {
	if got := claudeDenialNote("acceptEdits", nil); got != "" {
		t.Fatalf("no denials must add no note, got %q", got)
	}

	narrow := claudeDenialNote("acceptEdits", []string{"chrome__tabs_context_mcp"})
	if !strings.Contains(narrow, "full user access") {
		t.Fatalf("a narrowed mode must point at the fix: %q", narrow)
	}
	if len(narrow) > 160 {
		t.Fatalf("note is too long for a 300-char SMS reply (%d): %q", len(narrow), narrow)
	}

	// Under full access FlipAi did not cause the denial, so it must not tell the
	// user to set a mode they are already on.
	full := claudeDenialNote(claudeFullAccess, []string{"Bash"})
	if strings.Contains(full, "set Claude to full user access") {
		t.Fatalf("must not recommend the mode already in use: %q", full)
	}
	if !strings.Contains(full, "Bash") {
		t.Fatalf("the blocked tool must still be named: %q", full)
	}

	// A long denial list is capped rather than flooding the reply.
	many := claudeDenialNote("plan", []string{"a", "b", "c", "d", "e"})
	if !strings.Contains(many, "+2 more") {
		t.Fatalf("a long denial list must be summarised: %q", many)
	}
}

// End-to-end: the flags above must survive Run, not just the arg builder.
func TestClaudeRunSendsFullAccessAndChromeToTheCLI(t *testing.T) {
	t.Setenv("FLIPAI_TEST_CLAUDE_ECHO_ARGS", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := NewClaudeClient(os.Args[0], "", ClaudeConfig{UseChrome: true})
	line, _, err := c.Run(ctx, "", "check my sales in the browser")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--permission-mode " + claudeFullAccess,
		"--name " + claudeSessionName,
		"--chrome",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("SMS turn command line %q is missing %q", line, want)
		}
	}
}

// A blocked turn must say why instead of returning Claude's bare refusal, which
// reads as a Claude limitation rather than a FlipAi setting.
func TestClaudeRunExplainsABlockedChromeTurn(t *testing.T) {
	t.Setenv("FLIPAI_TEST_CLAUDE_DENY_CHROME", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := NewClaudeClient(os.Args[0], "", ClaudeConfig{PermissionMode: "acceptEdits", UseChrome: true})
	answer, _, err := c.Run(ctx, "", "open chrome")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "chrome__tabs_context_mcp") || !strings.Contains(answer, "acceptEdits") {
		t.Fatalf("a blocked turn must name the tool and the mode that blocked it: %q", answer)
	}
}

// The Agents page must tell the user how to reopen the SMS conversation, since
// Claude Code lists sessions per working folder instead of handing them to a
// desktop app the way Codex does.
func TestAgentsPageShowsHowToReopenTheClaudeSMSConversation(t *testing.T) {
	a := newTestApp(t)
	if err := saveState(a.statePath, State{ClaudeSessionID: "abc-123-session"}); err != nil {
		t.Fatal(err)
	}
	body := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	for _, want := range []string{
		"claude --resume abc-123-session",
		claudeSessionName,
		"Full user access",
		"claudeUseChrome",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Agents page is missing %q", want)
		}
	}
}

// Every mode the save handler accepts must also be offered by the dropdown.
// A mode that is accepted but not listed renders with nothing selected, so
// saving the form silently rewrites the user's choice to the first option.
func TestEveryAcceptedPermissionModeIsOfferedInTheDropdown(t *testing.T) {
	a := newTestApp(t)
	body := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	for _, mode := range []string{"bypassPermissions", "dontAsk", "acceptEdits", "plan", "default"} {
		if !strings.Contains(body, `value="`+mode+`"`) {
			t.Errorf("permission mode %q is accepted on save but missing from the dropdown", mode)
		}
		// And a round trip through the form must preserve it.
		if rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"permissionMode": {mode}}); rr.Code != http.StatusSeeOther {
			t.Fatalf("saving %q returned %d", mode, rr.Code)
		}
		cfg, err := loadConfig(a.configPath, a.dataDir)
		if err != nil {
			t.Fatal(err)
		}
		want := mode
		if mode == "acceptEdits" {
			// acceptEdits is the one value loadConfig migrates, because older
			// builds wrote it over every other choice.
			want = claudeFullAccess
		}
		if cfg.Claude.PermissionMode != want {
			t.Errorf("saved %q, loaded %q, want %q", mode, cfg.Claude.PermissionMode, want)
		}
	}
}

// Claude Code refuses full-access mode when the process is elevated. FlipAi
// never elevates, so this only happens when the bridge itself was started as an
// administrator — and the raw CLI text never mentions FlipAi's setting.
func TestClaudeElevationRefusalIsExplained(t *testing.T) {
	err := claudeElevationRefusal("--dangerously-skip-permissions cannot be used with root/sudo privileges for security reasons")
	if err == nil {
		t.Fatal("an elevation refusal must be recognised")
	}
	if !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("the explanation must name the cause: %v", err)
	}
	for _, unrelated := range []string{
		"Not logged in. Please run /login",
		// Ordinary Windows access-denied text also mentions administrators and
		// permissions; it is not a FlipAi setting problem.
		"Access is denied. You must have administrator privileges to change permissions on this file.",
	} {
		if err := claudeElevationRefusal(unrelated); err != nil {
			t.Fatalf("%q must not be reported as elevation: %v", unrelated, err)
		}
	}
}
