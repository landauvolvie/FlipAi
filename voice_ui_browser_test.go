package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The card that turns phone calling on is a script, not a template, and every
// test FlipAi had stopped at the Go side of it. That is how the switch could
// stop saving without a single test noticing: the endpoint worked, the config
// round-tripped, and the page still could not turn calling on.
//
// This runs the exact script the Windows app injects in headless Chromium,
// against the real local voice endpoint and a real config on disk, and clicks
// the switch.
func TestVoiceCardTurnsCallingOnInARealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser UI harness is skipped in -short mode")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available; skipping the browser UI harness")
	}
	if _, err := os.Stat(playwrightModule); err != nil {
		t.Skip("playwright is not installed; skipping the browser UI harness")
	}

	// The script talks to a fixed loopback port, and the endpoint only answers
	// the FlipAi page's own origin, so both ports are fixed here too. A machine
	// already running FlipAi is not one this test can run on.
	voiceLn, err := net.Listen("tcp", voiceControlListen)
	if err != nil {
		t.Skipf("the local voice port is already in use: %v", err)
	}
	defer voiceLn.Close()
	pageLn, err := net.Listen("tcp", "127.0.0.1:8765")
	if err != nil {
		t.Skipf("the FlipAi UI port is already in use: %v", err)
	}
	defer pageLn.Close()

	dir := t.TempDir()
	// The state a fresh PC is in: every audio endpoint holding the same default
	// device, which is exactly the configuration that used to make the switch
	// unsavable.
	stuck := defaultVoiceCallConfig()
	stuck.GoogleVoiceInput = "Default - Remote Audio"
	stuck.GoogleVoiceOutput = "Default - Remote Audio"
	stuck.AgentInput = "Default - Remote Audio"
	stuck.AgentOutput = "Default - Remote Audio"
	if err := saveVoiceCallConfig(dir, stuck); err != nil {
		t.Fatal(err)
	}

	voiceSrv := &http.Server{Handler: voiceControlHandler(dir, "127.0.0.1:8765",
		func() Config { return voiceTestConfig(t) },
		activityLogForStatePath(filepath.Join(dir, "state.json")))}
	go func() { _ = voiceSrv.Serve(voiceLn) }()
	defer voiceSrv.Close()

	// A stand-in for the FlipAi window: the same shell the desktop UI marks as
	// trusted, the same .content element the card is appended to, and nothing
	// else, so a failure here is the card's.
	pageSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html data-flipai-desktop="1"><head><meta charset="utf-8"><title>FlipAi</title></head>`+
			`<body><div class="content"></div><script>globalThis.__flipaiDesktop=true;</script><script>%s</script></body></html>`,
			voiceDesktopInitScript)
	})}
	go func() { _ = pageSrv.Serve(pageLn) }()
	defer pageSrv.Close()

	// The panel position is reported while the page is open and withdrawn when
	// it leaves, so both facts have to be observed while the driver runs rather
	// than read off the end state.
	var sawPanel atomic.Bool
	watching := make(chan struct{})
	defer close(watching)
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-watching:
				return
			case <-t.C:
				if loadVoiceDock(dir).Active(time.Now()) {
					sawPanel.Store(true)
				}
			}
		}
	}()

	cmd := exec.Command("node", filepath.Join("testdata", "voiceui", "drive.mjs"))
	cmd.Env = append(scrubProxyEnv(os.Environ()), "FLIPAI_UI_PAGE=http://127.0.0.1:8765/connections")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("browser driver failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
		}
	case <-time.After(3 * time.Minute):
		_ = cmd.Process.Kill()
		t.Fatalf("browser driver timed out\nstderr:\n%s", stderr.String())
	}

	var report struct {
		Errors             []string `json:"errors"`
		Steps              []string `json:"steps"`
		Cards              []string `json:"cards"`
		StateAfter         string   `json:"stateAfter"`
		SavedNote          string   `json:"savedNote"`
		PanelWhileStarting string   `json:"panelWhileStarting"`
		PanelWhenOff       string   `json:"panelWhenOff"`
		PanelButtons       []string `json:"panelButtons"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("could not read the driver report: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if len(report.Errors) > 0 {
		t.Fatalf("the card raised errors in the browser: %v", report.Errors)
	}
	for _, want := range []string{"card-rendered", "switch-applied", "dock-reported", "field-autosaved", "switch-reverted", "panel-explains-and-retries", "panel-explains-off", "pending-save-flushed"} {
		found := false
		for _, got := range report.Steps {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the card never got as far as %q; steps=%v", want, report.Steps)
		}
	}

	// One card, not two.
	if len(report.Cards) != 1 || report.Cards[0] != "Google Voice Experimental" {
		t.Errorf("Connections should carry one Google Voice card, got %v", report.Cards)
	}
	if !strings.Contains(report.StateAfter, "On") {
		t.Errorf("the card did not report calling as on: %q", report.StateAfter)
	}
	if report.SavedNote != "Saved" {
		t.Errorf("the card did not confirm the write: %q", report.SavedNote)
	}

	// And what is on disk is what the page said. This is the assertion the
	// whole harness exists for.
	saved := loadVoiceCallConfig(dir)
	if saved.Enabled {
		t.Error("calling should have been switched back off by the last click")
	}
	if saved.DefaultAgent != "A" {
		t.Errorf("a field changed on the card was not written: %+v", saved.DefaultAgent)
	}
	// A change made and navigated away from within the save debounce still has
	// to land, or "changes save as you make them" is not true.
	if saved.Codex.AppTitle != "Saved On The Way Out" {
		t.Errorf("a change made just before leaving the page was lost: %q", saved.Codex.AppTitle)
	}

	// The panel is the thing the user stares at when Google Voice does not
	// appear. It has to say which of the several possible things is happening,
	// and hand over something to press.
	if strings.Contains(report.PanelWhileStarting, "Turn calling on above") {
		t.Error("the panel still tells the user to turn on a switch that is already on")
	}
	// This machine has no WebView2 runtime, so that is the true reason Google
	// Voice is not on screen and the panel has to be the one that says it.
	if !strings.Contains(report.PanelWhileStarting, "WebView2") {
		t.Errorf("the panel did not name the missing component: %q", report.PanelWhileStarting)
	}
	found := false
	for _, b := range report.PanelButtons {
		if strings.HasPrefix(b, "Retry") {
			found = true
		}
	}
	if !found {
		t.Errorf("the panel offered no way to try again: %v", report.PanelButtons)
	}
	if !strings.Contains(report.PanelWhenOff, "Calling is off") {
		t.Errorf("with calling off the panel said: %q", report.PanelWhenOff)
	}

	if !sawPanel.Load() {
		t.Error("the page never reported where to put the Google Voice window")
	}
	if dock := loadVoiceDock(dir); dock.Active(time.Now()) {
		t.Errorf("the page left the panel docked after navigating away: %+v", dock)
	}
}
