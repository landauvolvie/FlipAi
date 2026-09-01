package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Connecting Claude to FlipAi has always had two possible credentials, and only
// one of them can do everything.
//
//   - A Claude Code account sign-in is the browser sign-in Claude Code itself
//     uses. It is what the Claude in Chrome extension and Remote Control use.
//   - A `claude setup-token` value can make model requests, but cannot provide
//     the browser-connected account session.
const (
	claudeConnLogin   = "login"
	claudeConnToken   = "token"
	claudeConnNone    = "none"
	claudeConnUnknown = "unknown"
)

type claudeConnection struct {
	Kind        string
	Label       string
	Detail      string
	ChromeReady bool
	NeedsSignIn bool
}

func evaluateClaudeConnection(hasToken, probed, loginExists bool) claudeConnection {
	if !probed {
		return claudeConnection{
			Kind:   claudeConnUnknown,
			Label:  "Checking…",
			Detail: "FlipAi is checking how Claude Code is signed in on this Windows account.",
		}
	}
	switch {
	case loginExists && hasToken:
		return claudeConnection{
			Kind:        claudeConnLogin,
			Label:       "Claude Code sign-in (token kept as fallback)",
			ChromeReady: true,
			Detail: "Connected the right way: FlipAi runs Claude on this Windows account's Claude Code sign-in. " +
				"The stored token is kept only as a fallback.",
		}
	case loginExists:
		return claudeConnection{
			Kind:        claudeConnLogin,
			Label:       "Claude Code sign-in",
			ChromeReady: true,
			Detail:      "Connected the right way: FlipAi runs Claude on this Windows account's Claude Code sign-in.",
		}
	case hasToken:
		return claudeConnection{
			Kind:        claudeConnToken,
			Label:       "Stored token only",
			NeedsSignIn: true,
			Detail: "Claude can answer model requests with the saved token, but the Windows account is not signed in to Claude Code. " +
				"Press Connect Claude to complete the normal Claude Code browser sign-in.",
		}
	default:
		return claudeConnection{
			Kind:        claudeConnNone,
			Label:       "Not connected",
			NeedsSignIn: true,
			Detail:      "Claude Code is not signed in on this Windows account. Press Connect Claude to set it up.",
		}
	}
}

// The existing interactive flow is retained for machines that already have the
// CLI. The console stays open so the user can see Claude's result.
func claudeSignInArgs(exe string) string {
	return fmt.Sprintf(`/k "%s" /login`, strings.TrimSpace(exe))
}

func (a *App) claudeConnectClient() *ClaudeClient {
	a.mu.Lock()
	c, cfg := a.claude, a.cfg
	a.mu.Unlock()
	if c != nil {
		return c
	}
	return a.newClaudeClient(cfg)
}

// A default/blank path is managed discovery, so on a clean machine Connect may
// install the official Claude Code CLI. An explicit custom path is never
// overwritten or silently replaced.
func claudePathUsesManagedInstall(configured string) bool {
	v := strings.TrimSpace(configured)
	return v == "" || strings.EqualFold(v, "claude") || strings.EqualFold(v, "claude.exe")
}

// claudeConnectionNow deliberately builds a fresh client. A fresh Windows
// install can add %USERPROFILE%\.local\bin\claude.exe while FlipAi is already
// running; the client created at startup cannot discover a binary that did not
// exist yet. Re-resolving here lets the Connect watcher notice the installation
// immediately, without making the user restart or browse for an executable.
func (a *App) claudeConnectionNow(ctx context.Context) claudeConnection {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fresh := NewClaudeClient(cfg.ClaudePath, cfg.claudeWorkingDir(), cfg.Claude)
	exists := fresh.RefreshLogin(pctx)
	return evaluateClaudeConnection(hasClaudeToken(claudeTokenPath(a.dataDir)), true, exists)
}

func (a *App) cachedClaudeConnection() claudeConnection {
	a.mu.Lock()
	c := a.claude
	a.mu.Unlock()
	if c == nil {
		return evaluateClaudeConnection(hasClaudeToken(claudeTokenPath(a.dataDir)), false, false)
	}
	checked, exists := c.CachedLogin()
	return evaluateClaudeConnection(hasClaudeToken(claudeTokenPath(a.dataDir)), checked, exists)
}

func (a *App) warmClaudeConnection(ctx context.Context, cfg Config, b *Bridge, c *ClaudeClient) {
	pctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	conn := evaluateClaudeConnection(
		hasClaudeToken(claudeTokenPath(a.dataDir)), true, c.interactiveLoginExists(pctx))
	if b == nil || !conn.NeedsSignIn {
		return
	}
	if cfg.Claude.UseChrome || normalizeClaudeSessionMode(cfg.Claude.SessionMode) == claudeSessionModeLive {
		b.event("warn", "agent", "Claude is connected as \""+conn.Label+
			"\", which cannot control Chrome or reach claude.ai/code. Press Connect on the Agents page.", "", "A", "")
	}
}

// claudeConnect is the complete first-run action. If Claude Code is already
// installed it opens the normal interactive sign-in. If it is missing and the
// user left the path on automatic discovery, FlipAi opens PowerShell, runs
// Anthropic's official Windows installer, then starts `claude auth login`.
// Either way a watcher verifies the browser authorization automatically.
func (a *App) claudeConnect(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	exe := resolveClaudeExecutable(cfg.ClaudePath)
	installing := !executableExists(exe)
	if installing && !claudePathUsesManagedInstall(cfg.ClaudePath) {
		renderResult(w, r, 400, false, "Claude Code was not found",
			"FlipAi could not find the Claude Code CLI at "+exe+
				". Fix the executable path under Routing & workspace, then press Connect again.")
		return
	}

	var err error
	if installing {
		err = startClaudeInstallAndSignIn(cfg.claudeWorkingDir())
	} else {
		err = startClaudeSignIn(exe, cfg.claudeWorkingDir())
	}
	if err != nil {
		renderResult(w, r, 500, false, "Could not start Claude setup", err.Error())
		return
	}

	// From the moment Connect is pressed, the top row remains in Connect state
	// until the watcher proves the account is usable. That prevents a stale
	// previous test result from showing Disconnect/Test during an unfinished
	// browser authorization.
	_ = a.clearAgentCheck("claude")
	a.claudeConnectClient().invalidateLoginCache()
	activityLogForStatePath(a.statePath).Add("info", "agent", "Claude connection setup opened from the Agents page", "", "A", "")
	go a.watchClaudeSignIn()

	if installing {
		renderResult(w, r, 200, true, "Claude setup started",
			"PowerShell is installing Claude Code from Anthropic, then it will open the Claude browser sign-in. " +
				"Complete the authorization in the browser. FlipAi will detect it automatically; you do not need to press another connection button.")
		return
	}
	renderResult(w, r, 200, true, "Finish connecting Claude",
		"The Claude Code sign-in window is open. Complete the browser authorization it starts. " +
			"FlipAi will detect the completed sign-in automatically.")
}

// watchClaudeSignIn removes the old manual "Check connection" step. It also
// restarts the bridge once after a successful first install/sign-in so any
// long-lived Claude client is rebuilt with the newly discovered executable.
func (a *App) watchClaudeSignIn() {
	deadline := time.Now().Add(10 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		conn := a.claudeConnectionNow(ctx)
		cancel()
		if !conn.NeedsSignIn {
			a.recordCheck("claude", true, "Claude Code sign-in verified automatically")
			activityLogForStatePath(a.statePath).Add("success", "agent", "Claude connected and verified automatically", "", "A", "")
			go a.restartSoon()
			return
		}
		<-ticker.C
	}
	activityLogForStatePath(a.statePath).Add("warn", "agent", "Claude connection setup was not completed within 10 minutes", "", "A", "")
}

// Kept for old bookmarks/builds that still post this route. The current Agents
// page no longer exposes a separate Check connection button.
func (a *App) claudeConnectVerify(w http.ResponseWriter, r *http.Request) {
	conn := a.claudeConnectionNow(r.Context())

	a.mu.Lock()
	useChrome := a.cfg.Claude.UseChrome
	a.mu.Unlock()

	if conn.NeedsSignIn {
		activityLogForStatePath(a.statePath).Add("warn", "agent", "Claude connection checked: "+conn.Label, "", "A", "")
		renderResult(w, r, 200, false, "Claude is connected as: "+conn.Label, conn.Detail)
		return
	}
	a.recordCheck("claude", true, "Claude Code sign-in verified")
	activityLogForStatePath(a.statePath).Add("success", "agent", "Claude connected with a Claude Code account sign-in", "", "A", "")
	detail := conn.Detail
	if !useChrome {
		detail += "\n\nLet Claude control Chrome is currently switched off under Access & tools."
	}
	restart := a.claudeLiveNeedsRestart()
	if restart {
		detail += liveRestartNote
	}
	renderResult(w, r, 200, true, "Claude is connected", detail)
	if restart {
		go a.restartSoon()
	}
}

// One POST endpoint handles both top-row Disconnect buttons so no extra route
// is needed. Disconnecting means FlipAi returns that agent to the explicit
// Connect state; it does not log the Windows account out of Codex or Claude.
func (a *App) claudeDisconnect(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(strings.TrimSpace(r.FormValue("agent")), "C") {
		if err := a.clearAgentCheck("codex"); err != nil {
			renderResult(w, r, 500, false, "Could not disconnect Codex", err.Error())
			return
		}
		activityLogForStatePath(a.statePath).Add("info", "agent", "Codex disconnected from FlipAi", "", "C", "")
		renderResult(w, r, 200, true, "Codex disconnected",
			"FlipAi will show Connect for Codex again. Your ChatGPT/Codex account sign-in on this Windows account was left alone.")
		return
	}

	had := hasClaudeToken(claudeTokenPath(a.dataDir))
	if err := clearClaudeToken(claudeTokenPath(a.dataDir)); err != nil {
		renderResult(w, r, 500, false, "Could not disconnect Claude", err.Error())
		return
	}
	if err := a.clearAgentCheck("claude"); err != nil {
		renderResult(w, r, 500, false, "Could not disconnect Claude", err.Error())
		return
	}
	a.claudeConnectClient().invalidateLoginCache()
	activityLogForStatePath(a.statePath).Add("info", "agent", "Claude disconnected from FlipAi", "", "A", "")

	detail := "FlipAi no longer holds a long-lived Claude token."
	if !had {
		detail = "There was no stored token to remove."
	}
	conn := a.claudeConnectionNow(r.Context())
	detail += "\n\nClaude Code's own account sign-in on this Windows account was left alone — that is yours, not FlipAi's.\n\n" +
		"Current connection: " + conn.Label + ". " + conn.Detail +
		"\n\nFlipAi will still show Connect at the top until you explicitly connect Claude again."
	restart := a.claudeLiveNeedsRestart()
	if restart {
		detail += liveRestartNote
	}
	renderResult(w, r, 200, true, "Claude disconnected", detail)
	if restart {
		go a.restartSoon()
	}
}

func (a *App) claudeLiveNeedsRestart() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bridge != nil && normalizeClaudeSessionMode(a.cfg.Claude.SessionMode) == claudeSessionModeLive
}

const liveRestartNote = "\n\nLive session mode is on, so the background bridge is restarting to bring the Claude session " +
	"up on this connection."
