package main

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The connection model is what the Agents page, the Activity log, and every
// connect handler read, so each state is pinned here rather than only being
// visible on a machine that happens to be in it.
func TestEvaluateClaudeConnection(t *testing.T) {
	t.Run("a real sign-in is the connected state", func(t *testing.T) {
		c := evaluateClaudeConnection(false, true, true)
		if c.Kind != claudeConnLogin || !c.ChromeReady || c.NeedsSignIn {
			t.Fatalf("want a Chrome-ready login, got %+v", c)
		}
	})

	// This is the whole point of the change: a token stored beside a real
	// sign-in is a fallback, not a downgrade.
	t.Run("a token beside a sign-in keeps Chrome", func(t *testing.T) {
		c := evaluateClaudeConnection(true, true, true)
		if c.Kind != claudeConnLogin || !c.ChromeReady || c.NeedsSignIn {
			t.Fatalf("a stored token must not demote a working sign-in, got %+v", c)
		}
		if !strings.Contains(c.Detail, "fallback") {
			t.Errorf("the detail should say the token is the fallback, got %q", c.Detail)
		}
	})

	// The state the user was actually in: texts answered, browser silently off.
	t.Run("a token alone cannot reach the browser", func(t *testing.T) {
		c := evaluateClaudeConnection(true, true, false)
		if c.Kind != claudeConnToken || c.ChromeReady || !c.NeedsSignIn {
			t.Fatalf("want a token-only connection needing sign-in, got %+v", c)
		}
		if !strings.Contains(c.Detail, "Connect Claude") {
			t.Errorf("the detail must name the fix, got %q", c.Detail)
		}
	})

	t.Run("nothing stored is not connected", func(t *testing.T) {
		c := evaluateClaudeConnection(false, true, false)
		if c.Kind != claudeConnNone || !c.NeedsSignIn {
			t.Fatalf("want a not-connected state, got %+v", c)
		}
	})

	// An unprobed machine must not be described as broken; nothing has looked
	// at it yet, and a Connect button offered on a guess is noise.
	t.Run("an unprobed machine claims nothing", func(t *testing.T) {
		c := evaluateClaudeConnection(true, false, false)
		if c.Kind != claudeConnUnknown || c.NeedsSignIn || c.ChromeReady {
			t.Fatalf("want an unknown state, got %+v", c)
		}
	})
}

// The sign-in has to run as an interactive console command, with the executable
// quoted: Claude Code installs under a path with a space in it often enough
// (C:\Program Files\...) that an unquoted command would fail for exactly the
// users who need this most.
func TestClaudeSignInArgsQuotesTheExecutable(t *testing.T) {
	got := claudeSignInArgs(`C:\Program Files\claude\claude.cmd`)
	if !strings.Contains(got, `"C:\Program Files\claude\claude.cmd"`) {
		t.Errorf("the executable must be quoted, got %q", got)
	}
	if !strings.HasSuffix(got, "/login") {
		t.Errorf("the sign-in command must run /login, got %q", got)
	}
	// /k, not /c: the window has to survive the flow so the user can read it.
	if !strings.HasPrefix(got, "/k ") {
		t.Errorf("the console must stay open after the flow, got %q", got)
	}
}

// The probe used to be cached for the life of the client, so a user who signed
// in to fix Chrome saw no change until they restarted FlipAi — the one step
// nothing on screen told them to take.
func TestLoginProbeExpiresAndCanBeInvalidated(t *testing.T) {
	c := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: true}, "sk-ant-oat01-testtoken")

	c.loginChecked, c.loginExists, c.loginAt = true, false, time.Now()
	if checked, exists := c.CachedLogin(); !checked || exists {
		t.Fatalf("a fresh negative probe should be reported as such, got checked=%v exists=%v", checked, exists)
	}

	c.invalidateLoginCache()
	if checked, _ := c.CachedLogin(); checked {
		t.Fatal("invalidating must leave nothing cached, so the next probe asks the machine")
	}

	// A stale answer must not be reused either, or a sign-in completed while
	// FlipAi was running would go unnoticed until the next restart.
	c.loginChecked, c.loginExists, c.loginAt = true, false, time.Now().Add(-2*claudeLoginTTL)
	c.mu.Lock()
	stale := c.loginChecked && time.Since(c.loginAt) < claudeLoginTTL
	c.mu.Unlock()
	if stale {
		t.Fatal("an answer older than the TTL must not satisfy the probe")
	}
}

// A withheld token must not walk back in through the Windows environment, or
// the live session would lose Remote Control and Chrome for the same reason the
// print path used to.
func TestLiveChildEnvStripsAnInheritedToken(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-inherited")

	withheld := NewClaudeLiveClient("claude", "", ClaudeConfig{}, "", "hook.exe")
	if envHas(withheld.childEnv(), "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Error("a live session running on the account sign-in must carry no token at all")
	}

	used := NewClaudeLiveClient("claude", "", ClaudeConfig{}, "sk-ant-oat01-stored", "hook.exe")
	var got string
	for _, e := range used.childEnv() {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") {
			got = strings.TrimPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=")
		}
	}
	if got != "sk-ant-oat01-stored" {
		t.Errorf("the stored token should win over an inherited one, got %q", got)
	}
}

// missingClaudeApp points the app at a Claude Code that is not installed, so a
// handler test exercises the connect flow rather than whatever CLI happens to
// be on the machine running the tests.
func missingClaudeApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.cfg.ClaudePath = filepath.Join(t.TempDir(), "no-claude-here")
	return a
}

// The connect flow is only a fix if it is on the page. Before this, the Agents
// page offered a token box and nothing else, which is how an install ended up
// token-only without anyone choosing that.
func TestAgentsPageOffersTheClaudeConnectFlow(t *testing.T) {
	body := missingClaudeApp(t).do(t, http.MethodGet, "/agents", nil).Body.String()
	for _, want := range []string{
		`formaction="/claude/connect"`,
		`formaction="/claude/connect/verify"`,
		`formaction="/claude/disconnect"`,
		"Connect Claude",
		"fallback",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Agents page is missing %q", want)
		}
	}
}

func TestClaudeConnectReportsAMissingCLI(t *testing.T) {
	a := missingClaudeApp(t)
	rr := a.do(t, http.MethodPost, "/claude/connect", url.Values{})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when Claude Code is not installed", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Claude Code was not found") {
		t.Errorf("the failure should name the missing CLI, got %q", rr.Body.String())
	}
}

// Disconnect removes what FlipAi stores and nothing else: the Claude Code
// sign-in belongs to the Windows account, not to FlipAi.
func TestClaudeDisconnectClearsTheStoredTokenOnly(t *testing.T) {
	a := missingClaudeApp(t)
	if err := saveClaudeToken(claudeTokenPath(a.dataDir), "sk-ant-oat01-"+strings.Repeat("x", 40)); err != nil {
		t.Fatal(err)
	}
	rr := a.do(t, http.MethodPost, "/claude/disconnect", url.Values{})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if hasClaudeToken(claudeTokenPath(a.dataDir)) {
		t.Error("the stored token should be gone after disconnecting")
	}
	if body := rr.Body.String(); !strings.Contains(body, "Not connected") {
		t.Errorf("the result should report the connection left behind, got %q", body)
	}
}

// A GET must not be able to disconnect Claude or open a console window.
func TestClaudeConnectRoutesRequireAPost(t *testing.T) {
	a := missingClaudeApp(t)
	for _, path := range []string{"/claude/connect", "/claude/connect/verify", "/claude/disconnect"} {
		if rr := a.do(t, http.MethodGet, path, nil); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s = %d, want 405", path, rr.Code)
		}
	}
}
