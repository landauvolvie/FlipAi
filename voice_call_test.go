package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVoiceCallDefaultsAreOffAndRestrictedToGoogleVoice(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	if cfg.Enabled {
		t.Fatal("voice calling must be opt-in")
	}
	if cfg.GoogleVoiceURL != googleVoiceWebURL {
		t.Fatalf("Google Voice URL = %q", cfg.GoogleVoiceURL)
	}
	if !cfg.AutoAnswer {
		t.Fatal("once enabled, authorized calls should auto-answer by default")
	}
}

func TestVoiceCallConfigNeverBecomesGeneralBrowser(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	cfg.GoogleVoiceURL = "https://example.com/"
	got, err := normalizeVoiceCallConfig(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoogleVoiceURL != googleVoiceWebURL {
		t.Fatalf("normalized URL = %q", got.GoogleVoiceURL)
	}
}

func TestVoiceEnabledAgentRequiresCallerAllowlist(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	cfg.Codex.Enabled = true
	if _, err := normalizeVoiceCallConfig(cfg, true); err == nil {
		t.Fatal("enabled agent without allowed callers should be rejected")
	}
}

func TestVoiceCallerRoutesPerAgentAndDefault(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	cfg.Enabled = true
	cfg.Codex.Enabled = true
	cfg.Claude.Enabled = true
	cfg.Codex.AllowedCallers = "8455551000\n8455553000"
	cfg.Claude.AllowedCallers = "8455552000\n8455553000"

	if agent, ok := voiceAgentForCaller(cfg, "+1 (845) 555-1000"); !ok || agent != "C" {
		t.Fatalf("Codex-only caller routed to %q, %v", agent, ok)
	}
	if agent, ok := voiceAgentForCaller(cfg, "8455552000"); !ok || agent != "A" {
		t.Fatalf("Claude-only caller routed to %q, %v", agent, ok)
	}
	if agent, ok := voiceAgentForCaller(cfg, "8455553000"); !ok || agent != "C" {
		t.Fatalf("shared caller should follow C default, got %q, %v", agent, ok)
	}
	cfg.DefaultAgent = "A"
	if agent, ok := voiceAgentForCaller(cfg, "8455553000"); !ok || agent != "A" {
		t.Fatalf("shared caller should follow A default, got %q, %v", agent, ok)
	}
	if agent, ok := voiceAgentForCaller(cfg, "8455559999"); ok || agent != "" {
		t.Fatalf("unknown caller was authorized as %q", agent)
	}
	if agent, ok := voiceAgentForCaller(cfg, "Private Caller"); ok || agent != "" {
		t.Fatalf("unparseable caller was authorized as %q", agent)
	}
}

func TestVoiceControlOriginOnlyAcceptsFlipAiLoopback(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:8765", "http://localhost:8765"} {
		if !voiceOriginAllowed(origin, "127.0.0.1:8765") {
			t.Fatalf("expected %q to be allowed", origin)
		}
	}
	for _, origin := range []string{"https://voice.google.com", "http://127.0.0.1:9999", "http://evil.example:8765", ""} {
		if voiceOriginAllowed(origin, "127.0.0.1:8765") {
			t.Fatalf("expected %q to be rejected", origin)
		}
	}
}

func TestVoiceConfigIsIndependentFromSMSConfig(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "bridge.json")
	mainCfg := defaultConfig(dir)
	mainCfg.GoogleVoice.AllowedFrom = "8455551111"
	if err := saveConfig(mainPath, mainCfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}

	vc := defaultVoiceCallConfig()
	vc.Codex.Enabled = true
	vc.Codex.AllowedCallers = "8455552222"
	if err := saveVoiceCallConfig(dir, vc); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("saving voice-call settings modified the SMS bridge config")
	}
}
