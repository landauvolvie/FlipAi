package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClaudeChatActionRoutesAreRegistered(t *testing.T) {
	const token = "claude-chat-route-test-token"
	app := &App{
		dataDir: t.TempDir(),
		cfg:     Config{LocalToken: token},
	}
	handler := app.handler()

	for _, path := range []string{
		"/claude-chat/connect",
		"/claude-chat/test",
		"/claude-chat/disconnect",
	} {
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
