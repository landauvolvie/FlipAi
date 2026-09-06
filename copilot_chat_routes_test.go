package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCopilotChatActionRoutesAreRegistered(t *testing.T) {
	const token = "copilot-chat-route-test-token"
	app := &App{dataDir: t.TempDir(), cfg: Config{LocalToken: token}}
	handler := app.handler()
	for _, path := range []string{"/copilot-chat/connect", "/copilot-chat/test", "/copilot-chat/disconnect"} {
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

func TestCopilotChatSMSPrefixAndNewConversation(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	got, err := parseCopilotChatSMSCommand("P: hello Copilot", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "P" || got.Text != "hello Copilot" {
		t.Fatalf("got agent=%q text=%q", got.Agent, got.Text)
	}
	fresh, err := parseCopilotChatSMSCommand("P: NEW", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Agent != "P" || !fresh.New {
		t.Fatalf("new-chat routing got agent=%q new=%v", fresh.Agent, fresh.New)
	}
	if len(cfg.CopilotChat.Phones) != 0 || cfg.CopilotChat.RequireCode {
		t.Fatal("new Microsoft Copilot Chat security boundary must not inherit phones or a required PIN")
	}
}

func TestCopilotChatUsesSetupOnlyVisibleWindowAndBackgroundWebView(t *testing.T) {
	windowsRaw, err := os.ReadFile("copilot_chat_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	modeRaw, err := os.ReadFile("copilot_chat_webview_mode_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	windows := string(windowsRaw)
	mode := string(modeRaw)
	for _, want := range []string{
		`DataPath: copilotChatProfilePath(dataDir)`,
		`opts.WindowOptions.X = -30000`,
		`opts.WindowOptions.Y = -30000`,
		`opts.WindowOptions.NoActivate = true`,
		`copilotChatWebURL`,
		`.click()`,
		`textarea#userInput`,
		`--copilot-chat-login`,
		`--copilot-chat-worker`,
	} {
		if !strings.Contains(windows+mode, want) {
			t.Fatalf("Microsoft Copilot Chat background architecture is missing %q", want)
		}
	}
	if !strings.Contains(windows, `runCopilotChatWebView(dataDir string, visible bool)`) {
		t.Fatal("Copilot WebView does not keep visible setup mode separate from the background worker")
	}
	if !strings.Contains(mode, `mode == "--copilot-chat-login"`) || !strings.Contains(mode, `mode == "--copilot-chat-worker"`) {
		t.Fatal("Copilot login and background worker modes are not separate")
	}
	for _, forbidden := range []string{"api.openai.com", "graph.microsoft.com", "api.cognitive.microsoft.com", "SendInput", "SetCursorPos"} {
		if strings.Contains(windows, forbidden) {
			t.Fatalf("Copilot browser integration unexpectedly contains direct API/global-input path %q", forbidden)
		}
	}
}
