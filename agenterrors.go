package main

import "strings"

// Agent failures arrive as whatever the CLI printed — for Claude that is a full
// JSON result object hundreds of characters long. Truncated to an SMS it is
// unreadable, and in Settings it rendered as a wall of JSON. friendlyAgentError
// turns the failures we recognise into one sentence saying what to actually do.
//
// Anything unrecognised is returned unchanged, so a new failure mode is never
// hidden behind a guess.
func friendlyAgentError(err error) string {
	if err == nil {
		return ""
	}
	return friendlyAgentMessage(err.Error())
}

func friendlyAgentMessage(raw string) string {
	s := strings.ToLower(raw)

	switch {
	// Browser/network filters are intentionally treated as an agent-local
	// availability problem. FlipAi never tells the user to bypass a filter and
	// never turns a blocked provider into a bridge-wide failure; the other agents
	// remain usable while this one reports unavailable.
	case strings.Contains(s, "err_blocked_by_client"),
		strings.Contains(s, "err_blocked_by_administrator"),
		strings.Contains(s, "blocked by your administrator"),
		strings.Contains(s, "dns_probe_finished_nxdomain"),
		strings.Contains(s, "err_name_not_resolved"),
		strings.Contains(s, "err_connection_refused"),
		strings.Contains(s, "err_proxy_connection_failed"),
		strings.Contains(s, "err_tunnel_connection_failed"):
		return "This agent's service is unreachable from this PC. A network or content filter may be blocking it. FlipAi and your other agents can keep working."

	// Claude Code's own sign-in lapsed. Its refresh already failed, so nothing
	// FlipAi can do automatically — but the token path avoids a repeat.
	case strings.Contains(s, "oauth session expired"),
		strings.Contains(s, "could not be refreshed"),
		strings.Contains(s, "oauth token has expired"):
		return "Claude sign-in on the PC has expired. Run `claude setup-token` in PowerShell and paste the token into FlipAi Settings (or run `claude /login`)."

	case strings.Contains(s, "invalid bearer token"),
		strings.Contains(s, "authentication_error"),
		strings.Contains(s, "oauth 401"):
		return "Claude rejected the stored sign-in. Run `claude setup-token` and paste the new token into FlipAi Settings."

	case strings.Contains(s, "please run /login"),
		strings.Contains(s, "not logged in"):
		return "Claude Code is not signed in on this PC. Run `claude /login` in PowerShell, or paste a `claude setup-token` value into FlipAi Settings."

	// Billing guards: FlipAi refuses API/Console paths on purpose.
	case strings.Contains(s, "api/console billing"),
		strings.Contains(s, "refuses external/provider billing"),
		strings.Contains(s, "refuses that billing path"):
		return "Claude is set up for API/Console billing. FlipAi only uses a Claude subscription sign-in."

	// Codex conversation history is gone. runCodex recovers automatically, so
	// this text only shows if the recovery itself could not start a new thread.
	case strings.Contains(s, "no rollout found"),
		strings.Contains(s, "-32600"):
		return "The stored Codex conversation no longer exists. Text `C NEW` to start a fresh one."

	case strings.Contains(s, "not signed in with chatgpt"),
		strings.Contains(s, "not authenticated with sign in with chatgpt"):
		return "Codex is not signed in with ChatGPT on this PC. Open Codex and sign in, then try again."

	case strings.Contains(s, "codex app-server is not running"),
		strings.Contains(s, "codex app server stopped"):
		return "The Codex background service is not running on the PC. Open Codex once, then try again."
	}

	return raw
}
