package main

import (
    "strings"
    "testing"
)

func TestSharedNumberUsesSMSShortcutAndDefault(t *testing.T) {
    cfg := defaultConfig(t.TempDir())
    cfg.DefaultAgent = "C"
    shared := AgentPhone{Number: "8455551000", Access: AccessAll}
    cfg.Codex.Phones = []AgentPhone{shared}
    cfg.Claude.Phones = []AgentPhone{shared}
    if err := normalizeAgents(&cfg); err != nil {
        t.Fatalf("shared number rejected: %v", err)
    }

    owner, phone, ok := agentForSender(cfg, shared.Number)
    if !ok || owner != "B" || !phone.AllowsSMS() {
        t.Fatalf("shared SMS number resolved to owner=%q phone=%+v ok=%v", owner, phone, ok)
    }

    for raw, want := range map[string]string{
        "C: codex task":  "C",
        "A: claude task": "A",
    } {
        rc, err := parseRemoteCommandForMessage(raw, cfg, owner, GmailMessage{})
        if err != nil || rc.Agent != want {
            t.Errorf("%q routed to %+v, err=%v; want %s", raw, rc, err, want)
        }
    }

    cfg.DefaultAgent = "A"
    rc, err := parseRemoteCommandForMessage("unprefixed task", cfg, owner, GmailMessage{})
    if err != nil || rc.Agent != "A" {
        t.Fatalf("unprefixed shared SMS did not use default Claude: %+v %v", rc, err)
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
    if _, err := parseRemoteCommandForMessage("A: should fail", cfg, owner, GmailMessage{}); err == nil || !strings.Contains(err.Error(), "cannot address") {
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
    if !ok || owner != "B" {
        t.Fatalf("expected shared marker, got %q (ok=%v)", owner, ok)
    }
    rc, err := parseRemoteCommandForMessage("claude1 A: protected task", cfg, owner, GmailMessage{})
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
