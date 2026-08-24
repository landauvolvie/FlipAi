package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAgentsPageOwnsAgentSpecificSettings(t *testing.T) {
	a := newTestApp(t)
	if err := saveState(a.statePath, State{
		CodexThreadID:     "codex-thread",
		ClaudeSessionID:   "claude-session",
		ClaudeSessionName: "FlipAi-SMS-123",
	}); err != nil {
		t.Fatal(err)
	}
	body := a.do(t, http.MethodGet, "/agents", nil).Body.String()

	for _, want := range []string{
		"agent-rail",
		"Shortcuts &amp; session",
		"Workspace &amp; paths",
		"Access &amp; tools",
		"Authentication &amp; session",
		`name="codexPrefix"`,
		`name="claudePrefix"`,
		`name="newSessionCommand"`,
		`name="permissionMode"`,
		`formaction="/agents/reset"`,
		"Shared defaults",
		"Save changes",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Agents page is missing %q", want)
		}
	}

	// The mockup showed a plus button, but FlipAi has no generic agent registry.
	// Do not ship a control that pretends arbitrary agent types can be added.
	if strings.Contains(body, `agent-add`) || strings.Contains(body, `aria-label="Add agent"`) {
		t.Fatal("Agents page exposes an Add agent control without backend support")
	}

	advanced := a.do(t, http.MethodGet, "/advanced", nil).Body.String()
	for _, forbidden := range []string{`name="codexPath"`, `name="claudePath"`, `name="permissionMode"`, `name="codexPrefix"`, `name="claudePrefix"`} {
		if strings.Contains(advanced, forbidden) {
			t.Errorf("Advanced still contains agent-specific control %q", forbidden)
		}
	}
	if !strings.Contains(advanced, "Agent settings moved") || !strings.Contains(advanced, "Open Agents") {
		t.Fatal("Advanced should point users to the reorganized Agents page")
	}
}

func TestAgentConversationResetClearsOnlySelectedAgent(t *testing.T) {
	a := newTestApp(t)
	state := State{
		CodexThreadID:     "codex-thread",
		ClaudeSessionID:   "claude-session",
		ClaudeSessionName: "FlipAi-SMS-123",
	}
	if err := saveState(a.statePath, state); err != nil {
		t.Fatal(err)
	}

	rr := a.do(t, http.MethodPost, "/agents/reset", url.Values{"agent": {"C"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("Codex reset returned %d: %s", rr.Code, rr.Body.String())
	}
	got := loadState(a.statePath)
	if got.CodexThreadID != "" {
		t.Fatalf("Codex thread was not cleared: %q", got.CodexThreadID)
	}
	if got.ClaudeSessionID != "claude-session" || got.ClaudeSessionName != "FlipAi-SMS-123" {
		t.Fatalf("Codex reset changed Claude state: %+v", got)
	}

	got.CodexThreadID = "codex-thread-2"
	if err := saveState(a.statePath, got); err != nil {
		t.Fatal(err)
	}
	rr = a.do(t, http.MethodPost, "/agents/reset", url.Values{"agent": {"A"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("Claude reset returned %d: %s", rr.Code, rr.Body.String())
	}
	got = loadState(a.statePath)
	if got.ClaudeSessionID != "" || got.ClaudeSessionName != "" {
		t.Fatalf("Claude session was not fully cleared: %+v", got)
	}
	if got.CodexThreadID != "codex-thread-2" {
		t.Fatalf("Claude reset changed Codex state: %+v", got)
	}
}

func TestAgentConversationResetRejectsUnknownAgent(t *testing.T) {
	a := newTestApp(t)
	rr := a.do(t, http.MethodPost, "/agents/reset", url.Values{"agent": {"X"}})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Unknown agent") {
		t.Fatalf("unknown reset returned %d: %s", rr.Code, rr.Body.String())
	}
}
