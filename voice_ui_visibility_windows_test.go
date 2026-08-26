//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestVoiceUIBootstrapsBeforeDocumentElementExists(t *testing.T) {
	if !strings.Contains(baseDesktopInitScript, "globalThis.__flipaiDesktop = true") {
		t.Fatal("desktop startup must mark the FlipAi WebView without requiring documentElement")
	}
	if !strings.Contains(baseDesktopInitScript, "if (document.documentElement)") {
		t.Fatal("documentElement access must be guarded at document-created time")
	}
}

func TestVoiceUIShowsServiceFailureInsteadOfDisappearing(t *testing.T) {
	if !strings.Contains(voiceDesktopInitScript, "serviceErrorCard") {
		t.Fatal("voice UI must render a visible local-service failure state")
	}
	if !strings.Contains(voiceDesktopInitScript, "catch(e){serviceErrorCard(e);return}") {
		t.Fatal("voice status failure must not silently hide the calling feature")
	}
	if !strings.Contains(voiceVisibilityFallbackScript, "Google Voice calling is installed") {
		t.Fatal("desktop UI needs a fallback visibility warning")
	}
}
