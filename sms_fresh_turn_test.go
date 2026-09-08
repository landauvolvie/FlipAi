package main

import "testing"

func TestEveryPublicSMSRouteSupportsInlineNew(t *testing.T) {
	cfg := stickyRoutingConfig(t)
	owner, _, ok := agentForSender(cfg, "18455550123")
	if !ok {
		t.Fatal("shared sender was not allowed")
	}

	cases := []struct {
		raw      string
		agent    string
		mode     string
		wantText string
	}{
		{"O new: fresh chat", "G", browserModeChat, "fresh chat"},
		{"OW NEW: fresh work", "G", browserModeWork, "fresh work"},
		{"OC New: fresh codex", "C", "", "fresh codex"},
		{"A new: fresh claude chat", "H", browserModeChat, "fresh claude chat"},
		{"AW new: fresh cowork", "H", browserModeCowork, "fresh cowork"},
		{"AC new: fresh code web", "H", browserModeCode, "fresh code web"},
		{"AL new: fresh local claude", "A", "", "fresh local claude"},
		{"G new: fresh gemini", "M", "", "fresh gemini"},
		{"M new: fresh copilot", "P", "", "fresh copilot"},
		{"X new: fresh grok", "X", "", "fresh grok"},
	}

	for _, tc := range cases {
		rc, err := parseRemoteCommandForMessageSticky(tc.raw, cfg, owner, "", GmailMessage{})
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if !rc.New {
			t.Fatalf("%s did not request a fresh session: %+v", tc.raw, rc)
		}
		if rc.Agent != tc.agent {
			t.Fatalf("%s agent=%q want %q", tc.raw, rc.Agent, tc.agent)
		}
		mode, text := extractBrowserModeCommand(rc.Text)
		if mode != tc.mode || text != tc.wantText {
			t.Fatalf("%s mode/text=%q/%q want %q/%q", tc.raw, mode, text, tc.mode, tc.wantText)
		}
		if !remoteCommandHasTurn(rc) {
			t.Fatalf("%s should be a fresh turn, not reset-only", tc.raw)
		}
	}
}

func TestPublicSMSRouteSupportsResetOnlyNewColon(t *testing.T) {
	cfg := stickyRoutingConfig(t)
	owner, _, _ := agentForSender(cfg, "18455550123")

	for _, tc := range []struct {
		raw   string
		agent string
		mode  string
	}{
		{"O new:", "G", browserModeChat},
		{"OW new:", "G", browserModeWork},
		{"A new:", "H", browserModeChat},
		{"AW new:", "H", browserModeCowork},
		{"AC new:", "H", browserModeCode},
		{"OC new:", "C", ""},
		{"AL new:", "A", ""},
		{"G new:", "M", ""},
		{"M new:", "P", ""},
		{"X new:", "X", ""},
	} {
		rc, err := parseRemoteCommandForMessageSticky(tc.raw, cfg, owner, "", GmailMessage{})
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		mode, text := extractBrowserModeCommand(rc.Text)
		if !rc.New || rc.Agent != tc.agent || mode != tc.mode || text != "" || remoteCommandHasTurn(rc) {
			t.Fatalf("%s reset parsed incorrectly: rc=%+v mode=%q text=%q", tc.raw, rc, mode, text)
		}
	}
}

func TestPublicSMSRouteUsesConfiguredNewWord(t *testing.T) {
	cfg := stickyRoutingConfig(t)
	cfg.NewSessionCommand = "FRESH"
	owner, _, _ := agentForSender(cfg, "18455550123")

	rc, err := parseRemoteCommandForMessageSticky("OW fresh: start here", cfg, owner, "", GmailMessage{})
	if err != nil {
		t.Fatal(err)
	}
	mode, text := extractBrowserModeCommand(rc.Text)
	if !rc.New || mode != browserModeWork || text != "start here" {
		t.Fatalf("configured fresh word parsed incorrectly: rc=%+v mode=%q text=%q", rc, mode, text)
	}
}

func TestRemoteCommandDisplayNameKeepsBrowserMode(t *testing.T) {
	cases := []struct {
		rc   remoteCommand
		want string
	}{
		{remoteCommand{Agent: "G", Text: markBrowserModeCommand("weather", browserModeChat)}, "ChatGPT Chat"},
		{remoteCommand{Agent: "G", Text: markBrowserModeCommand("weather", browserModeWork)}, "ChatGPT Work"},
		{remoteCommand{Agent: "H", Text: markBrowserModeCommand("task", browserModeChat)}, "Claude Chat"},
		{remoteCommand{Agent: "H", Text: markBrowserModeCommand("task", browserModeCode)}, "Claude Code Web"},
		{remoteCommand{Agent: "H", Text: markBrowserModeCommand("task", browserModeCowork)}, "Claude Cowork"},
	}
	for _, tc := range cases {
		if got := remoteCommandDisplayName(tc.rc); got != tc.want {
			t.Fatalf("remoteCommandDisplayName(%+v)=%q want %q", tc.rc, got, tc.want)
		}
	}
}

func TestChatGPTWorkFreshPlanReusesONewThenOW(t *testing.T) {
	if got := chatGPTFreshResetMode(browserModeWork); got != browserModeChat {
		t.Fatalf("OW NEW reset mode=%q want regular Chat so it reuses O NEW", got)
	}
	if got := chatGPTFreshResetMode(browserModeChat); got != browserModeChat {
		t.Fatalf("O NEW reset mode=%q want Chat", got)
	}

	rc := remoteCommand{Agent: "G", New: true, Text: markBrowserModeCommand("what's the weather?", browserModeWork)}
	mode, text := extractBrowserModeCommand(rc.Text)
	if mode != browserModeWork || text != "what's the weather?" {
		t.Fatalf("OW NEW must preserve Work on the actual turn: mode/text=%q/%q", mode, text)
	}
	if chatGPTFreshResetMode(mode) != browserModeChat {
		t.Fatal("OW NEW must reset through the same regular-Chat boundary as O NEW")
	}
}
