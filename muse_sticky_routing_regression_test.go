package main

import (
	"path/filepath"
	"testing"
)

func TestMuseStickyFollowUpStaysOnMuse(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	statePath := filepath.Join(t.TempDir(), "state.json")
	b := NewBridge(cfg, statePath, State{}, nil, nil, nil)
	const sender = "+18455550147"

	first, err := parseRemoteCommandForMessageSticky("MU: hi", cfg, "B", "", GmailMessage{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Agent != "U" || first.Text != "hi" {
		t.Fatalf("explicit MU route got agent=%q text=%q; want U/hi", first.Agent, first.Text)
	}
	if err := b.rememberStickySMSRoute(sender, first); err != nil {
		t.Fatal(err)
	}
	sticky := b.stickySMSAgent(sender)
	if sticky != "route:MU" {
		t.Fatalf("stored Muse sticky route = %q; want route:MU", sticky)
	}

	follow, err := parseRemoteCommandForMessageSticky("Check my last email", cfg, "B", sticky, GmailMessage{})
	if err != nil {
		t.Fatal(err)
	}
	if follow.Agent != "U" {
		t.Fatalf("unprefixed follow-up switched away from Muse: got agent=%q; want U", follow.Agent)
	}
	if follow.Text != "Check my last email" {
		t.Fatalf("Muse follow-up text = %q; want original text preserved", follow.Text)
	}
}

func TestLegacyMuseStickyAgentIsAccepted(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	statePath := filepath.Join(t.TempDir(), "state.json")
	b := NewBridge(cfg, statePath, State{}, nil, nil, nil)
	const sender = "+18455550147"

	if err := b.rememberStickySMSAgent(sender, "U"); err != nil {
		t.Fatalf("legacy Muse sticky agent was rejected: %v", err)
	}
	if got := b.stickySMSAgent(sender); got != "U" {
		t.Fatalf("legacy Muse sticky value = %q; want U", got)
	}
}

func TestSharedNumberLegacyTargetAllowsMuse(t *testing.T) {
	if !smsTargetAllowed("B", "U") {
		t.Fatal("shared SMS source B should allow Muse target U")
	}
}
