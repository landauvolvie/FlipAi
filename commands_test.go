package main

import "testing"

func TestParseRemoteCommandConfigurablePrefixes(t *testing.T) {
	cfg := Config{DefaultAgent: "A", CodexPrefix: "1", ClaudePrefix: "Z9", NewSessionCommand: "FRESH"}
	cases := []struct {
		raw    string
		agent  string
		text   string
		newRun bool
	}{
		{"1: inspect logs", "C", "inspect logs", false},
		{"z9: review issue", "A", "review issue", false},
		{"1 FRESH", "C", "", true},
		{"Z9: fresh", "A", "", true},
		{"fresh", "A", "", true},
		{"no prefix here", "A", "no prefix here", false},
	}
	for _, tc := range cases {
		// The sending number decides the agent now, so each case is parsed as
		// though it arrived from a number allowed on that agent.
		rc, err := parseRemoteCommand(tc.raw, cfg, tc.agent)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		if rc.Agent != tc.agent || rc.Text != tc.text || rc.New != tc.newRun {
			t.Fatalf("parse %q = %+v, want agent=%q text=%q new=%v", tc.raw, rc, tc.agent, tc.text, tc.newRun)
		}
	}
}

func TestPrefixCannotReachAnAgentTheNumberIsNotAllowedOn(t *testing.T) {
	// A number belongs to one agent. A prefix naming the other one has to be
	// refused rather than quietly routed, or the allowlist would mean nothing.
	cfg := Config{DefaultAgent: "C"}
	if _, err := parseRemoteCommand("A: hello", cfg, "C"); err == nil {
		t.Fatal("a Codex number addressed Claude and was accepted")
	}
	if _, err := parseRemoteCommand("C NEW", cfg, "A"); err == nil {
		t.Fatal("a Claude number addressed Codex and was accepted")
	}
	rc, err := parseRemoteCommand("A: hello", cfg, "A")
	if err != nil || rc.Agent != "A" || rc.Text != "hello" {
		t.Fatalf("matching prefix: rc=%+v err=%v", rc, err)
	}
	rc, err = parseRemoteCommand("C NEW", cfg, "C")
	if err != nil || rc.Agent != "C" || !rc.New {
		t.Fatalf("matching new-session: rc=%+v err=%v", rc, err)
	}
}

func TestValidateCommandToken(t *testing.T) {
	if got, err := validateCommandToken(" 7: ", "prefix"); err != nil || got != "7" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := validateCommandToken("two words", "prefix"); err == nil {
		t.Fatal("expected spaces to be rejected")
	}
}
