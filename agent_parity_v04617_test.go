package main

import (
	"strings"
	"testing"
)

func TestChatGPTDefaultsToDelayedReceipt(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	s := agentSettings(cfg, "G")
	if !s.ackEnabled() {
		t.Fatal("ChatGPT receipt should be enabled by default")
	}
	if s.AckDelaySeconds != 30 {
		t.Fatalf("ChatGPT receipt delay = %d, want 30 seconds", s.AckDelaySeconds)
	}
	if agentSettings(cfg, "C").AckDelaySeconds != 0 || agentSettings(cfg, "A").AckDelaySeconds != 0 {
		t.Fatal("Codex and Claude should keep immediate receipts")
	}
}

func TestChatGPTHasIndependentPhonePermission(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Security.ChatGPTAgentMigrated = true
	cfg.ChatGPT.Phones = []AgentPhone{{Number: "8455551212", Access: AccessSMS}}
	if agent, _, ok := agentForSender(cfg, "8455551212"); !ok || agent != "G" {
		t.Fatalf("ChatGPT-only phone resolved as agent=%q ok=%v", agent, ok)
	}
	if smsTargetAllowed("G", "C") || smsTargetAllowed("G", "A") || !smsTargetAllowed("G", "G") {
		t.Fatal("ChatGPT-only phone can cross agent boundaries")
	}
}

func TestOneSMSInstructionAppliesToAllAgents(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.GoogleVoice.ReplyStyleHint = "One shared line"
	cfg.Codex.Instruction = "old Codex override"
	cfg.Claude.Instruction = "old Claude override"
	for _, agent := range []string{"C", "A", "G"} {
		if got := cfg.replyStyleHintFor(agent); got != "One shared line" {
			t.Fatalf("%s instruction = %q", agent, got)
		}
	}
}

func TestChatGPTShortcutIsConfigurable(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.ChatGPTPrefix = "CHAT"
	if explicitSMSAgent("CHAT: hello", cfg) != "G" {
		t.Fatal("custom ChatGPT shortcut did not select G")
	}
}

func TestAgentsUIExposesSharedParityControls(t *testing.T) {
	for _, want := range []string{"chatgptPrefix", "chatgptRequireCode", "chatgptAckDelay", "sharedReplyStyle", "No default agent."} {
		if !strings.Contains(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML)), want) {
			t.Fatalf("Agents UI missing %q", want)
		}
	}
}
