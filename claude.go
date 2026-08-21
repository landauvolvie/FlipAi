package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type ClaudeAuthStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	ApiProvider      string `json:"apiProvider"`
	SubscriptionType string `json:"subscriptionType"`
}

type ClaudeClient struct {
	path            string
	cwd             string
	cfg             ClaudeConfig
	mu              sync.Mutex
	chromeChecked   bool
	chromeSupported bool
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

func scrubAnthropicEnv(env []string) []string {
	deny := map[string]bool{"ANTHROPIC_API_KEY": true, "ANTHROPIC_AUTH_TOKEN": true, "ANTHROPIC_BASE_URL": true}
	out := make([]string, 0, len(env))
	for _, e := range env {
		k := e
		if i := strings.IndexByte(e, '='); i >= 0 { k = e[:i] }
		if !deny[strings.ToUpper(k)] { out = append(out, e) }
	}
	return out
}

func (c *ClaudeClient) supportsChrome(ctx context.Context) bool {
	c.mu.Lock()
	if c.chromeChecked {
		v := c.chromeSupported
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	cmd := exec.CommandContext(ctx, c.path, "--help")
	cmd.Env = scrubAnthropicEnv(os.Environ())
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput()
	v := strings.Contains(string(out), "--chrome")
	c.mu.Lock()
	c.chromeChecked, c.chromeSupported = true, v
	c.mu.Unlock()
	return v
}

func (c *ClaudeClient) checkAuth(ctx context.Context) error {
	if strings.TrimSpace(c.path) == "" { return errors.New("Claude path is empty") }
	cmd := exec.CommandContext(ctx, c.path, "auth", "status")
	cmd.Env = scrubAnthropicEnv(os.Environ())
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Claude Code is not signed in or could not start using %q: %v: %s", c.path, err, strings.TrimSpace(string(out)))
	}
	var st ClaudeAuthStatus
	if json.Unmarshal(out, &st) != nil {
		return errors.New("Claude auth status did not return JSON; update Claude Code")
	}
	if !st.LoggedIn { return errors.New("Claude Code is not signed in") }
	m := strings.ToLower(st.AuthMethod)
	provider := strings.ToLower(st.ApiProvider)
	if strings.Contains(m, "api") || strings.Contains(m, "console") || strings.Contains(provider, "console") {
		return fmt.Errorf("Claude is authenticated for API/Console billing (%s); sign in with your Claude subscription instead", st.AuthMethod)
	}
	return nil
}

// Test does a real, tiny non-interactive Claude Code request after the auth
// check. This catches cases where `claude auth status` looks healthy but the
// same background/print-mode execution used by SMS commands cannot actually
// authenticate or run. It intentionally avoids tools and limits the request to
// one turn.
func (c *ClaudeClient) Test(ctx context.Context) error {
	if err := c.checkAuth(ctx); err != nil { return err }
	args := []string{"-p", "Reply with exactly FLIPAI_CLAUDE_OK and nothing else.", "--output-format", "json", "--max-turns", "1", "--permission-mode", "plan"}
	cmd := exec.CommandContext(ctx, c.path, args...)
	if c.cwd != "" { cmd.Dir = c.cwd }
	cmd.Env = scrubAnthropicEnv(os.Environ())
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Claude login exists, but a real background request failed: %v: %s", err, truncate(string(out), 800))
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
	if err := c.checkAuth(ctx); err != nil { return "", sessionID, err }
	args := []string{"-p", prompt, "--output-format", "json"}
	if sessionID != "" { args = append(args, "--resume", sessionID) }
	pm := strings.TrimSpace(c.cfg.PermissionMode)
	if pm == "" || pm == "auto" || pm == "bypassPermissions" { pm = "acceptEdits" }
	args = append(args, "--permission-mode", pm)
	if c.cfg.UseChrome && c.supportsChrome(ctx) { args = append(args, "--chrome") }
	cmd := exec.CommandContext(ctx, c.path, args...)
	if c.cwd != "" { cmd.Dir = c.cwd }
	cmd.Env = scrubAnthropicEnv(os.Environ())
	hideWindow(cmd)
	out, e := cmd.CombinedOutput()
	if e != nil { return "", sessionID, fmt.Errorf("Claude Code failed: %v: %s", e, truncate(string(out), 800)) }
	var r claudeResult
	if json.Unmarshal(out, &r) != nil { return strings.TrimSpace(string(out)), sessionID, nil }
	if r.IsError { return "", r.SessionID, fmt.Errorf("Claude reported an error: %s", r.Result) }
	if r.SessionID == "" { r.SessionID = sessionID }
	return strings.TrimSpace(r.Result), r.SessionID, nil
}
