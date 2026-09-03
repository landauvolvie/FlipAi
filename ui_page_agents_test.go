package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAgentsPageOwnsUnifiedAgentSettings(t *testing.T) {
	a := newTestApp(t)
	if err := saveState(a.statePath, State{
		CodexThreadID: "codex-thread", ClaudeSessionID: "claude-session", ClaudeSessionName: "FlipAi-SMS-123",
	}); err != nil {
		t.Fatal(err)
	}
	body := a.do(t, http.MethodGet, "/agents", nil).Body.String()

	for _, want := range []string{
		"agent-rail", "Routing &amp; workspace", "SMS instruction", "Conversation",
		`name="codexPrefix"`, `name="claudePrefix"`, `name="chatgptPrefix"`, `name="claudeChatPrefix"`, `name="geminiChatPrefix"`, `name="grokChatPrefix"`,
		`name="codexPath"`, `name="claudePath"`, `name="permissionMode"`,
		`name="sharedReplyStyle"`, "Allowed phone numbers", "Security code",
		`name="codexRequireCode"`, `name="claudeRequireCode"`, `name="chatgptRequireCode"`, `name="claudeChatRequireCode"`, `name="geminiChatRequireCode"`, `name="grokChatRequireCode"`,
		`name="codexAckDelay"`, `name="claudeAckDelay"`, `name="chatgptAckDelay"`, `name="claudeChatAckDelay"`, `name="geminiChatAckDelay"`, `name="grokChatAckDelay"`,
		`formaction="/agents/numbers/add"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Agents page is missing %q", want)
		}
	}
	for _, old := range []string{`name="codexReplyStyle"`, `name="claudeReplyStyle"`, `name="defaultAgent"`, "Make default"} {
		if strings.Contains(body, old) {
			t.Errorf("Agents page still exposes retired control %q", old)
		}
	}
	if strings.Count(body, `name="sharedReplyStyle"`) != 6 {
		t.Fatalf("shared SMS instruction should be available from all six panes")
	}

	agentFields := []string{`name="codexPath"`, `name="claudePath"`, `name="permissionMode"`, `name="codexPrefix"`, `name="claudePrefix"`, `name="chatgptPrefix"`, `name="claudeChatPrefix"`, `name="geminiChatPrefix"`, `name="grokChatPrefix"`, `name="sharedReplyStyle"`}
	for _, page := range []string{"/settings", "/connections", "/"} {
		other := a.do(t, http.MethodGet, page, nil).Body.String()
		for _, forbidden := range agentFields {
			if strings.Contains(other, forbidden) {
				t.Errorf("%s still contains agent control %q", page, forbidden)
			}
		}
	}

	if rr := a.do(t, http.MethodPost, "/agents/numbers/add", url.Values{
		"agent": {"G"}, "newNumber": {"845 555 0147"}, "newAccess": {"sms"},
	}); rr.Code != http.StatusSeeOther {
		t.Fatalf("adding ChatGPT number returned %d: %s", rr.Code, rr.Body.String())
	}
	withNumber := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	for _, want := range []string{`formaction="/agents/numbers/remove"`, "(845) 555-0147", `name="access-G-8455550147"`} {
		if !strings.Contains(withNumber, want) {
			t.Errorf("Agents page is missing %q once a ChatGPT number exists", want)
		}
	}
}

// FlipAi has one SMS instruction for all six agents. Editing it in any
// agent pane changes the one shared value used by every composePrompt call.
func TestSharedSMSInstructionIsEditableAndUsedByEveryAgent(t *testing.T) {
	a := newTestApp(t)

	rr := a.do(t, http.MethodPost, "/agents/save", url.Values{
		"sharedReplyStyle": {"  Answer in one line. No markdown.  "},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("saving instruction returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	if cfg.GoogleVoice.ReplyStyleHint != "Answer in one line. No markdown." {
		t.Fatalf("shared instruction was not stored trimmed: %q", cfg.GoogleVoice.ReplyStyleHint)
	}
	if cfg.Codex.Instruction != "" || cfg.Claude.Instruction != "" || cfg.ChatGPT.Instruction != "" || cfg.ClaudeChat.Instruction != "" || cfg.GeminiChat.Instruction != "" || cfg.GrokChat.Instruction != "" {
		t.Fatalf("retired per-agent overrides survived")
	}
	b := NewBridge(cfg, a.statePath, State{}, nil, nil, nil)
	for _, agent := range []string{"C", "A", "G", "H", "M", "X"} {
		if got := b.composePrompt(agent, "ship it"); !strings.HasSuffix(got, "Answer in one line. No markdown.") {
			t.Fatalf("%s prompt does not carry shared instruction: %q", agent, got)
		}
	}

	if rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"sharedReplyStyle": {"   "}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("clearing instruction returned %d", rr.Code)
	}
	if got := a.reloadConfig(t).GoogleVoice.ReplyStyleHint; got != defaultReplyStyleHint {
		t.Fatalf("cleared shared instruction should restore default, got %q", got)
	}

	long := strings.Repeat("x", replyStyleHintMaxChars+500)
	if rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"sharedReplyStyle": {long}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("saving long instruction returned %d", rr.Code)
	}
	if got := len(a.reloadConfig(t).GoogleVoice.ReplyStyleHint); got > replyStyleHintMaxChars {
		t.Fatalf("instruction stored at %d chars, cap is %d", got, replyStyleHintMaxChars)
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
