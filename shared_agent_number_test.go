package main

import (
	"strings"
	"testing"
)

func TestSharedNumberUsesShortcutThenStickyRoutingWithNoSMSDefault(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	shared := AgentPhone{Number: "8455551000", Access: AccessAll}
	cfg.Codex.Phones = []AgentPhone{shared}
	cfg.Claude.Phones = []AgentPhone{shared}
	if err := normalizeAgents(&cfg); err != nil {
		t.Fatalf("shared number rejected: %v", err)
	}

	owner, phone, ok := agentForSender(cfg, shared.Number)
	if !ok || owner != "CA" || !phone.AllowsSMS() {
		t.Fatalf("shared SMS number resolved to owner=%q phone=%+v ok=%v", owner, phone, ok)
	}
	if _, err := parseRemoteCommandForMessageSticky("unprefixed task", cfg, owner, "", GmailMessage{}); err == nil || !strings.Contains(err.Error(), "no SMS agent is selected") {
		t.Fatalf("shared SMS unexpectedly used a default agent: %v", err)
	}

	for raw, want := range map[string]string{
		"C: codex task":  "C",
		"A: claude task": "A",
	} {
		rc, err := parseRemoteCommandForMessageSticky(raw, cfg, owner, "", GmailMessage{})
		if err != nil || rc.Agent != want {
			t.Errorf("%q routed to %+v, err=%v; want %s", raw, rc, err, want)
		}
		follow, err := parseRemoteCommandForMessageSticky("follow up", cfg, owner, want, GmailMessage{})
		if err != nil || follow.Agent != want || follow.Text != "follow up" {
			t.Errorf("sticky follow-up for %s failed: %+v err=%v", want, follow, err)
		}
	}
}

func TestSharedNumberDoesNotWidenPerAgentSMSAccess(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Codex.Phones = []AgentPhone{{Number: "8455551000", Access: AccessSMS}}
	cfg.Claude.Phones = []AgentPhone{{Number: "8455551000", Access: AccessVoice}}
	if err := normalizeAgents(&cfg); err != nil {
		t.Fatal(err)
	}

	owner, phone, ok := agentForSender(cfg, "8455551000")
	if !ok || owner != "C" || !phone.AllowsSMS() {
		t.Fatalf("SMS permission should remain Codex-only: owner=%q phone=%+v ok=%v", owner, phone, ok)
	}
	if _, err := parseRemoteCommandForMessageSticky("A: should fail", cfg, owner, "", GmailMessage{}); err == nil || !strings.Contains(err.Error(), "cannot address") {
		t.Fatalf("Claude voice-only copy unexpectedly gained SMS permission: %v", err)
	}
}

func TestSharedNumberSecurityCodeCanPrecedeShortcut(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	shared := AgentPhone{Number: "8455551000", Access: AccessAll}
	cfg.Codex.Phones = []AgentPhone{shared}
	cfg.Claude.Phones = []AgentPhone{shared}
	if err := setAgentCode(&cfg, "A", "claude1"); err != nil {
		t.Fatal(err)
	}
	s := cfg.Claude.AgentSettings
	s.RequireCode = true
	cfg.Claude.AgentSettings = s
	if err := normalizeAgents(&cfg); err != nil {
		t.Fatal(err)
	}

	owner, _, ok := agentForSender(cfg, "8455551000")
	if !ok || owner != "CA" {
		t.Fatalf("expected shared marker, got %q (ok=%v)", owner, ok)
	}
	rc, err := parseRemoteCommandForMessageSticky("claude1 A: protected task", cfg, owner, "", GmailMessage{})
	if err != nil || rc.Agent != "A" || rc.Text != "protected task" {
		t.Fatalf("security-code + shortcut routing failed: %+v %v", rc, err)
	}
}

func TestSharedVoiceCallerUsesDefaultAgentWhenBothAllowCalls(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.DefaultAgent = "A"
	cfg.Codex.Phones = []AgentPhone{{Number: "8455551000", Access: AccessAll}}
	cfg.Claude.Phones = []AgentPhone{{Number: "8455551000", Access: AccessAll}}
	if err := normalizeAgents(&cfg); err != nil {
		t.Fatal(err)
	}
	vc := defaultVoiceCallConfig()
	vc.Enabled = true
	d := decideVoiceCall(vc, cfg, "8455551000", "")
	if !d.Allowed || d.Agent != "A" {
		t.Fatalf("shared caller should use default Claude when both allow calls: %+v", d)
	}
}
