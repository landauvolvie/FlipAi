package main

import (
	"strings"
	"testing"
)

// This test intentionally inspects the final page registered after every init
// function has run. Muse v0.46.70 had correct helper markup, but a later UI
// registration rebuilt the Agents page without museChatDirectUI and silently
// removed Muse from the shipped screen.
func TestFinalRegisteredAgentsPageContainsMuse(t *testing.T) {
	tpl, ok := uiPages["agents"]
	if !ok || tpl == nil {
		t.Fatal("final Agents page is not registered")
	}
	content := tpl.Lookup("content")
	if content == nil || content.Tree == nil || content.Tree.Root == nil {
		t.Fatal("final Agents content template is unavailable")
	}
	out := content.Tree.Root.String()
	for _, want := range []string{
		`agent-muse-chat`,
		`muse-chat-pane`,
		`/muse-chat/connect`,
		`/muse-chat/test`,
		`/muse-chat/disconnect`,
		`MU = Muse`,
		`Answers MU: messages`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("final registered Agents page lost Muse marker %q", want)
		}
	}
}
