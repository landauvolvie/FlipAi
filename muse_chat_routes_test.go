package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMuseChatActionRoutesAreRegistered(t *testing.T) {
	const token = "muse-chat-route-test-token"
	app := &App{dataDir: t.TempDir(), cfg: Config{LocalToken: token}}
	handler := app.handler()
	for _, path := range []string{"/muse-chat/connect", "/muse-chat/test", "/muse-chat/disconnect"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{Name: "aisms_session", Value: token})
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s status = %d; want %d so the action is registered instead of 404", path, rr.Code, http.StatusMethodNotAllowed)
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/muse-chat/status.json", nil)
	req.AddCookie(&http.Cookie{Name: "aisms_session", Value: token})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Muse status route = %d; want 200", rr.Code)
	}
}

func TestMuseChatSMSPrefixAndNewConversation(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	got, err := parseMuseChatSMSCommand("U: hello Muse", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "U" || got.Text != "hello Muse" {
		t.Fatalf("got agent=%q text=%q", got.Agent, got.Text)
	}
	fresh, err := parseMuseChatSMSCommand("U: NEW", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Agent != "U" || !fresh.New {
		t.Fatalf("new-chat routing got agent=%q new=%v", fresh.Agent, fresh.New)
	}

	foundPublic := false
	for _, route := range smsRouteSpecs(cfg) {
		if route.Prefix == "MU" {
			foundPublic = route.Agent == "U" && route.Display == "Muse"
		}
	}
	if !foundPublic {
		t.Fatal("public MU: shortcut is not mapped to Muse")
	}
	if len(cfg.MuseChat.Phones) != 0 || cfg.MuseChat.RequireCode {
		t.Fatal("new Muse security boundary must not inherit phones or a required PIN")
	}
}

func TestMuseAgentsPaneAndCentralDispatchStayWired(t *testing.T) {
	body := smsRouteAgentsUI(museChatDirectUI(copilotChatDirectUI(exactWebAgentsHTML())))
	for _, want := range []string{"agent-muse-chat", "/muse-chat/connect", "/muse-chat/test", "/muse-chat/disconnect", "MU = Muse", "MuseChatAccess"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Agents page is missing %q", want)
		}
	}

	bridge, err := os.ReadFile("bridge.go")
	if err != nil {
		t.Fatal(err)
	}
	bridgeText := string(bridge)
	for _, want := range []string{`rc.Agent == "U"`, `b.runMuseChatSMS(ctx, rc.Text)`, `b.newMuseChatConversation(ctx)`} {
		// Go raw-string literals above intentionally keep the production spelling,
		// so normalize the one quoted agent check for readability in this test.
		want = strings.ReplaceAll(want, `\"`, `"`)
		if !strings.Contains(bridgeText, want) {
			t.Fatalf("central SMS executor is missing %q", want)
		}
	}
}

func TestMuseUsesIsolatedBackgroundWebViewAndDetectsAuth(t *testing.T) {
	windowsRaw, err := os.ReadFile("muse_chat_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	modeRaw, err := os.ReadFile("muse_chat_webview_mode_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	windows := string(windowsRaw)
	mode := string(modeRaw)
	for _, want := range []string{
		`DataPath: museChatProfilePath(dataDir)`,
		`opts.WindowOptions.X = -30000`,
		`opts.WindowOptions.Y = -30000`,
		`opts.WindowOptions.NoActivate = true`,
		`auth.muse.ai`,
		`museChatWebURL`,
		`--muse-chat-login`,
		`--muse-chat-worker`,
	} {
		if !strings.Contains(windows+mode, want) {
			t.Fatalf("Muse browser architecture is missing %q", want)
		}
	}
	for _, forbidden := range []string{"api.openai.com", "graph.microsoft.com", "SendInput", "SetCursorPos"} {
		if strings.Contains(windows, forbidden) {
			t.Fatalf("Muse integration unexpectedly contains direct API/global-input path %q", forbidden)
		}
	}
}
