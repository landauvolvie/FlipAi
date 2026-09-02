package main

import "testing"

func TestChatGPTPrefixRoutesFromExistingAllowedAgent(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Codex.AgentSettings.RequireCode = false
	rc, err := parseRemoteCommand("G: explain this briefly", cfg, "C")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Agent != "G" || rc.Text != "explain this briefly" || rc.New {
		t.Fatalf("unexpected ChatGPT route: %+v", rc)
	}
}

func TestChatGPTNewCommandUsesGPrefix(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	rc, err := parseRemoteCommand("G NEW", cfg, "A")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Agent != "G" || !rc.New {
		t.Fatalf("unexpected ChatGPT new route: %+v", rc)
	}
}

func TestSharedNumberChatGPTRouteKeepsExistingSecurityGate(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.DefaultAgent = "C"
	if err := setAgentCode(&cfg, "C", "secret1"); err != nil {
		t.Fatal(err)
	}
	s := cfg.Codex.AgentSettings
	s.RequireCode = true
	cfg.Codex.AgentSettings = s
	agent := agentForSharedSMSCommand("secret1 G: hello", cfg)
	if agent != "C" {
		t.Fatalf("shared ChatGPT command should use the default agent security gate, got %q", agent)
	}
	rc, err := parseRemoteCommand("secret1 G: hello", cfg, agent)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Agent != "G" || rc.Text != "hello" {
		t.Fatalf("unexpected secured ChatGPT route: %+v", rc)
	}
	if _, err := parseRemoteCommand("wrong G: hello", cfg, agent); err == nil {
		t.Fatal("wrong existing agent security code should still reject ChatGPT routing")
	}
}
