package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChatGPTConversationID(t *testing.T) {
	cases := map[string]string{
		"https://chatgpt.com/c/abc-123": "abc-123",
		"https://chatgpt.com/c/abc-123?model=auto": "abc-123",
		"https://chatgpt.com/": "",
	}
	for in, want := range cases {
		if got := chatGPTConversationID(in); got != want {
			t.Fatalf("chatGPTConversationID(%q)=%q want %q", in, got, want)
		}
	}
}

func TestChatGPTRuntimeRoundTripAndStatusHidesToken(t *testing.T) {
	dir := t.TempDir()
	mutateChatGPTRuntime(dir, func(s *ChatGPTWebRuntime) {
		s.Running = true
		s.SignedIn = true
		s.ControlPort = 12345
		s.ControlToken = "private-token"
		s.ConversationID = "conversation-1"
	})
	got := loadChatGPTRuntime(dir)
	if !got.Running || !got.SignedIn || got.ControlToken != "private-token" || got.ConversationID != "conversation-1" {
		t.Fatalf("unexpected runtime state: %+v", got)
	}
	if got.UpdatedAt.IsZero() || time.Since(got.UpdatedAt) > time.Minute {
		t.Fatalf("runtime timestamp not written: %+v", got)
	}
}

func TestChatGPTActivityIsMetadataOnly(t *testing.T) {
	dir := t.TempDir()
	secretPrompt := "DO-NOT-LOG-PROMPT-123"
	secretReply := "DO-NOT-LOG-REPLY-456"
	chatGPTActivity(dir, "info", "chatgpt-turn", "ChatGPT completed a browser-session turn successfully.", 25*time.Millisecond)
	b, err := os.ReadFile(filepath.Join(dir, "activity.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, secretPrompt) || strings.Contains(s, secretReply) {
		t.Fatalf("activity leaked private content: %s", s)
	}
	if !strings.Contains(s, `"stage":"chatgpt-turn"`) || !strings.Contains(s, `"agent":"ChatGPT Chat"`) {
		t.Fatalf("activity lacks ChatGPT metadata: %s", s)
	}
}

func TestChatGPTJSStringEscapesPrompt(t *testing.T) {
	got := chatGPTJSString("hello </script> \"world\"\nnext")
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Fatalf("not a JSON string: %s", got)
	}
	if strings.Contains(got, "\nnext") == false {
		t.Fatalf("newline was not escaped: %s", got)
	}
}
