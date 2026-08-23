package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

func multipartRequest(t *testing.T, values map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range values {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/setup/save", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestSetupValidationUsesFriendlyResultPage(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaultConfig(tmp)
	a := &App{dataDir: tmp, configPath: tmp + "/bridge.json", statePath: tmp + "/state.json", tokenPath: tmp + "/token.dat", cfg: cfg}
	req := multipartRequest(t, map[string]string{
		"gmailMethod": "", "allowedFrom": "8455551212", "defaultAgent": "C", "replyMaxChars": "300",
	})
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
	req := multipartRequest(t, map[string]string{
		"allowedFrom": "8455551212", "defaultAgent": "C", "replyMaxChars": "50000",
	})
	rr := httptest.NewRecorder()
	a.saveSetup(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Reply length is invalid") {
		t.Fatalf("unexpected reply length result: %d %s", rr.Code, rr.Body.String())
	}
}
