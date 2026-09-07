package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedActivityRemovesGmailNoiseButKeepsUsefulVoiceDiagnostics(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	log := activityLogForStatePath(statePath)

	log.Add("warn", "gmail", "Gmail backend is not ready", "", "", "")
	log.Add("warn", "bridge", "SMS processing did not start. Check Gmail and agent diagnostics.", "", "", "")
	log.Add("info", "gmail", "New Google Voice candidate detected in Gmail", "", "", "candidate-1")
	log.Add("info", "google-voice", "Google Voice background session restored.", "", "", "")

	events := log.Recent(20)
	if len(events) != 3 {
		t.Fatalf("got %d visible events, want three useful Google Voice diagnostics: %#v", len(events), events)
	}
	for _, e := range events {
		if strings.Contains(strings.ToLower(e.Message), "gmail") {
			t.Fatalf("retired Gmail wording leaked into Activity: %#v", e)
		}
	}
	foundCandidate := false
	foundStartupWarning := false
	for _, e := range events {
		if e.Stage == "google-voice" && e.Message == "New Google Voice candidate detected" {
			foundCandidate = true
		}
		if e.Message == "SMS processing did not start. Check Google Voice and agent diagnostics." {
			foundStartupWarning = true
		}
	}
	if !foundCandidate || !foundStartupWarning {
		t.Fatalf("useful legacy events were not relabeled correctly: %#v", events)
	}

	b, err := os.ReadFile(filepath.Join(dir, "activity.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(b)), "gmail") {
		t.Fatalf("retired Gmail text was written to the activity log: %s", b)
	}
}

func TestPublishedActivityHidesHistoricGmailEntries(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	path := filepath.Join(dir, "activity.jsonl")
	old := `{"level":"warn","stage":"gmail","message":"Gmail startup failed"}` + "\n" +
		`{"level":"info","stage":"google-voice","message":"Google Voice ready"}` + "\n"
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}

	events := activityLogForStatePath(statePath).Recent(20)
	if len(events) != 1 || events[0].Message != "Google Voice ready" {
		t.Fatalf("historic retired Gmail entry leaked into Activity: %#v", events)
	}
}
