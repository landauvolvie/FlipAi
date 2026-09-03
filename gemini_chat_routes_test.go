package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeminiChatActionRoutesAreRegistered(t *testing.T) {
	const token = "gemini-chat-route-test-token"
	app := &App{dataDir: t.TempDir(), cfg: Config{LocalToken: token}}
	handler := app.handler()
	for _, path := range []string{"/gemini-chat/connect", "/gemini-chat/test", "/gemini-chat/disconnect"} {
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

func TestGeminiChatSMSPrefix(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	got, err := parseGeminiChatSMSCommand("M: hello Gemini", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "M" || got.Text != "hello Gemini" {
		t.Fatalf("got agent=%q text=%q", got.Agent, got.Text)
	}
	if len(cfg.GeminiChat.Phones) != 0 || cfg.GeminiChat.RequireCode {
		t.Fatal("new Gemini Chat security boundary must not inherit phones or a required PIN")
	}
}
