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
//   - A `claude /login` session is the browser sign-in Claude Code itself uses.
//     It is what the Claude in Chrome extension can authenticate against, and
//     what Remote Control needs to open the conversation at claude.ai/code.
//   - A `claude setup-token` value can only make model requests. Claude Code
//     turns Chrome off for a token session even when --chrome is passed, and
//     Remote Control refuses it.
//
// FlipAi used to let an install end up token-only by accident: the token is the
// thing a user is asked to paste, nothing ever asked for the login, and the
// consequence only showed up as Claude texting back that it could not reach the
// browser. The connect flow below makes the login the thing FlipAi actually
// connects, and demotes the token to what it is — the fallback that keeps an
// unattended bridge answering when that sign-in lapses.
const (
	// claudeConnLogin is the good state: a real sign-in exists on this Windows
	// account, so Chrome and Remote Control both work. A token may also be
	// stored; it is simply not used while the sign-in is valid.
	claudeConnLogin = "login"
	// claudeConnToken is the state this flow exists to end: a token and nothing
	// else, which answers texts but cannot touch the browser.
	claudeConnToken = "token"
	// claudeConnNone means Claude cannot run at all yet.
	claudeConnNone = "none"
	// claudeConnUnknown means nothing has probed the machine yet.
	claudeConnUnknown = "unknown"
)

// claudeConnection is how the Agents page describes the Claude connection, and
// what every handler here returns to the user.
type claudeConnection struct {
	Kind   string
	Label  string
	Detail string

	// ChromeReady reports whether the credential in use can drive Chrome and
	// open Remote Control. Both need the same thing — a real sign-in — so they
	// are one flag rather than two.
	ChromeReady bool

	// NeedsSignIn marks the states Connect Claude fixes.
	NeedsSignIn bool
}

// evaluateClaudeConnection turns the two facts FlipAi can establish about a
// machine into the connection it actually has. It is pure so every state can be
// asserted in a test rather than only on a machine that happens to be in it.
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
			Detail: "Connected the right way: FlipAi runs SMS turns on this account's `claude /login` session, " +
				"so Chrome control and the claude.ai/code view both work. The stored token is held back as the " +
				"fallback and used only if that sign-in ever lapses.",
		}
	case loginExists:
		return claudeConnection{
			Kind:        claudeConnLogin,
			Label:       "Claude Code sign-in",
			ChromeReady: true,
			Detail: "Connected the right way: FlipAi runs SMS turns on this account's `claude /login` session, " +
				"so Chrome control and the claude.ai/code view both work.",
		}
	case hasToken:
		return claudeConnection{
			Kind:        claudeConnToken,
			Label:       "Stored token only",
			NeedsSignIn: true,
			Detail: "Claude will answer texts, but it cannot control Chrome and cannot appear at claude.ai/code: " +
				"a `claude setup-token` value can only make model requests, and Claude Code turns the browser off " +
				"for it. Press Connect Claude to sign in with `claude /login` on this Windows account — the token " +
				"stays as the fallback.",
		}
	default:
		return claudeConnection{
			Kind:        claudeConnNone,
			Label:       "Not connected",
			NeedsSignIn: true,
			Detail: "Claude Code is not signed in on this Windows account, so FlipAi cannot run a Claude turn at all. " +
				"Press Connect Claude to sign in.",
		}
	}
}

// claudeSignInArgs builds the console command that completes the sign-in.
//
// FlipAi cannot do this itself: `/login` is an interactive browser flow, and
// Claude Code stores the result under whichever Windows account ran it — which
// is exactly the account whose credential the Chrome extension has to match.
// So FlipAi opens a real console window on the user's desktop and lets Claude
// Code run its own flow there.
//
// /k rather than /c: the window stays open after the flow finishes, so the user
// can read the outcome, and can retry in place if Claude Code asks for
// anything else.
func claudeSignInArgs(exe string) string {
	return fmt.Sprintf(`/k "%s" /login`, strings.TrimSpace(exe))
}

// claudeConnectClient returns the client whose cached probe the SMS turns
// actually read, so an invalidation here is one the bridge sees. Before the
// bridge is up there is nothing to share and a fresh client is equivalent.
func (a *App) claudeConnectClient() *ClaudeClient {
	a.mu.Lock()
	c, cfg := a.claude, a.cfg
	a.mu.Unlock()
	if c != nil {
		return c
	}
	return a.newClaudeClient(cfg)
}

// claudeConnectionNow probes the machine and reports the connection it has.
func (a *App) claudeConnectionNow(ctx context.Context) claudeConnection {
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	exists := a.claudeConnectClient().RefreshLogin(pctx)
	return evaluateClaudeConnection(hasClaudeToken(claudeTokenPath(a.dataDir)), true, exists)
}

// cachedClaudeConnection is the render-safe form: it reports whatever the last
// probe found and never starts a subprocess from a page render.
func (a *App) cachedClaudeConnection() claudeConnection {
	a.mu.Lock()
	c := a.claude
	a.mu.Unlock()
	checked, exists := c.CachedLogin()
	return evaluateClaudeConnection(hasClaudeToken(claudeTokenPath(a.dataDir)), checked, exists)
}

// warmClaudeConnection establishes the connection state once at bridge start,
// so the Agents page describes the machine before anyone sends a text, and so a
// bridge that can only answer without the browser says so in the Activity log
// rather than leaving Claude to report it over SMS.
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
			"\", which cannot control Chrome or reach claude.ai/code. Press Connect Claude on the Agents page.", "", "A", "")
	}
}

// claudeConnect opens the sign-in window. It deliberately does not wait for the
// flow to finish: the user completes it at the console, then presses Check
// connection, which is the step that decides what FlipAi actually has.
func (a *App) claudeConnect(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	exe := resolveClaudeExecutable(cfg.ClaudePath)
	if !executableExists(exe) {
		renderResult(w, r, 400, false, "Claude Code was not found",
			"FlipAi could not find the Claude Code CLI at "+exe+
				". Set the executable path under Workspace & paths, then press Connect Claude again.")
		return
	}
	if err := startClaudeSignIn(exe, cfg.claudeWorkingDir()); err != nil {
		renderResult(w, r, 500, false, "Could not open the Claude sign-in window",
			"FlipAi could not start a console for the sign-in: "+err.Error()+
				"\n\nOpen PowerShell on this Windows account, run `"+exe+"` and complete /login there, then press Check connection.")
		return
	}
	// Whatever the probe last found is about to stop being true.
	a.claudeConnectClient().invalidateLoginCache()
	activityLogForStatePath(a.statePath).Add("info", "agent", "Claude sign-in window opened from the Agents page", "", "A", "")
	renderResult(w, r, 200, true, "Finish the Claude sign-in in the window that opened",
		"A console window is running the Claude Code sign-in. Complete it in the browser it opens, "+
			"then come back here and press Check connection.\n\n"+
			"This is the sign-in the Claude in Chrome extension authenticates against, so it is what lets a text "+
			"drive your browser. Any long-lived token you have saved stays in place as the fallback.")
}

// claudeConnectVerify is the step that makes the new sign-in real for FlipAi:
// it re-probes, and rebuilds anything that was built against the old credential.
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
	activityLogForStatePath(a.statePath).Add("success", "agent", "Claude connected with a `claude /login` session; Chrome and Remote Control are available", "", "A", "")
	detail := conn.Detail
	if !useChrome {
		detail += "\n\nLet Claude control Chrome is currently switched off under Access & tools; turn it on if you want texts to use the browser."
	}
	restart := a.claudeLiveNeedsRestart()
	if restart {
		detail += liveRestartNote
	}
	renderResult(w, r, 200, true, "Claude is connected the right way", detail)
	if restart {
		go a.restartSoon()
	}
}

// claudeDisconnect clears what FlipAi stores for Claude so the next Connect
// starts clean. It removes FlipAi's own copy of the long-lived token and every
// cached answer about the sign-in; it never runs `claude logout`, because the
// CLI sign-in belongs to the Windows account rather than to FlipAi.
func (a *App) claudeDisconnect(w http.ResponseWriter, r *http.Request) {
	had := hasClaudeToken(claudeTokenPath(a.dataDir))
	if err := clearClaudeToken(claudeTokenPath(a.dataDir)); err != nil {
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
	detail += "\n\nClaude Code's own sign-in on this Windows account was left alone — that is yours, not FlipAi's.\n\n" +
		"Current connection: " + conn.Label + ". " + conn.Detail
	restart := a.claudeLiveNeedsRestart()
	if restart {
		detail += liveRestartNote
	}
	renderResult(w, r, 200, true, "Claude disconnected", detail)
	if restart {
		go a.restartSoon()
	}
}

// claudeLiveNeedsRestart reports whether a supervised session is running
// against the credential that has just changed.
//
// Print mode needs nothing: it reads the refreshed probe on its next turn. A
// live session is a process that was started with whichever credential was
// current at the time, so it has to come back to pick the new one up — and the
// bridge restart every settings save already uses is the tested way to do that.
func (a *App) claudeLiveNeedsRestart() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bridge != nil && normalizeClaudeSessionMode(a.cfg.Claude.SessionMode) == claudeSessionModeLive
}

// liveRestartNote is appended to whatever a connect handler reports, so a
// restart the user is about to see is one they were told about.
const liveRestartNote = "\n\nLive session mode is on, so the background bridge is restarting to bring the Claude session " +
	"up on this connection."
