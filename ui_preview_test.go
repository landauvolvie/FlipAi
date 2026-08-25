package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDumpPreview writes every desktop page, plus the stylesheet and script, to
// a directory so the UI can be opened in a browser while it is being worked on
// — including on a machine that cannot run the Windows app. It is skipped
// unless FLIPAI_PREVIEW_DIR is set, so a normal `go test ./...` writes nothing.
func TestDumpPreview(t *testing.T) {
	dir := os.Getenv("FLIPAI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set FLIPAI_PREVIEW_DIR to dump the UI preview")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t)
	// Enough state that the conversation and session cards render with real
	// content rather than their empty variants.
	if err := saveState(a.statePath, State{
		CodexThreadID:     "thr_01JQ8ZC2K9",
		ClaudeSessionID:   "0f8b1d64-4c31-4b0e-9a77-2b6d0e5a91cc",
		ClaudeSessionName: "FlipAi-SMS-7f31c2",
	}); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("flipai.css", uiCSS)
	write("flipai.js", uiJS)

	for path, file := range map[string]string{
		"/":            "home.html",
		"/connections": "connections.html",
		"/agents":      "agents.html",
		"/phone":       "phone.html",
		"/activity":    "activity.html",
		"/settings":    "settings.html",
		"/advanced":    "advanced.html",
	} {
		rr := a.do(t, http.MethodGet, path, nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, rr.Code)
		}
		body := rr.Body.String()
		body = strings.ReplaceAll(body, "/assets/flipai.css?v="+version, "flipai.css")
		body = strings.ReplaceAll(body, "/assets/flipai.js?v="+version, "flipai.js")
		write(file, body)
	}
}
