package main

import (
	"strings"
	"testing"
)

// envHas reports whether an environment slice carries a variable.
func envHas(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), strings.ToUpper(key)+"=") {
			return true
		}
	}
	return false
}

// The rule this encodes, from Claude Code's own documentation: Chrome
// integration requires signing in with /login, and a `claude setup-token`
// session has Chrome turned off even when --chrome is passed, because the
// browser extension cannot authenticate with that credential.
//
// FlipAi injected the token into every turn, so an SMS command could never
// drive Chrome while the same account drove it fine from a terminal. These
// cases pin which credential each combination actually gets.
func TestStoredTokenIsWithheldOnlyWhenItWouldDisableChrome(t *testing.T) {
	const tok = "sk-ant-oat01-testtoken"

	t.Run("chrome off keeps the token", func(t *testing.T) {
		c := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: false}, tok)
		c.loginChecked, c.loginExists = true, true
		if !envHas(c.childEnv(), "CLAUDE_CODE_OAUTH_TOKEN") {
			t.Error("with Chrome off the token costs nothing and must still be used")
		}
	})

	t.Run("chrome on with a real login withholds the token", func(t *testing.T) {
		c := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: true}, tok)
		c.loginChecked, c.loginExists = true, true
		if envHas(c.childEnv(), "CLAUDE_CODE_OAUTH_TOKEN") {
			t.Error("the token must be withheld so the /login session can drive Chrome")
		}
	})

	t.Run("chrome on with no login still uses the token", func(t *testing.T) {
		c := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: true}, tok)
		c.loginChecked, c.loginExists = true, false
		if !envHas(c.childEnv(), "CLAUDE_CODE_OAUTH_TOKEN") {
			t.Error("with no interactive login the token is the only way to answer at all")
		}
	})

	t.Run("before the probe runs the token is used", func(t *testing.T) {
		// Failing closed here would strand a bridge whose only credential is the
		// token, which is worse than losing Chrome for one turn.
		c := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: true}, tok)
		if !envHas(c.childEnv(), "CLAUDE_CODE_OAUTH_TOKEN") {
			t.Error("an unresolved probe must not withhold the only credential")
		}
	})

	t.Run("no token stored is unaffected", func(t *testing.T) {
		c := NewClaudeClient("claude", "", ClaudeConfig{UseChrome: true})
		if envHas(c.childEnv(), "CLAUDE_CODE_OAUTH_TOKEN") {
			t.Error("nothing should be injected when no token is stored")
		}
	})
}

// baseEnv must strip a token inherited from the Windows environment too, or the
// withheld-token case would leak straight back in and Chrome would stay off.
func TestBaseEnvStripsAnInheritedToken(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-inherited")
	c := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: true}, "sk-ant-oat01-stored")
	c.loginChecked, c.loginExists = true, true
	env := c.childEnv()
	if envHas(env, "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatal("an inherited token must be stripped when the token is being withheld")
	}
	// And the stored one must still win when it is being used.
	c2 := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: false}, "sk-ant-oat01-stored")
	var got string
	for _, e := range c2.childEnv() {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") {
			got = strings.TrimPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=")
		}
	}
	if got != "sk-ant-oat01-stored" {
		t.Errorf("stored token should win over an inherited one, got %q", got)
	}
}

// The warning is what turns an invisible failure into something fixable, so it
// must appear exactly when Chrome cannot work and never otherwise.
func TestChromeTokenNoticeAppearsOnlyWhenChromeCannotWork(t *testing.T) {
	const tok = "sk-ant-oat01-testtoken"

	blocked := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: true}, tok)
	blocked.loginChecked, blocked.loginExists = true, false
	if notice := blocked.CachedChromeTokenConflict(); notice == "" {
		t.Error("a machine where Chrome cannot work must say so")
	} else if !strings.Contains(notice, "/login") {
		t.Errorf("the notice must name the fix, got %q", notice)
	}

	for name, c := range map[string]*ClaudeClient{
		"real login exists": func() *ClaudeClient {
			x := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: true}, tok)
			x.loginChecked, x.loginExists = true, true
			return x
		}(),
		"chrome switched off": func() *ClaudeClient {
			x := NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: false}, tok)
			x.loginChecked, x.loginExists = true, false
			return x
		}(),
		"no token stored":   NewClaudeClient("claude", "", ClaudeConfig{UseChrome: true}),
		"probe not yet run": NewClaudeClientWithToken("claude", "", ClaudeConfig{UseChrome: true}, tok),
	} {
		if notice := c.CachedChromeTokenConflict(); notice != "" {
			t.Errorf("%s should produce no warning, got %q", name, notice)
		}
	}
}
