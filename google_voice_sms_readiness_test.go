package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The Google Voice SMS listener proved itself through LastObserverAt, which
// belonged to a DOM observer that no longer exists and is written by nothing.
// Readiness could therefore never become true: Connections stayed "Not
// connected", Test always timed out, and every reply failed with "background
// connection is not ready". Readiness must follow the poll that actually runs.
func TestGoogleVoiceSMSReadinessFollowsTheLivePoll(t *testing.T) {
	dir := t.TempDir()
	mutateGoogleVoiceSMSRuntime(dir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Running = true
		s.Connected = true
		s.SignedIn = true
		s.ListenerRunning = true
		s.Ready = true
		s.LastProbeAt = time.Now()
	})

	got := loadGoogleVoiceSMSRuntime(dir)
	if !got.Ready || !got.ListenerRunning {
		t.Fatalf("a listener that just polled was reported as not ready: %+v", got)
	}
	if !googleVoiceSMSConnected(got) {
		t.Fatalf("a live listener was not reported as connected: %+v", got)
	}
}

func TestGoogleVoiceSMSReadinessExpiresWithoutAFreshPoll(t *testing.T) {
	dir := t.TempDir()
	mutateGoogleVoiceSMSRuntime(dir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Running = true
		s.Connected = true
		s.SignedIn = true
		s.ListenerRunning = true
		s.Ready = true
		s.LastProbeAt = time.Now().Add(-time.Hour)
	})

	got := loadGoogleVoiceSMSRuntime(dir)
	if got.Ready || got.ListenerRunning {
		t.Fatalf("a stale listener was still reported ready: %+v", got)
	}
	if googleVoiceSMSConnected(got) {
		t.Fatal("a stale listener was still reported as connected")
	}
}

// A state file with no probe at all is the state a never-started listener
// leaves behind, and it must not read as ready either.
func TestGoogleVoiceSMSReadinessRequiresAProbe(t *testing.T) {
	dir := t.TempDir()
	mutateGoogleVoiceSMSRuntime(dir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Running = true
		s.Connected = true
		s.SignedIn = true
		s.ListenerRunning = true
		s.Ready = true
	})
	if got := loadGoogleVoiceSMSRuntime(dir); got.Ready || googleVoiceSMSConnected(got) {
		t.Fatalf("readiness was believed without any recorded poll: %+v", got)
	}
}

// The retired observer field must not come back as a readiness source.
func TestGoogleVoiceSMSReadinessIgnoresTheRetiredObserverStamp(t *testing.T) {
	dir := t.TempDir()
	mutateGoogleVoiceSMSRuntime(dir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Running = true
		s.Connected = true
		s.SignedIn = true
		s.ListenerRunning = true
		s.Ready = true
		s.LastObserverAt = time.Now()
	})
	if got := loadGoogleVoiceSMSRuntime(dir); got.Ready {
		t.Fatal("readiness is being taken from the retired DOM observer stamp again")
	}
}

// Readiness is written by one process and read by another, so it has to survive
// the state file rather than living in memory.
func TestGoogleVoiceSMSReadinessCrossesProcesses(t *testing.T) {
	dir := t.TempDir()
	mutateGoogleVoiceSMSRuntime(dir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Running = true
		s.Connected = true
		s.SignedIn = true
		s.ListenerRunning = true
		s.Ready = true
		s.LastProbeAt = time.Now()
		s.LastNote = "2 conversation(s), 7 message(s)"
	})
	raw, err := os.ReadFile(googleVoiceSMSRuntimePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var reloaded GoogleVoiceSMSRuntimeState
	if err := json.Unmarshal(raw, &reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.LastProbeAt.IsZero() {
		t.Fatal("the readiness stamp is not persisted, so another process can never see it")
	}
	if reloaded.LastNote == "" {
		t.Fatal("the listener diagnostic is not persisted for the Connections card")
	}
}
