package main

import (
	"os"
	"strings"
	"testing"
)

// A content filter may block one AI provider while allowing the others. Each
// browser provider therefore has to own its own WebView profile, child process,
// loopback control endpoint, token, and runtime state. A provider failure must
// never request a FlipAi-wide quit.
func TestBrowserProvidersRemainIndependentWhenOneSiteIsFiltered(t *testing.T) {
	providers := []struct {
		file    string
		profile string
		worker  string
		mutate  string
	}{
		{"chatgpt_webview_windows.go", "chatGPTProfilePath", "--chatgpt-worker", "mutateChatGPTRuntime"},
		{"claude_chat_webview_windows.go", "claudeChatProfilePath", "--claude-chat-worker", "mutateClaudeChatRuntime"},
		{"gemini_chat_webview_windows.go", "geminiChatProfilePath", "--gemini-chat-worker", "mutateGeminiChatRuntime"},
		{"grok_chat_webview_windows.go", "grokChatProfilePath", "--grok-chat-worker", "mutateGrokChatRuntime"},
	}

	for _, p := range providers {
		raw, err := os.ReadFile(p.file)
		if err != nil {
			t.Fatalf("read %s: %v", p.file, err)
		}
		s := string(raw)
		for _, want := range []string{
			p.profile,
			p.worker,
			p.mutate,
			`net.Listen("tcp", "127.0.0.1:0")`,
			`X-FlipAi-Token`,
		} {
			if !strings.Contains(s, want) {
				t.Fatalf("%s lost provider-isolation safeguard %q", p.file, want)
			}
		}
		if strings.Contains(s, "requestQuit(") {
			t.Fatalf("%s can turn an agent-local browser failure into a FlipAi-wide shutdown", p.file)
		}
	}
}

func TestFilteredProviderErrorSaysOtherAgentsKeepWorking(t *testing.T) {
	for _, raw := range []string{
		"net::ERR_BLOCKED_BY_CLIENT",
		"net::ERR_BLOCKED_BY_ADMINISTRATOR",
		"DNS_PROBE_FINISHED_NXDOMAIN",
		"net::ERR_NAME_NOT_RESOLVED",
		"net::ERR_CONNECTION_REFUSED",
	} {
		got := friendlyAgentMessage(raw)
		low := strings.ToLower(got)
		if !strings.Contains(low, "filter") || !strings.Contains(low, "other agents") || !strings.Contains(low, "keep working") {
			t.Fatalf("filtered provider error %q did not explain isolation: %q", raw, got)
		}
	}
}
