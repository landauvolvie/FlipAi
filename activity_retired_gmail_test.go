package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedActivitySuppressesRetiredGmailNoise(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	log := activityLogForStatePath(statePath)

	log.Add("warn", "gmail", "Gmail backend is not ready", "", "", "")
	log.Add("warn", "bridge", "SMS processing did not start. Check Gmail and agent diagnostics.", "", "", "")
	log.Add("info", "google-voice", "Google Voice background session restored.", "", "", "")

	events := log.Recent(20)
	if len(events) != 1 {
		t.Fatalf("got %d visible events, want only the Google Voice event: %#v", len(events), events)
	}
	if events[0].Stage != "google-voice" {
		t.Fatalf("visible stage = %q, want google-voice", events[0].Stage)
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
