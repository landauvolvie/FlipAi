package main

import (
	"errors"
	"testing"
	"time"
)

func TestGoogleVoiceMMSMissingMediaIsRetriedWhileFresh(t *testing.T) {
	now := time.Now()
	m := GmailMessage{
		ID:           "gv-api-fresh",
		Body:         "Google Voice\nMMS Received",
		Snippet:      "MMS Received",
		InternalDate: now.Add(-30 * time.Second),
	}
	got, ok := staleGoogleVoiceMMSNoop(m, errors.New("No MMS media element was found next to the received message."), now)
	if ok {
		t.Fatalf("fresh MMS was suppressed instead of retried: %+v", got)
	}
}

func TestGoogleVoiceMMSMissingMediaStopsReplayingWhenStale(t *testing.T) {
	now := time.Now()
	m := GmailMessage{
		ID:           "gv-api-stale",
		Body:         "Google Voice\nMMS Received",
		Snippet:      "MMS Received",
		InternalDate: now.Add(-10 * time.Minute),
	}
	got, ok := staleGoogleVoiceMMSNoop(m, errors.New("No MMS media element was found next to the received message."), now)
	if !ok {
		t.Fatal("stale MMS marker remained retryable forever")
	}
	if got.ID != m.ID || got.InternalDate != m.InternalDate {
		t.Fatal("stale MMS checkpoint identity/date changed")
	}
	if got.Snippet != "" || got.Body != "Google Voice\n" || len(got.Attachments) != 0 {
		t.Fatalf("stale MMS was not converted into a non-executable checkpoint candidate: %+v", got)
	}
}

func TestGoogleVoiceMMSUnrelatedErrorsRemainFailures(t *testing.T) {
	now := time.Now()
	m := GmailMessage{ID: "gv-api-error", InternalDate: now.Add(-time.Hour)}
	if _, ok := staleGoogleVoiceMMSNoop(m, errors.New("Google Voice media browser is unavailable"), now); ok {
		t.Fatal("an unrelated media failure was incorrectly suppressed")
	}
}
