package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type ClaudeAuthStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	ApiProvider      string `json:"apiProvider"`
	SubscriptionType string `json:"subscriptionType"`
}

// claudeAuthTTL is how long a validated auth status is reused. The check exists
// to refuse API/Console billing, which does not change mid-session; running it
// as a separate subprocess before every single SMS added seconds to each turn
// for no benefit. A failed run clears the cache, so a genuine sign-out is
// caught on the next attempt.
const claudeAuthTTL = 10 * time.Minute

type ClaudeClient struct {
	path string
	cwd  string
	cfg  ClaudeConfig
	// token is the optional long-lived `claude setup-token` value. Empty means
	// fall back to the CLI's own browser login, exactly as before.
	token string
	mu    sync.Mutex

	authCached ClaudeAuthStatus
	authAt     time.Time
	authValid  bool
}

type claudeResult struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
}

func NewClaudeClient(path, cwd string, cfg ClaudeConfig) *ClaudeClient {
	return &ClaudeClient{path: resolveClaudeExecutable(path), cwd: cwd, cfg: cfg}
}

// NewClaudeClientWithToken builds a client that authenticates with a stored
// long-lived token instead of relying on the CLI's expiring browser session.
func NewClaudeClientWithToken(path, cwd string, cfg ClaudeConfig, token string) *ClaudeClient {
	c := NewClaudeClient(path, cwd, cfg)
	c.token = strings.TrimSpace(token)
	return c
}

// scrubAnthropicEnv prevents FlipAi from silently using API-key, custom
// endpoint, Bedrock, Vertex, or Foundry billing inherited from the Windows
// environment. CLAUDE_CODE_OAUTH_TOKEN is intentionally not removed because it
// is Claude Code's subscription/OAuth token mechanism.
func scrubAnthropicEnv(env []string) []string {
	deny := map[string]bool{
		"ANTHROPIC_API_KEY":             true,
		"ANTHROPIC_AUTH_TOKEN":          true,
		"ANTHROPIC_BASE_URL":            true,
		"CLAUDE_CODE_USE_BEDROCK":       true,
		"CLAUDE_CODE_USE_VERTEX":        true,
		"CLAUDE_CODE_USE_FOUNDRY":       true,
		"ANTHROPIC_VERTEX_PROJECT_ID":   true,
		"ANTHROPIC_VERTEX_REGION":       true,
		"ANTHROPIC_FOUNDRY_RESOURCE":    true,
		"ANTHROPIC_FOUNDRY_API_VERSION": true,
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		k := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			k = e[:i]
		}
		if !deny[strings.ToUpper(k)] {
			out = append(out, e)
		}
	}
	return out
}

// cachedAuthStatus returns a recent validated status, or runs the real check.
func (c *ClaudeClient) cachedAuthStatus(ctx context.Context) (ClaudeAuthStatus, error) {
	c.mu.Lock()
	if c.authValid && time.Since(c.authAt) < claudeAuthTTL {
		st := c.authCached
		c.mu.Unlock()
		return st, nil
	}
	c.mu.Unlock()

	st, err := c.authStatus(ctx)
	if err != nil {
		return st, err
	}
	c.mu.Lock()
	c.authCached, c.authAt, c.authValid = st, time.Now(), true
	c.mu.Unlock()
	return st, nil
}

func (c *ClaudeClient) invalidateAuthCache() {
	c.mu.Lock()
	c.authValid = false
	c.mu.Unlock()
}

// authStatus deliberately parses Claude's JSON even when the command exits 1.
// Current Claude Code versions can return a perfectly useful JSON status such
// as {loggedIn:false, authMethod:"none", apiProvider:"firstParty"} with exit 1.
// Treating the exit code as the status was the reason FlipAi stopped before it
// could try the actual background request.
func (c *ClaudeClient) authStatus(ctx context.Context) (ClaudeAuthStatus, error) {
	var st ClaudeAuthStatus
	if strings.TrimSpace(c.path) == "" {
		return st, errors.New("Claude Code path is empty")
	}
	cmd := exec.CommandContext(ctx, c.path, "auth", "status")
	cmd.Env = c.childEnv()
	hideWindow(cmd)
	out, runErr := cmd.CombinedOutput()
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) > 0 && json.Unmarshal(trimmed, &st) == nil {
		return st, nil
	}
	if runErr != nil {
		return st, fmt.Errorf("Claude Code auth status could not run using %q: %v: %s", c.path, runErr, truncate(string(out), 800))
	}
	return st, fmt.Errorf("Claude auth status did not return JSON; update Claude Code: %s", truncate(string(out), 500))
}

func validateClaudeSubscriptionPath(st ClaudeAuthStatus) error {
	method := strings.ToLower(strings.TrimSpace(st.AuthMethod))
	provider := strings.ToLower(strings.TrimSpace(st.ApiProvider))
	if provider != "" && provider != "firstparty" {
		return fmt.Errorf("Claude is configured for provider %q, not the first-party Claude subscription service; FlipAi refuses external/provider billing", st.ApiProvider)
	}
	if strings.Contains(method, "api") || strings.Contains(method, "console") {
		return fmt.Errorf("Claude is authenticated for API/Console billing (%s); FlipAi refuses that billing path", st.AuthMethod)
	}
	return nil
}

// childEnv builds the environment for a Claude Code subprocess: the scrubbed
// parent environment, plus the long-lived token when one is stored.
// scrubAnthropicEnv deliberately preserves CLAUDE_CODE_OAUTH_TOKEN, so setting
// it here is the supported way to keep an unattended bridge signed in.
func (c *ClaudeClient) childEnv() []string {
	env := scrubAnthropicEnv(os.Environ())
	if tok := strings.TrimSpace(c.token); tok != "" {
		// Drop any inherited value first so the stored token always wins.
		out := env[:0]
		for _, e := range env {
			if !strings.HasPrefix(strings.ToUpper(e), "CLAUDE_CODE_OAUTH_TOKEN=") {
				out = append(out, e)
			}
		}
		env = append(out, "CLAUDE_CODE_OAUTH_TOKEN="+tok)
	}
	return env
}

func (c *ClaudeClient) runPrint(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.path, args...)
	if c.cwd != "" {
		cmd.Dir = c.cwd
	}
	cmd.Env = c.childEnv()
	hideWindow(cmd)
	return cmd.CombinedOutput()
}

// Test performs the real non-interactive Claude Code request used by SMS.
// `claude auth status` is used only to reject explicit API/provider billing;
// it is not trusted as the final readiness signal because current Claude Code
// builds can report loggedIn:false while another first-party OAuth path still
// lets `claude -p` work. The real request is the authoritative test.
func (c *ClaudeClient) Test(ctx context.Context) error {
	st, err := c.authStatus(ctx)
	if err != nil {
		return err
	}
	if err := validateClaudeSubscriptionPath(st); err != nil {
		return err
	}
	args := []string{"-p", "Reply with exactly FLIPAI_CLAUDE_OK and nothing else.", "--output-format", "json", "--max-turns", "1", "--permission-mode", "plan"}
	out, runErr := c.runPrint(ctx, args)
	if runErr != nil {
		if !st.LoggedIn {
			return fmt.Errorf("Claude Code CLI could not run a first-party background request: %v: %s\n\nClaude Desktop and Claude Code CLI have separate sign-in state on Windows. Being signed into the Claude desktop app does not prove the CLI at %q is authenticated. Open Claude Code in PowerShell and complete /login (or run `claude auth login`), then test again", runErr, truncate(string(out), 800), c.path)
		}
		return fmt.Errorf("Claude subscription login exists, but the real background request failed: %v: %s", runErr, truncate(string(out), 800))
	}
	var r claudeResult
	if err := json.Unmarshal(out, &r); err != nil {
		return fmt.Errorf("Claude background test returned unexpected output; update Claude Code: %s", truncate(string(out), 500))
	}
	if r.IsError {
		return fmt.Errorf("Claude background test reported an error: %s", truncate(r.Result, 500))
	}
	if !strings.Contains(strings.ToUpper(r.Result), "FLIPAI_CLAUDE_OK") {
		return fmt.Errorf("Claude background test did not return the expected verification response: %s", truncate(r.Result, 300))
	}
	return nil
}

func (c *ClaudeClient) Run(ctx context.Context, sessionID, prompt string) (result, newSession string, err error) {
	st, err := c.cachedAuthStatus(ctx)
	if err != nil {
		return "", sessionID, err
	}
	if err := validateClaudeSubscriptionPath(st); err != nil {
		return "", sessionID, err
	}
	args := []string{"-p", prompt, "--output-format", "json"}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	pm := strings.TrimSpace(c.cfg.PermissionMode)
	if pm == "" || pm == "auto" || pm == "bypassPermissions" {
		pm = "acceptEdits"
	}
	args = append(args, "--permission-mode", pm)
	// Claude keeps whatever capabilities it has at the desktop, browser
	// included, so "check my sales in the browser" works by text too. FlipAi
	// no longer probes `claude --help` for this: the flag is configuration.
	if c.cfg.UseChrome {
		args = append(args, "--chrome")
	}
	out, runErr := c.runPrint(ctx, args)
	if runErr != nil {
		// A failed run may mean the CLI was signed out since the last check.
		c.invalidateAuthCache()
		if !st.LoggedIn {
			return "", sessionID, fmt.Errorf("Claude Code CLI is not usable from FlipAi yet: %v: %s. Claude Desktop sign-in is separate from Claude Code CLI sign-in; complete Claude Code /login once on this Windows account", runErr, truncate(string(out), 800))
		}
		return "", sessionID, fmt.Errorf("Claude Code failed: %v: %s", runErr, truncate(string(out), 800))
	}
	var r claudeResult
	if json.Unmarshal(out, &r) != nil {
		return strings.TrimSpace(string(out)), sessionID, nil
	}
	if r.IsError {
		return "", r.SessionID, fmt.Errorf("Claude reported an error: %s", r.Result)
	}
	if r.SessionID == "" {
		r.SessionID = sessionID
	}
	return strings.TrimSpace(r.Result), r.SessionID, nil
}
