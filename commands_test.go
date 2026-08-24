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
		rc, err := parseRemoteCommand(tc.raw, cfg)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		if rc.Agent != tc.agent || rc.Text != tc.text || rc.New != tc.newRun {
			t.Fatalf("parse %q = %+v, want agent=%q text=%q new=%v", tc.raw, rc, tc.agent, tc.text, tc.newRun)
		}
	}
}

func TestParseRemoteCommandLegacyDefaultsRemain(t *testing.T) {
	cfg := Config{DefaultAgent: "C"}
	rc, err := parseRemoteCommand("A: hello", cfg)
	if err != nil || rc.Agent != "A" || rc.Text != "hello" {
		t.Fatalf("legacy A prefix: rc=%+v err=%v", rc, err)
	}
	rc, err = parseRemoteCommand("C NEW", cfg)
	if err != nil || rc.Agent != "C" || !rc.New {
		t.Fatalf("legacy C NEW: rc=%+v err=%v", rc, err)
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
