package main

import (
	"strings"
	"testing"
)

func TestAgentsUIShowsPublicSMSRouteMap(t *testing.T) {
	body := smsRouteAgentsUI(copilotChatDirectUI(exactWebAgentsHTML()))
	for _, want := range []string{
		"O = ChatGPT Chat",
		"OW = ChatGPT Work",
		"OC = Codex",
		"A = Claude Chat",
		"AW = Claude Cowork",
		"AC = Claude Code Web",
		"AL = Claude Code Local",
		"G = Gemini",
		"M = Microsoft Copilot",
		"X = Grok",
		"OW NEW: research this",
		"Answers OC: messages",
		"Answers AL: messages",
		"Answers G: messages",
		"Answers M: messages",
		"Answers X: messages",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Agents UI missing public routing text %q", want)
		}
	}
	for _, stale := range []string{
		"Answers {{.S.CodexPrefix}}: messages",
		"Answers {{.S.ClaudePrefix}}: messages",
		"Answers {{.CopilotChatAccess.Prefix}}: messages",
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("Agents UI still exposes stale internal shortcut %q", stale)
		}
	}
}
