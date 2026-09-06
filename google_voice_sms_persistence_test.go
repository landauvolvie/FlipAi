package main

import (
	"os"
	"strings"
	"testing"
)

// One sign-in has to survive closing the window, restarting FlipAi, and
// restarting Windows. Running, Starting, Visible and LoginActive all describe a
// process, so a state file that outlived its process describes one that no
// longer exists. A persisted Starting used to block the restart guard forever
// and a persisted LoginActive made the supervisor believe a sign-in window was
// still open, so a connected install stayed down after a reboot.
func TestGoogleVoiceSMSSupervisorRecoversFromStaleProcessFlags(t *testing.T) {
	raw, err := os.ReadFile("google_voice_sms_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	start := strings.Index(body, "func runGoogleVoiceSMSBackgroundSupervisor")
	if start < 0 {
		t.Fatal("the Google Voice SMS background supervisor is gone")
	}
	supervisor := body[start:]
	if end := strings.Index(supervisor, "\nfunc "); end > 0 {
		supervisor = supervisor[:end]
	}
	for _, want := range []string{
		"googleVoiceSMSProcessAlive()",
		"platformEnsureGoogleVoiceSMSWorker",
		"v.Starting = false",
		"v.LoginActive = false",
		"s.UpdatedAt",
	} {
		if !strings.Contains(supervisor, want) {
			t.Fatalf("the supervisor can no longer restore a connected install: missing %q", want)
		}
	}
	if !strings.Contains(supervisor, "s.Connected") {
		t.Fatal("the supervisor no longer restores only an install that completed sign-in")
	}
}

// Readiness must never go back to the retired DOM observer's stamp, which
// nothing writes: keying off it made the listener permanently "not ready".
func TestGoogleVoiceSMSReadinessGateStaysOnTheLivePoll(t *testing.T) {
	raw, err := os.ReadFile("google_voice_sms_runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	start := strings.Index(body, "func googleVoiceSMSProbeFresh")
	if start < 0 {
		t.Fatal("the shared readiness check is gone")
	}
	gate := body[start:]
	if end := strings.Index(gate, "\nfunc "); end > 0 {
		gate = gate[:end]
	}
	if !strings.Contains(gate, "s.LastProbeAt") {
		t.Fatal("readiness is no longer proven by the poll that actually runs")
	}
	if strings.Contains(gate, "LastObserverAt") {
		t.Fatal("readiness regressed to the retired DOM observer stamp, which nothing writes")
	}

	// Every consumer must ask the same question, or the Connections card, Test
	// and the outbound send gate can disagree about what "connected" means.
	for _, path := range []string{"google_voice_sms.go", "google_voice_sms_control.go", "google_voice_sms_windows.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(source), "googleVoiceSMSConnected(") {
			t.Fatalf("%s no longer uses the shared readiness check", path)
		}
	}
}
