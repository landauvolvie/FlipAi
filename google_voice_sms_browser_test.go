package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGoogleVoiceSMSDetectionInRealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser SMS harness is skipped in -short mode")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available; skipping the browser SMS harness")
	}
	pw := playwrightModule(t)
	dir := t.TempDir()
	fixture := filepath.Join(dir, "voice-sms.html")
	html := `<!doctype html><html><head><meta charset="utf-8"><title>Voice messages</title></head><body>
<gv-conversation-list-item role="listitem" aria-label="US Mobile">
  <a href="/u/2/messages/contact" title="+1 (845) 555-0142"><span class="contact">US Mobile</span></a>
  <span id="snippet" class="snippet">old message</span>
</gv-conversation-list-item>
<script>globalThis.__captured=[];globalThis.flipVoiceSMS=(payload)=>globalThis.__captured.push(payload);</script>
<script>` + googleVoiceSMSInitScript + `</script></body></html>`
	if err := os.WriteFile(fixture, []byte(html), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("node", filepath.Join("testdata", "voicesms", "drive.mjs"))
	cmd.Env = append(scrubProxyEnv(os.Environ()),
		"FLIPAI_GV_SMS_FIXTURE="+fixture,
		"FLIPAI_PLAYWRIGHT_MODULE="+pw,
	)
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
			t.Fatalf("SMS browser driver failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
		}
	case <-time.After(2 * time.Minute):
		_ = cmd.Process.Kill()
		t.Fatalf("SMS browser driver timed out\nstderr:\n%s", stderr.String())
	}

	var report struct {
		Errors        []string `json:"errors"`
		Captured      []string `json:"captured"`
		AfterOutgoing []string `json:"afterOutgoing"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &report); err != nil {
		t.Fatalf("could not parse SMS browser report: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(report.Errors) != 0 {
		t.Fatalf("SMS detector raised browser errors: %v", report.Errors)
	}
	if len(report.Captured) != 1 {
		t.Fatalf("contact-name inbound SMS was not captured exactly once: %v", report.Captured)
	}
	var payload struct {
		Sender string `json:"sender"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(report.Captured[0]), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Sender != "8455550142" || payload.Body != "X: hi" {
		t.Fatalf("wrong SMS payload: %+v", payload)
	}
	if len(report.AfterOutgoing) != len(report.Captured) {
		t.Fatalf("outgoing Voice row was mistaken for inbound SMS: before=%v after=%v", report.Captured, report.AfterOutgoing)
	}
	fmt.Fprint(os.Stdout, "direct Google Voice SMS browser detection passed\n")
}
