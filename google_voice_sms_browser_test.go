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
	if strings.Contains(googleVoiceSMSInitScript, ".click(") ||
		strings.Contains(googleVoiceSMSInitScript, "aria-selected") ||
		strings.Contains(googleVoiceSMSInitScript, "gv-message-list-header") {
		t.Fatal("direct SMS inbound detector must not click, select, or open a Google Voice conversation")
	}

	pw := playwrightModule(t)
	dir := t.TempDir()
	fixture := filepath.Join(dir, "voice-sms.html")
	// Match the real background case: the visible Messages list shows only the
	// saved contact name "Me". The conversation is never selected or opened.
	// A Google Voice background response carries the exact t.+1 sender/thread,
	// then the list preview changes. A phone number inside the SMS body and an
	// unrelated conversation identity must never become the sender.
	html := `<!doctype html><html><head><meta charset="utf-8"><title>Voice messages</title></head><body>
<div id="threadRow" class="voice-thread-row" role="listitem" aria-label="Me">
  <span class="contact-name">Me</span>
  <div class="latest-preview"><span id="messageText">old message</span></div>
  <span class="time">10:17 PM</span>
</div>
<script>
  globalThis.__captured=[];
  globalThis.__rowClicks=0;
  globalThis.flipVoiceSMS=(payload)=>globalThis.__captured.push(payload);
  document.getElementById('threadRow').addEventListener('click',()=>globalThis.__rowClicks++);
</script>
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
		DetectorReady bool     `json:"detectorReady"`
		DetectorRows  int      `json:"detectorRows"`
		FinalURL      string   `json:"finalURL"`
		RowClicks     int      `json:"rowClicks"`
		Selected      string   `json:"selected"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &report); err != nil {
		t.Fatalf("could not parse SMS browser report: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(report.Errors) != 0 {
		t.Fatalf("SMS detector raised browser errors: %v", report.Errors)
	}
	if !report.DetectorReady || report.DetectorRows < 1 {
		t.Fatalf("background detector claimed ready without seeing the Google Voice list row: %+v", report)
	}
	if report.RowClicks != 0 || report.Selected != "" {
		t.Fatalf("background SMS detector interacted with the conversation UI: %+v", report)
	}
	if strings.Contains(report.FinalURL, "itemId") || !strings.HasSuffix(report.FinalURL, "/u/2/messages") {
		t.Fatalf("background detector navigated away from the Messages list: %q", report.FinalURL)
	}
	if len(report.Captured) != 1 {
		t.Fatalf("background inbound SMS was not captured exactly once: %v", report.Captured)
	}
	var payload struct {
		Sender string `json:"sender"`
		Thread string `json:"thread"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(report.Captured[0]), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Sender != "8455550142" {
		t.Fatalf("sender was not recovered from Google Voice background data: got %+v", payload)
	}
	if payload.Sender == "2125550199" {
		t.Fatal("phone number inside SMS body was incorrectly used as sender")
	}
	if payload.Sender == "9995550123" {
		t.Fatal("unrelated conversation identity was incorrectly used as sender")
	}
	if payload.Thread != "/u/2/messages?itemId=t.%2B18455550142" {
		t.Fatalf("wrong Google Voice itemId thread: %+v", payload)
	}
	if payload.Body != "X: call 212-555-0199" {
		t.Fatalf("wrong SMS body: %+v", payload)
	}
	if len(report.AfterOutgoing) != len(report.Captured) {
		t.Fatalf("outgoing Voice preview was mistaken for inbound SMS: before=%v after=%v", report.Captured, report.AfterOutgoing)
	}
	fmt.Fprint(os.Stdout, "direct Google Voice SMS background-only sender resolution passed\n")
}
