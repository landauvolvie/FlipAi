package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pausedNoticeTestConfig(t *testing.T, dir string, paused bool) {
	t.Helper()
	cfg := defaultConfig(dir)
	cfg.Gmail.Method = GmailMethodGoogleVoice
	cfg.Paused = paused
	allowTestNumber(&cfg, "C", "8455551234")
	if err := saveConfig(filepath.Join(dir, "bridge.json"), cfg); err != nil {
		t.Fatal(err)
	}
}

func receiveTestVoiceSMS(t *testing.T, dir, body string) {
	t.Helper()
	payload, err := json.Marshal(directGoogleVoiceSMS{
		ID:     "paused-notice-1",
		Sender: "8455551234",
		Thread: "/u/0/messages?itemId=t.%2B18455551234",
		Body:   body,
		At:     time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := appendDirectGoogleVoiceSMS(dir, string(payload)); err != nil {
		t.Fatal(err)
	}
}

func voiceActivityMessages(t *testing.T, dir string) []string {
	t.Helper()
	events := directGoogleVoiceActivity(dir).Recent(100)
	out := make([]string, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		out = append(out, events[i].Message)
	}
	return out
}

// While FlipAi is paused the Google Voice listener keeps running, so a text is
// still logged as received and its sender still logged as allowed -- and then
// nothing happens. Without a line naming the pause, that silence reads exactly
// like a broken bridge.
func TestPausedFlipAiSaysWhyAnAllowedTextIsNotReachingAnAgent(t *testing.T) {
	dir := t.TempDir()
	pausedNoticeTestConfig(t, dir, true)
	receiveTestVoiceSMS(t, dir, "what is the weather")

	lines := voiceActivityMessages(t, dir)
	found := false
	for _, line := range lines {
		if strings.Contains(line, "FlipAi is paused") && strings.Contains(line, "Resume") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a paused install never explained the waiting text: %v", lines)
	}

	// The pause still holds: the text is kept for delivery, not dropped.
	raw, err := os.ReadFile(googleVoiceSMSSpoolPath(dir))
	if err != nil || !strings.Contains(string(raw), "paused-notice-1") {
		t.Fatalf("a paused install dropped the text instead of holding it: %v", err)
	}
}

// Running normally, there is nothing to explain and nothing extra is written.
func TestRunningFlipAiDoesNotWriteAPausedNotice(t *testing.T) {
	dir := t.TempDir()
	pausedNoticeTestConfig(t, dir, false)
	receiveTestVoiceSMS(t, dir, "what is the weather")

	for _, line := range voiceActivityMessages(t, dir) {
		if strings.Contains(line, "FlipAi is paused") {
			t.Fatalf("a running install claimed to be paused: %q", line)
		}
	}
}
