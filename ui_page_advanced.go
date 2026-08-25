package main

import "strings"

// What used to be the Advanced page now lives at the bottom of Settings: the
// loopback service, the log files, and the actions that restart or stop local
// processes. Only the helpers those cards need survive here.
type healthRow struct {
	Label, Value, Tone string
}

func agentSessionsSub(s uiStatus) string {
	var parts []string
	if s.CodexThreadActive {
		parts = append(parts, "Codex thread")
	}
	if s.ClaudeSessionActive {
		parts = append(parts, "Claude session")
	}
	if len(parts) == 0 {
		return "No agent conversation open"
	}
	return strings.Join(parts, " · ") + " open"
}

func readyText(ok bool, good, bad string) string {
	if ok {
		return good
	}
	return bad
}

func toneClass(ok bool) string {
	if ok {
		return "ok"
	}
	return "warn"
}

func pausedOrStopped(s uiStatus) string {
	if s.Paused {
		return "Paused"
	}
	return "Waiting for setup"
}
