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
		"Routing &amp; workspace",
		"SMS instruction",
		"Access &amp; tools",
		"Conversation",
		"Connection",
		`name="codexPrefix"`,
		`name="claudePrefix"`,
		`name="codexPath"`,
		`name="claudePath"`,
		`name="permissionMode"`,
		`name="codexReplyStyle"`,
		`name="claudeReplyStyle"`,
		`formaction="/agents/reset"`,
		// Who may reach the agent, its code, and how it replies now live on the
		// agent rather than on a shared page.
		"Allowed phone numbers",
		"Security code",
		`formaction="/agents/numbers/add"`,
		`name="codexRequireCode"`,
		`name="claudeRequireCode"`,
		`name="codexAck"`,
		`name="claudeProgress"`,
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

	// Every agent-specific control belongs to exactly one pane.
	for field, want := range map[string]int{
		`name="codexPrefix"`:      1,
		`name="claudePrefix"`:     1,
		`name="codexPath"`:        1,
		`name="claudePath"`:       1,
		`name="permissionMode"`:   1,
		`name="codexReplyStyle"`:  1,
		`name="claudeReplyStyle"`: 1,
		`name="codexRequireCode"`: 1,
		`name="claudeCode"`:       1,
	} {
		if got := strings.Count(body, field); got != want {
			t.Errorf("Agents page renders %s %d times, want %d", field, got, want)
		}
	}

	// Agent-specific settings must not reappear anywhere else in the app.
	agentFields := []string{`name="codexPath"`, `name="claudePath"`, `name="permissionMode"`, `name="codexPrefix"`, `name="claudePrefix"`, `name="claudeReplyStyle"`, `name="codexReplyStyle"`}
	for _, page := range []string{"/settings", "/connections", "/"} {
		other := a.do(t, http.MethodGet, page, nil).Body.String()
		for _, forbidden := range agentFields {
			if strings.Contains(other, forbidden) {
				t.Errorf("%s still contains agent-specific control %q", page, forbidden)
			}
		}
	}

	// A number that exists can be removed from the agent that holds it.
	if rr := a.do(t, http.MethodPost, "/agents/numbers/add", url.Values{
		"agent": {"C"}, "newNumber": {"845 555 0147"}, "newAccess": {"all"},
	}); rr.Code != http.StatusSeeOther {
		t.Fatalf("adding a number returned %d: %s", rr.Code, rr.Body.String())
	}
	withNumber := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	for _, want := range []string{`formaction="/agents/numbers/remove"`, "(845) 555-0147", `name="access-C-8455550147"`} {
		if !strings.Contains(withNumber, want) {
			t.Errorf("Agents page is missing %q once a number exists", want)
		}
	}

	settings := a.do(t, http.MethodGet, "/settings", nil).Body.String()
	if !strings.Contains(settings, `href="/agents"`) {
		t.Fatal("Settings should point users to the Agents page for agent settings")
	}
}

// The SMS instruction is the one thing FlipAi puts in front of the agent on
// every text, so it must be editable per agent and must reach composePrompt.
func TestPerAgentSMSInstructionIsEditableAndUsed(t *testing.T) {
	a := newTestApp(t)

	rr := a.do(t, http.MethodPost, "/agents/save", url.Values{
		"codexReplyStyle":  {"  Answer in one line. No markdown.  "},
		"claudeReplyStyle": {"Keep it under 300 characters."},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("saving instructions returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	if cfg.Codex.Instruction != "Answer in one line. No markdown." {
		t.Fatalf("Codex instruction was not stored trimmed: %q", cfg.Codex.Instruction)
	}
	if cfg.Claude.Instruction != "Keep it under 300 characters." {
		t.Fatalf("Claude instruction was not stored: %q", cfg.Claude.Instruction)
	}

	b := NewBridge(cfg, a.statePath, State{}, nil, nil, nil)
	if got := b.composePrompt("C", "ship it"); !strings.HasSuffix(got, "Answer in one line. No markdown.") {
		t.Fatalf("Codex prompt does not carry its own instruction: %q", got)
	}
	if got := b.composePrompt("A", "ship it"); !strings.HasSuffix(got, "Keep it under 300 characters.") {
		t.Fatalf("Claude prompt does not carry its own instruction: %q", got)
	}

	// Clearing an agent's box restores the built-in wording, because every turn
	// needs some framing and a blank one silently stops telling the agent that
	// its answer becomes a text message.
	if rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"claudeReplyStyle": {"   "}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("clearing an instruction returned %d", rr.Code)
	}
	if got := a.reloadConfig(t).Claude.Instruction; got != "" {
		t.Fatalf("a cleared instruction must fall back to the built-in wording, got %q", got)
	}

	// An over-long instruction is capped rather than stored whole, so a pasted
	// document cannot ride along on every single text.
	long := strings.Repeat("x", replyStyleHintMaxChars+500)
	if rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"claudeReplyStyle": {long}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("saving a long instruction returned %d", rr.Code)
	}
	if got := len(a.reloadConfig(t).Claude.Instruction); got > replyStyleHintMaxChars {
		t.Fatalf("instruction was stored at %d chars, cap is %d", got, replyStyleHintMaxChars)
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
