package main

import (
	"os"
	"strings"
	"testing"
)

func TestPublishedStartBridgeUsesOnlyDirectGoogleVoiceTransport(t *testing.T) {
	raw, err := os.ReadFile("webui.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, "func (a *App) startBridge(ctx context.Context)")
	if start < 0 {
		t.Fatal("startBridge not found")
	}
	body := src[start:]

	for _, want := range []string{
		"cfg.Gmail.Method != GmailMethodGoogleVoice",
		"Google Voice SMS background connection is not ready",
		"Google Voice SMS connection test failed",
		"Google Voice SMS monitoring active through the signed-in background browser",
		"go b.Run(ctx)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("published Google Voice startup is missing %q", want)
		}
	}

	for _, retired := range []string{
		"Gmail monitoring active",
		"Gmail connection method not selected",
		"Gmail connection test failed",
		"Gmail not configured",
	} {
		if strings.Contains(body, retired) {
			t.Fatalf("published startup still contains retired Gmail log text %q", retired)
		}
	}
}

func TestPublishedHandlerDoesNotExposeRetiredGmailOrBootStartupRoutes(t *testing.T) {
	raw, err := os.ReadFile("webui.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, "func (a *App) handler() http.Handler")
	end := strings.Index(src[start:], "func pageMovedTo")
	if start < 0 || end < 0 {
		t.Fatal("handler bounds not found")
	}
	body := src[start : start+end]
	for _, retired := range []string{
		`"/gmail/test"`,
		`"/oauth/google/start"`,
		`"/oauth/google/callback"`,
		`"/settings/bootstartup"`,
	} {
		if strings.Contains(body, retired) {
			t.Fatalf("published handler still exposes retired route %s", retired)
		}
	}
}
