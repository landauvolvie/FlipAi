package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Muse answered, its answer was on screen, and every selector the driver knew
// missed it -- so the turn reported that the model had stopped without
// producing anything. Microsoft Copilot had its prompt typed into the box and
// then abandoned because no control matching "send" ever appeared.
//
// Both are the same underlying problem: a driver that only works on a page
// laid out the way FlipAi expects. This harness runs the real turn scripts in
// a real browser against a chat page that uses none of those names -- anonymous
// divs, no data-testid, no author roles, no <main> -- and, in one variant, no
// send button at all.
func TestBrowserDriversWorkOnAnUnfamiliarPage(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	pw := playwrightModule(t)

	cmd := exec.Command("node", filepath.Join("testdata", "browserdrivers", "drive.mjs"))
	cmd.Env = append(scrubProxyEnv(os.Environ()), "FLIPAI_PLAYWRIGHT_MODULE="+pw)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("browser driver harness failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(180 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("browser driver harness timed out\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}

	var report struct {
		OK       bool     `json:"ok"`
		Report   []string `json:"report"`
		Failures []string `json:"failures"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("harness output was not readable: %v\n%s", err, stdout.String())
	}
	if !report.OK || len(report.Failures) > 0 {
		t.Fatalf("drivers failed on an unfamiliar page:\n%s", strings.Join(report.Failures, "\n"))
	}
	if len(report.Report) != 24 {
		t.Fatalf("expected 24 driver scenarios, got %d: %v", len(report.Report), report.Report)
	}
	t.Log(strings.Join(report.Report, "\n"))
}
