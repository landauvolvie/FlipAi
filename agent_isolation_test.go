package main

import (
	"strings"
	"testing"
)

// Permissions belong to one agent. A number, or a caller name, allowed under
// ChatGPT/Codex must not reach Claude by any route -- not by texting, not by
// asking for the other agent by name, not by calling, and not through a
// default that quietly picks an agent nobody allowed.
//
// This is one test rather than several because the guarantee is one sentence
// and every path has to keep it.
func TestOneAgentsPermissionsNeverReachTheOther(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.DefaultAgent = "A" // the wrong answer everywhere below
	cfg.Codex.Phones = []AgentPhone{{Number: "8455551000", Access: AccessAll}}
	cfg.Codex.CallerNames = "Dana Codex"
	cfg.Claude.Phones = []AgentPhone{{Number: "8455552000", Access: AccessAll}}
	cfg.Claude.CallerNames = "Robin Claude"
	if err := normalizeAgents(&cfg); err != nil {
		t.Fatalf("test configuration was rejected: %v", err)
	}

	// 1. A text from a ChatGPT number is a ChatGPT text, whatever it says.
	if agent, _, ok := agentForSender(cfg, "8455551000"); !ok || agent != "C" {
		t.Fatalf("a ChatGPT number resolved to %q (ok=%v)", agent, ok)
	}
	for _, addressed := range []string{"A: what is on my calendar", "A NEW", "a: hello"} {
		if _, err := parseRemoteCommand(addressed, cfg, "C"); err == nil {
			t.Errorf("a ChatGPT number reached Claude with %q", addressed)
		} else if !strings.Contains(err.Error(), "cannot address") {
			t.Errorf("%q was refused for the wrong reason: %v", addressed, err)
		}
	}
	// The same text from the Claude number is fine, which is what makes the
	// refusal above about permission rather than about the prefix.
	if rc, err := parseRemoteCommand("A: what is on my calendar", cfg, "A"); err != nil || rc.Agent != "A" {
		t.Errorf("the Claude number could not address Claude: %+v %v", rc, err)
	}

	// 2. An unprefixed text goes to the agent that owns the number, never to
	//    the configured default agent.
	rc, err := parseRemoteCommand("summarize my inbox", cfg, "C")
	if err != nil || rc.Agent != "C" {
		t.Fatalf("an unprefixed text from a ChatGPT number went to %+v (%v)", rc, err)
	}

	// 3. A number that is on nobody's list reaches nobody.
	if _, _, ok := agentForSender(cfg, "8455559999"); ok {
		t.Error("an unlisted number was matched to an agent")
	}

	// 4. Calls. The same lists decide, per agent.
	vc := defaultVoiceCallConfig()
	vc.Enabled = true
	if d := decideVoiceCall(vc, cfg, "8455551000", ""); !d.Allowed || d.Agent != "C" {
		t.Errorf("a ChatGPT caller was not routed to ChatGPT: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "8455552000", ""); !d.Allowed || d.Agent != "A" {
		t.Errorf("a Claude caller was not routed to Claude: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "", "Dana Codex"); !d.Allowed || d.Agent != "C" {
		t.Errorf("a ChatGPT caller name was not routed to ChatGPT: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "", "Robin Claude"); !d.Allowed || d.Agent != "A" {
		t.Errorf("a Claude caller name was not routed to Claude: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "8455559999", ""); d.Allowed {
		t.Errorf("an unlisted number was allowed on a call: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "", "Somebody Else"); d.Allowed {
		t.Errorf("an unlisted caller name was allowed on a call: %+v", d)
	}

	// 5. Turning an agent's own call switch on does not widen who may call it.
	//    That switch says "this agent may take calls", never "anyone may call".
	both := vc
	both.Codex.Enabled, both.Claude.Enabled = true, true
	if d := decideVoiceCall(both, cfg, "8455559999", ""); d.Allowed {
		t.Errorf("switching both agents on let an unlisted number through: %+v", d)
	}
	if d := decideVoiceCall(both, cfg, "8455551000", ""); !d.Allowed || d.Agent != "C" {
		t.Errorf("a ChatGPT number was not routed to ChatGPT with both switches on: %+v", d)
	}
}

// The same name may not be an allowed caller on both agents. Two claims on one
// caller can only be settled by a default, and a default is exactly how a
// permission granted on one agent starts deciding for the other.
func TestACallerNameBelongsToOneAgent(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Codex.CallerNames = "Dana Codex"
	cfg.Claude.CallerNames = "dana codex"
	err := normalizeAgents(&cfg)
	if err == nil {
		t.Fatal("the same caller name was accepted on both agents")
	}
	if !strings.Contains(err.Error(), "one agent only") {
		t.Errorf("the refusal did not explain itself: %v", err)
	}

	// A phone number may deliberately be shared. SMS routing is then explicit
	// through C:/A: while each agent still keeps its own access policy.
	dup := defaultConfig(t.TempDir())
	dup.Codex.Phones = []AgentPhone{{Number: "8455551000", Access: AccessAll}}
	dup.Claude.Phones = []AgentPhone{{Number: "(845) 555-1000", Access: AccessAll}}
	if err := normalizeAgents(&dup); err != nil {
		t.Fatalf("the same phone should be allowed on both agents: %v", err)
	}
	if len(dup.Codex.Phones) != 1 || len(dup.Claude.Phones) != 1 {
		t.Fatalf("shared phone was not preserved on both agents: C=%+v A=%+v", dup.Codex.Phones, dup.Claude.Phones)
	}
}

// A number allowed to text must not thereby be allowed to call, and the other
// way round. The access setting is part of the permission, not decoration.
func TestAccessKindIsPartOfThePermission(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Codex.Phones = []AgentPhone{
		{Number: "8455551000", Access: AccessSMS},
		{Number: "8455552000", Access: AccessVoice},
	}
	if err := normalizeAgents(&cfg); err != nil {
		t.Fatal(err)
	}
	vc := defaultVoiceCallConfig()
	vc.Enabled = true

	if d := decideVoiceCall(vc, cfg, "8455551000", ""); d.Allowed {
		t.Errorf("a texts-only number was allowed to call: %+v", d)
	} else if !strings.Contains(d.Reason, "not to call") {
		t.Errorf("the refusal did not say why: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "8455552000", ""); !d.Allowed {
		t.Errorf("a calls-only number was refused: %+v", d)
	}
	if allowed := smsAllowedFrom(cfg); strings.Contains(allowed, "8455552000") {
		t.Error("a calls-only number was put on the SMS allowlist")
	} else if !strings.Contains(allowed, "8455551000") {
		t.Error("a texts-only number was left off the SMS allowlist")
	}
}

// A stored configuration that somehow claims one caller on both agents is
// repaired on load rather than left to a default to settle. This is the path a
// file hand-edited, downgraded, or written by an older FlipAi takes.
func TestALoadedConfigNeverLeavesACallerOnTwoAgents(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.DefaultAgent = "A"
	cfg.Codex.Phones = []AgentPhone{{Number: "8455551000", Access: AccessAll}}
	cfg.Claude.Phones = []AgentPhone{{Number: "8455551000", Access: AccessAll}}
	cfg.Codex.CallerNames = "Dana Codex"
	cfg.Claude.CallerNames = "Dana Codex"

	salvageAgents(&cfg)

	if got := len(cfg.Claude.Phones); got != 1 || cfg.Claude.Phones[0].Number != "8455551000" {
		t.Errorf("the valid shared number was not preserved on the second agent: %+v", cfg.Claude.Phones)
	}
	if cfg.Claude.CallerNames != "" {
		t.Errorf("the duplicate caller name stayed on the second agent: %q", cfg.Claude.CallerNames)
	}
	vc := defaultVoiceCallConfig()
	vc.Enabled = true
	if d := decideVoiceCall(vc, cfg, "", "Dana Codex"); !d.Allowed || d.Agent != "C" {
		t.Errorf("the repaired caller did not reach the agent that claimed it first: %+v", d)
	}
}
