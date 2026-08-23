package main

import "testing"

func testConfigWithCode(t *testing.T) Config {
	t.Helper()
	cfg := defaultConfig(t.TempDir())
	if err := setSecurityCode(&cfg, "482913"); err != nil {
		t.Fatal(err)
	}
	cfg.GoogleVoice.AllowedFrom = "8455551212"
	return cfg
}
func TestParseGVAndRouting(t *testing.T) {
	cfg := testConfigWithCode(t)
	m := GmailMessage{From: "Google Voice <voice-noreply@google.com>", AuthenticationResults: "mx.google.com; dkim=pass header.d=google.com", Subject: "New text message from (845) 555-1212", Body: "Google Voice\n482913 C: Check my GitHub and fix the failing build."}
	raw, sender, ok := parseGoogleVoiceBody(m, cfg.GoogleVoice.AllowedFrom, "new text message from")
	if !ok || sender != "8455551212" {
		t.Fatalf("not parsed correctly: sender=%q ok=%v", sender, ok)
	}
	rc, err := parseRemoteCommand(raw, cfg)
	if err != nil || rc.Agent != "C" || rc.Text != "Check my GitHub and fix the failing build." {
		t.Fatalf("%+v %v", rc, err)
	}
}
func TestClaudeRouting(t *testing.T) {
	cfg := testConfigWithCode(t)
	rc, err := parseRemoteCommand("482913 A: check Gmail", cfg)
	if err != nil || rc.Agent != "A" || rc.Text != "check Gmail" {
		t.Fatalf("%+v %v", rc, err)
	}
}
func TestRejectWrongCode(t *testing.T) {
	cfg := testConfigWithCode(t)
	if _, err := parseRemoteCommand("111111 C: do it", cfg); err == nil {
		t.Fatal("wrong code accepted")
	}
}
func TestRejectSpoofOrNoDKIM(t *testing.T) {
	cfg := testConfigWithCode(t)
	m := GmailMessage{From: "Google Voice <voice-noreply@google.com>", Subject: "New text message from 8455551212", Body: "482913 C: do it"}
	if _, _, ok := parseGoogleVoiceBody(m, cfg.GoogleVoice.AllowedFrom, "new text message from"); ok {
		t.Fatal("message without Google DKIM accepted")
	}
}
func TestSafeReplyAddress(t *testing.T) {
	if _, err := safeGoogleVoiceReplyAddress("abc@txt.voice.google.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := safeGoogleVoiceReplyAddress("abc@example.com"); err == nil {
		t.Fatal("unsafe domain accepted")
	}
}
