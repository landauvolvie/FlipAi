package main

import (
	"os"
	"strings"
	"testing"
)

func TestChatGPTV04612SignedInStateMigratesToDurableConnection(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"running":false,"visible":false,"signedIn":true,"conversationId":"old-chat"}`
	if err := os.WriteFile(chatGPTRuntimePath(dir), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s := loadChatGPTRuntime(dir)
	if !s.Connected {
		t.Fatalf("v0.46.12 signed-in profile was not migrated to durable Connected state: %+v", s)
	}
	if s.ConversationID != "old-chat" {
		t.Fatalf("migration lost conversation id: %+v", s)
	}
}

func TestChatGPTTemporaryPageSignOutDoesNotForgetSavedConnection(t *testing.T) {
	dir := t.TempDir()
	mutateChatGPTRuntime(dir, func(s *ChatGPTWebRuntime) {
		s.Connected = true
		s.SignedIn = true
	})
	mutateChatGPTRuntime(dir, func(s *ChatGPTWebRuntime) {
		s.SignedIn = false
		s.LastEvent = "session-restoring"
	})
	s := loadChatGPTRuntime(dir)
	if !s.Connected {
		t.Fatalf("loading/restart transition forgot the saved ChatGPT connection: %+v", s)
	}
	if s.SignedIn {
		t.Fatalf("live SignedIn should still describe the current page, got %+v", s)
	}
}

func TestChatGPTAgentsPaneExplainsOneTimePersistentConnection(t *testing.T) {
	body := chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))
	for _, want := range []string{
		"Connect only once",
		"after FlipAi restarts",
		"after Windows restarts",
		"Saved connection",
		"Live sign-in",
		"Starting in background",
		"Restoring",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("persistent ChatGPT UI missing %q", want)
		}
	}
}
