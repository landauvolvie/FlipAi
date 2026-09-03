package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGrokChatActionRoutesAreRegistered(t *testing.T) {
	const token = "grok-chat-route-test-token"
	app := &App{dataDir: t.TempDir(), cfg: Config{LocalToken: token}}
	handler := app.handler()
	for _, path := range []string{"/grok-chat/connect", "/grok-chat/test", "/grok-chat/disconnect"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{Name: "aisms_session", Value: token})
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s status = %d; want %d so the registered action reaches requirePost instead of 404", path, rr.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestGrokChatSMSPrefix(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	got, err := parseGrokChatSMSCommand("X: hello Grok", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "X" || got.Text != "hello Grok" {
		t.Fatalf("got agent=%q text=%q", got.Agent, got.Text)
	}
	if len(cfg.GrokChat.Phones) != 0 || cfg.GrokChat.RequireCode {
		t.Fatal("new Grok Chat security boundary must not inherit phones or a required PIN")
	}
}
