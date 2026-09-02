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
	cfg.Codex.Phones = []AgentPhone{{Number: "18455550123", Access: AccessSMS}}
	cfg.Claude.Phones = []AgentPhone{{Number: "18455550123", Access: AccessSMS}}
	cfg.ChatGPT.Phones = []AgentPhone{{Number: "18455550123", Access: AccessSMS}}
	return cfg
}

func TestStickySMSRoutingHasNoDefaultAndSupportsG(t *testing.T) {
	cfg := stickyRoutingConfig(t)
	owner, _, ok := agentForSender(cfg, "18455550123")
	if !ok || owner != "CAG" {
		t.Fatalf("shared sender marker=%q ok=%v", owner, ok)
	}
	if _, err := selectStickySMSAgent("hello", cfg, owner, ""); err == nil || !strings.Contains(err.Error(), "C:") || !strings.Contains(err.Error(), "G:") {
		t.Fatalf("shared sender without sticky agent should be told to select one: %v", err)
	}
	rc, err := parseRemoteCommandForMessageSticky("G: hi", cfg, owner, "", GmailMessage{})
	if err != nil || rc.Agent != "G" || rc.Text != "hi" {
		t.Fatalf("G routing failed: rc=%+v err=%v", rc, err)
	}
	rc, err = parseRemoteCommandForMessageSticky("follow up", cfg, owner, "G", GmailMessage{})
	if err != nil || rc.Agent != "G" || rc.Text != "follow up" {
		t.Fatalf("sticky G follow-up failed: rc=%+v err=%v", rc, err)
	}
	rc, err = parseRemoteCommandForMessageSticky("A: switch", cfg, owner, "G", GmailMessage{})
	if err != nil || rc.Agent != "A" || rc.Text != "switch" {
		t.Fatalf("switch to A failed: rc=%+v err=%v", rc, err)
	}
	rc, err = parseRemoteCommandForMessageSticky("next", cfg, owner, "A", GmailMessage{})
	if err != nil || rc.Agent != "A" || rc.Text != "next" {
		t.Fatalf("sticky A follow-up failed: rc=%+v err=%v", rc, err)
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
