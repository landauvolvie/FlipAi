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

func TestChatGPTPageDriverInRealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser harness is skipped in -short mode")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	pw := playwrightModule(t)
	cmd := exec.Command("node", filepath.Join("testdata", "chatgpt", "drive.mjs"))
	cmd.Env = append(scrubProxyEnv(os.Environ()), "FLIPAI_PLAYWRIGHT_MODULE="+pw)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ChatGPT browser harness failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
		}
	case <-time.After(45 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("ChatGPT browser harness timed out\nstderr:\n%s", stderr.String())
	}
	var report struct {
		Result struct {
			OK    bool   `json:"ok"`
			Reply string `json:"reply"`
		} `json:"result"`
		Composer string   `json:"composer"`
		Errors   []string `json:"errors"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &report); err != nil {
		t.Fatalf("bad browser report: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(report.Errors) != 0 {
		t.Fatalf("page driver raised browser errors: %v", report.Errors)
	}
	if report.Composer != "browser harness prompt" {
		t.Fatalf("page driver did not fill composer: %q", report.Composer)
	}
	if !report.Result.OK || report.Result.Reply != "FLIPAI browser response for browser harness prompt" {
		t.Fatalf("page driver did not wait for final response: %+v", report.Result)
	}
}
