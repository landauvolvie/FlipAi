package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChatGPTConversationIDFromURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"https://chatgpt.com/c/1234-abcd", "1234-abcd"},
		{"https://chatgpt.com/c/abc?model=x", "abc"},
		{"https://chatgpt.com/", ""},
		{"not a url", ""},
	} {
		if got := chatGPTConversationIDFromURL(tc.in); got != tc.want {
			t.Errorf("chatGPTConversationIDFromURL(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestChatGPTWebClientUsesAuthenticatedLocalControlWithoutLoggingContent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	const token = "local-control-token"
	const promptSecret = "PROMPT-MUST-NOT-ENTER-ACTIVITY-8d5d"
	const replySecret = "REPLY-MUST-NOT-ENTER-ACTIVITY-c117"

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-FlipAi-Token") != token {
			t.Errorf("missing local control token")
			http.Error(w, "forbidden", 403)
			return
		}
		writeJSON(w, chatGPTWebStatus{Running: true, SignedIn: true, ComposerReady: true, CurrentURL: "https://chatgpt.com/"})
	})
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-FlipAi-Token") != token {
			http.Error(w, "forbidden", 403)
			return
		}
		var req chatGPTWebTurnRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.Text != promptSecret {
			t.Errorf("prompt=%q", req.Text)
		}
		writeJSON(w, chatGPTWebTurnResult{Reply: replySecret, ConversationID: "conv-123", Capture: "network", DurationMS: 42})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	if err := saveChatGPTWebRuntime(dir, chatGPTWebRuntime{Running: true, Port: port, ControlToken: token}); err != nil {
		t.Fatal(err)
	}
	client := newChatGPTWebClient(dir, statePath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := client.Chat(ctx, promptSecret, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reply != replySecret || got.ConversationID != "conv-123" || got.Capture != "network" {
		t.Fatalf("unexpected result: %+v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "activity.jsonl"))
	if strings.Contains(string(raw), promptSecret) || strings.Contains(string(raw), replySecret) {
		t.Fatalf("Activity leaked prompt/reply content: %s", raw)
	}
	if !strings.Contains(string(raw), "background WebView") || !strings.Contains(string(raw), "captured via network") {
		t.Fatalf("Activity omitted useful ChatGPT stage metadata: %s", raw)
	}
}

func TestChatGPTWebInitScriptHasNetworkAndDOMCaptureWithoutCredentialReads(t *testing.T) {
	for _, want := range []string{"res.clone()", "parseConversationBody", "data-message-author-role", "__flipAiChatGPTSubmit", "requestSubmit", "flipChatGPTReply"} {
		if !strings.Contains(chatGPTWebInitScript, want) {
			t.Errorf("ChatGPT page script missing %q", want)
		}
	}
	for _, forbidden := range []string{"document.cookie", "localStorage", "indexedDB", "sessionStorage", "getAllCookies", "SendKeys", "UIAutomation"} {
		if strings.Contains(chatGPTWebInitScript, forbidden) {
			t.Errorf("ChatGPT page script must not contain %q", forbidden)
		}
	}
}

func TestNewChatGPTConversationIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	b := &Bridge{statePath: filepath.Join(dir, "state.json")}
	if err := b.newChatGPTWebConversation(); err != nil {
		t.Fatalf("reset with no prior conversation should succeed: %v", err)
	}
	path := chatGPTWebConversationPath(dir)
	if err := saveChatGPTWebConversation(path, chatGPTWebConversationState{ID: "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := b.newChatGPTWebConversation(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("conversation file still exists: %v", err)
	}
}
