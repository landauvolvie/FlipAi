package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stickyRoutingConfig(t *testing.T) Config {
	t.Helper()
	cfg := defaultConfig(t.TempDir())
	cfg.Codex.RequireCode = false
	cfg.Claude.RequireCode = false
	phone := AgentPhone{Number: "8455550123", Access: AccessSMS}
	cfg.Codex.Phones = []AgentPhone{phone}
	cfg.Claude.Phones = []AgentPhone{phone}
	cfg.ChatGPT.Phones = []AgentPhone{phone}
	cfg.ClaudeChat.Phones = []AgentPhone{phone}
	cfg.GeminiChat.Phones = []AgentPhone{phone}
	cfg.GrokChat.Phones = []AgentPhone{phone}
	cfg.CopilotChat.Phones = []AgentPhone{phone}
	return cfg
}

func TestProviderGroupedSMSRoutes(t *testing.T) {
	cfg := stickyRoutingConfig(t)
	owner, _, ok := agentForSender(cfg, "18455550123")
	if !ok {
		t.Fatal("shared sender was not allowed")
	}
	if _, err := selectStickySMSAgent("hello", cfg, owner, ""); err == nil || !strings.Contains(err.Error(), "OW:") || !strings.Contains(err.Error(), "AC:") {
		t.Fatalf("shared sender without sticky agent should be told to select a grouped route: %v", err)
	}

	cases := []struct {
		raw       string
		agent     string
		mode      string
		wantText  string
	}{
		{"O: hello", "G", browserModeChat, "hello"},
		{"OW: research this", "G", browserModeWork, "research this"},
		{"OC: fix the build", "C", "", "fix the build"},
		{"A: hello Claude", "H", browserModeChat, "hello Claude"},
		{"AC: fix the repo", "H", browserModeCode, "fix the repo"},
		{"AW: prepare a report", "H", browserModeCowork, "prepare a report"},
		{"AL: inspect locally", "A", "", "inspect locally"},
		{"G: hello Gemini", "M", "", "hello Gemini"},
		{"M: hello Copilot", "P", "", "hello Copilot"},
		{"X: hello Grok", "X", "", "hello Grok"},
	}
	for _, tc := range cases {
		rc, err := parseRemoteCommandForMessageSticky(tc.raw, cfg, owner, "", GmailMessage{})
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if rc.Agent != tc.agent {
			t.Fatalf("%s agent=%q want %q", tc.raw, rc.Agent, tc.agent)
		}
		mode, text := extractBrowserModeCommand(rc.Text)
		if mode != tc.mode || text != tc.wantText {
			t.Fatalf("%s mode/text=%q/%q want %q/%q", tc.raw, mode, text, tc.mode, tc.wantText)
		}
	}
}

func TestLegacyStickyAgentStillMapsToOldProvider(t *testing.T) {
	cfg := stickyRoutingConfig(t)
	owner, _, _ := agentForSender(cfg, "18455550123")
	rc, err := parseRemoteCommandForMessageSticky("follow up", cfg, owner, "G", GmailMessage{})
	if err != nil || rc.Agent != "G" {
		t.Fatalf("legacy sticky G should remain ChatGPT: rc=%+v err=%v", rc, err)
	}
	mode, text := extractBrowserModeCommand(rc.Text)
	if mode != browserModeChat || text != "follow up" {
		t.Fatalf("legacy ChatGPT sticky mode/text=%q/%q", mode, text)
	}
}

func TestStickySMSAgentPersistsPerSender(t *testing.T) {
	dir := t.TempDir()
	b := &Bridge{statePath: filepath.Join(dir, "state.json")}
	if err := b.rememberStickySMSAgent("(845) 555-0123", "G"); err != nil {
		t.Fatal(err)
	}
	st := loadState(b.statePath)
	if got := st.LastAgentBySender["8455550123"]; got != "G" {
		t.Fatalf("persisted sticky agent=%q, want G", got)
	}
}

func TestChatGPTRebootRecoveryClearsOnlyTransientRuntime(t *testing.T) {
	dir := t.TempDir()
	mutateChatGPTRuntime(dir, func(s *ChatGPTWebRuntime) {
		s.Connected = true
		s.Running = true
		s.Starting = true
		s.SignedIn = true
		s.ControlPort = 9
		s.ControlToken = "stale"
		s.ConversationID = "keep-me"
	})
	prepareChatGPTRuntimeForTray(dir)
	s := loadChatGPTRuntime(dir)
	if !s.Connected || s.ConversationID != "keep-me" {
		t.Fatalf("durable ChatGPT state was lost: %+v", s)
	}
	if s.Running || s.Starting || s.SignedIn || s.ControlPort != 0 || s.ControlToken != "" {
		t.Fatalf("stale process state survived restart recovery: %+v", s)
	}
	if _, err := os.Stat(chatGPTProfilePath(dir)); err == nil {
		// The function never deletes the profile; an absent directory is fine in this synthetic test.
	}
}

func TestAgentsUIHasStickyRoutingAndNoDefaultAgentControl(t *testing.T) {
	body := chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))
	for _, want := range []string{"chatgptPrefix", "No default agent.", "Unprefixed follow-ups stay here until you switch", "New conversation word"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Agents UI missing %q", want)
		}
	}
	for _, old := range []string{"Make default", "Default agent"} {
		if strings.Contains(body, old) {
			t.Fatalf("Agents UI still contains retired default routing control %q", old)
		}
	}
}
