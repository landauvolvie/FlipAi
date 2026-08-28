package main

import (
	"strings"
	"testing"
	"time"
)

func TestADesktopRequestIsPerformedOnceThenGone(t *testing.T) {
	dir := t.TempDir()
	requestVoiceDesktopAction(dir, voiceDesktopOpen)

	if got := takeVoiceDesktopAction(dir); got != voiceDesktopOpen {
		t.Fatalf("first take = %q, want %q", got, voiceDesktopOpen)
	}
	if got := takeVoiceDesktopAction(dir); got != "" {
		t.Errorf("the same request was performed twice: %q", got)
	}
}

// A request made while nobody was signed in must not fire the next time the
// user signs in and the worker starts, minutes or hours later.
func TestAStaleDesktopRequestIsDroppedNotReplayed(t *testing.T) {
	dir := t.TempDir()
	mutateVoiceRuntime(dir, func(s *VoiceRuntimeState) {
		s.DesktopRequest = voiceDesktopRestart
		s.DesktopRequestAt = time.Now().Add(-voiceDesktopRequestTTL - time.Minute)
	})
	if got := takeVoiceDesktopAction(dir); got != "" {
		t.Errorf("a stale request was replayed: %q", got)
	}
	// And it is cleared, so it cannot accumulate.
	if s := loadVoiceRuntime(dir); s.DesktopRequest != "" {
		t.Errorf("the stale request was not cleared: %q", s.DesktopRequest)
	}
}

// The whole point of the change: when the host has a desktop it acts directly;
// when it does not, it hands the work off rather than failing.
func TestOpenActsDirectlyWhenInteractiveAndDelegatesWhenNot(t *testing.T) {
	restore := voiceSessionInteractive
	restoreOpen := openGoogleVoiceWindow
	defer func() { voiceSessionInteractive = restore; openGoogleVoiceWindow = restoreOpen }()

	t.Run("interactive host opens directly", func(t *testing.T) {
		dir := t.TempDir()
		voiceSessionInteractive = func() bool { return true }
		called := false
		openGoogleVoiceWindow = func(_ string, show bool) error {
			called = true
			if !show {
				t.Error("the UI open must ask for the window to be shown")
			}
			return nil
		}
		if err := voiceOpenForUI(dir); err != nil {
			t.Fatalf("interactive open failed: %v", err)
		}
		if !called {
			t.Error("an interactive host did not open the window itself")
		}
		if s := loadVoiceRuntime(dir); s.DesktopRequest != "" {
			t.Errorf("an interactive host handed the work off anyway: %q", s.DesktopRequest)
		}
	})

	t.Run("non-interactive host hands off and reports the worker's outcome", func(t *testing.T) {
		dir := t.TempDir()
		voiceSessionInteractive = func() bool { return false }
		openGoogleVoiceWindow = func(_ string, _ bool) error {
			t.Fatal("a non-interactive host must not try to open the window itself")
			return nil
		}
		// Stand in for the interactive worker: it picks the request up and
		// brings Google Voice online a moment later.
		go func() {
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if takeVoiceDesktopAction(dir) == voiceDesktopOpen {
					mutateVoiceRuntime(dir, func(s *VoiceRuntimeState) { s.BrowserRunning = true })
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
		if err := voiceOpenForUI(dir); err != nil {
			t.Fatalf("a handed-off open reported failure though the worker succeeded: %v", err)
		}
	})
}

// A handed-off open that the worker could not satisfy must return the real
// reason, not a generic one, when the worker recorded it.
func TestHandedOffOpenReportsTheWorkersRecordedReason(t *testing.T) {
	restore := voiceSessionInteractive
	defer func() { voiceSessionInteractive = restore }()
	voiceSessionInteractive = func() bool { return false }

	dir := t.TempDir()
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if takeVoiceDesktopAction(dir) == voiceDesktopOpen {
				mutateVoiceRuntime(dir, func(s *VoiceRuntimeState) {
					s.LastOpenError = "the Edge WebView2 Runtime is not installed"
					s.LastOpenAt = time.Now()
				})
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	err := voiceOpenForUI(dir)
	if err == nil || !strings.Contains(err.Error(), "WebView2 Runtime") {
		t.Fatalf("handed-off open did not surface the worker's reason, got %v", err)
	}
}
