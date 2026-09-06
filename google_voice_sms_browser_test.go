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
	// Mirror the real Google Voice shape that v0.46.36 missed: the conversation
	// identity is an itemId query URL, the visible contact is only a saved name,
	// and the latest-message preview is a sibling of the anchor rather than a
	// child of a gv-conversation-list-item/role=listitem wrapper. The body also
	// contains a decoy phone number which must never become sender identity.
	html := `<!doctype html><html><head><meta charset="utf-8"><title>Voice messages</title></head><body>
<div class="voice-thread-tile">
  <a class="conversation-target" href="/u/2/messages?itemId=t.%2B18455550142"><span class="contact-name">US Mobile</span></a>
  <div class="latest-preview"><span id="messageText">old message</span> <a class="body-link" href="tel:+12125550199" title="212-555-0199">212-555-0199</a></div>
  <span class="time">10:17 PM</span>
</div>
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
		t.Fatalf("real-shape contact-name inbound SMS was not captured exactly once: %v", report.Captured)
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
		t.Fatalf("sender was not taken from Google Voice itemId identity; got %+v", payload)
	}
	if payload.Sender == "2125550199" {
		t.Fatal("clickable SMS-body phone number was incorrectly used as sender")
	}
	if payload.Thread != "/u/2/messages?itemId=t.%2B18455550142" {
		t.Fatalf("wrong Google Voice itemId thread: %+v", payload)
	}
	if payload.Body != "X: call 212-555-0199" {
		t.Fatalf("wrong SMS body: %+v", payload)
	}
	if len(report.AfterOutgoing) != len(report.Captured) {
		t.Fatalf("outgoing Voice row was mistaken for inbound SMS: before=%v after=%v", report.Captured, report.AfterOutgoing)
	}
	fmt.Fprint(os.Stdout, "direct Google Voice SMS real itemId DOM detection passed\n")
}
