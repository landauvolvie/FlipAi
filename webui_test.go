package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestModernSetupPageRendersCoreFlow(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaultConfig(tmp)
	a := &App{dataDir: tmp, configPath: tmp + "/bridge.json", statePath: tmp + "/state.json", tokenPath: tmp + "/token.dat", cfg: cfg}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/", nil)
	req.AddCookie(&http.Cookie{Name: "aisms_session", Value: cfg.LocalToken})
	rr := httptest.NewRecorder()
	a.page(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("page status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"FlipAi", "Connect Gmail", "Phone security", "Choose your agents",
		"Start with Windows", "Quit FlipAi completely",
		"Background bridge stays running when this page closes",
		"App Password", "Your Google API project",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("modern page missing %q", want)
		}
	}
	if strings.Contains(body, "http://fonts.googleapis.com") || strings.Contains(body, "https://cdn") {
		t.Fatal("UI must not depend on external fonts/CDNs")
	}
}

func TestSetupValidationUsesFriendlyResultPage(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaultConfig(tmp)
	a := &App{dataDir: tmp, configPath: tmp + "/bridge.json", statePath: tmp + "/state.json", tokenPath: tmp + "/token.dat", cfg: cfg}
	form := url.Values{}
	form.Set("gmailMethod", "")
	form.Set("allowedFrom", "8455551212")
	form.Set("defaultAgent", "C")
	form.Set("replyMaxChars", "300")
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/setup/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	a.saveSetup(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Set an SMS security code") || !strings.Contains(body, "Back to FlipAi") {
		t.Fatalf("expected styled validation result, got %s", body)
	}
}

func TestReplyLengthValidation(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaultConfig(tmp)
	if err := setSecurityCode(&cfg, "482913"); err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: tmp, configPath: tmp + "/bridge.json", statePath: tmp + "/state.json", tokenPath: tmp + "/token.dat", cfg: cfg}
	form := url.Values{}
	form.Set("allowedFrom", "8455551212")
	form.Set("defaultAgent", "C")
	form.Set("replyMaxChars", "50000")
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/setup/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	a.saveSetup(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Reply length is invalid") {
		t.Fatalf("unexpected reply length result: %d %s", rr.Code, rr.Body.String())
	}
}
