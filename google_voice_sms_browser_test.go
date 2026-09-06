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
	// Mirror the live v0.46.38 failure: the linkless conversation row contains
	// only a saved contact name. Clicking it leaves location.href on /messages
	// and the opened Google Voice pane exposes the sender only as visible header
	// text: "Me" plus "mobile • (845) 555-0142". Keep unrelated preloaded
	// identity metadata on the page so the detector must bind to the explicitly
	// opened header. The SMS body also contains a decoy phone number which may
	// never become sender identity.
	html := `<!doctype html><html><head><meta charset="utf-8"><title>Voice messages</title></head><body>
<div id="preloaded" data-conversation-id="t.+19995550123" hidden></div>
<div id="threadRow" class="voice-thread-row" role="listitem" aria-label="Me">
  <span class="contact-name">Me</span>
  <div class="latest-preview"><span id="messageText">old message</span> <a class="body-link" href="tel:+12125550199" title="212-555-0199">212-555-0199</a></div>
  <span class="time">10:17 PM</span>
</div>
<div id="conversationPane"></div>
<script>
  globalThis.__captured=[];
  globalThis.flipVoiceSMS=(payload)=>globalThis.__captured.push(payload);
  document.getElementById('threadRow').addEventListener('click',()=>{
    document.getElementById('threadRow').setAttribute('aria-selected','true');
    document.getElementById('conversationPane').innerHTML='<gv-thread-details><gv-message-list-header><div>Me</div><p>mobile • (845) 555-0142</p></gv-message-list-header></gv-thread-details>';
  });
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
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &report); err != nil {
		t.Fatalf("could not parse SMS browser report: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(report.Errors) != 0 {
		t.Fatalf("SMS detector raised browser errors: %v", report.Errors)
	}
	if !report.DetectorReady || report.DetectorRows < 1 {
		t.Fatalf("detector claimed ready without seeing the linkless Google Voice row: %+v", report)
	}
	if strings.Contains(report.FinalURL, "itemId") || !strings.HasSuffix(report.FinalURL, "/u/2/messages") {
		t.Fatalf("fixture unexpectedly exposed sender through browser URL: %q", report.FinalURL)
	}
	if len(report.Captured) != 1 {
		t.Fatalf("saved-contact inbound SMS with visible opened-header phone was not captured exactly once: %v", report.Captured)
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
		t.Fatalf("sender was not recovered from the opened Google Voice visible phone header while URL stayed unchanged: got %+v", payload)
	}
	if payload.Sender == "2125550199" {
		t.Fatal("clickable SMS-body phone number was incorrectly used as sender")
	}
	if payload.Sender == "9995550123" {
		t.Fatal("unrelated preloaded conversation identity was incorrectly used as sender")
	}
	if payload.Thread != "/u/2/messages?itemId=t.%2B18455550142" {
		t.Fatalf("wrong Google Voice itemId thread: %+v", payload)
	}
	if payload.Body != "X: call 212-555-0199" {
		t.Fatalf("wrong SMS body: %+v", payload)
	}
	if len(report.AfterOutgoing) != len(report.Captured) {
		t.Fatalf("outgoing Voice row or opened header was mistaken for inbound SMS: before=%v after=%v", report.Captured, report.AfterOutgoing)
	}
	fmt.Fprint(os.Stdout, "direct Google Voice SMS visible-header sender resolution passed\n")
}
