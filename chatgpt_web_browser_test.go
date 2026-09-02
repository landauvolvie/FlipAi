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

func TestChatGPTWebPageFlowInRealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser UI harness is skipped in -short mode")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	pw := playwrightModule(t)
	dir := t.TempDir()
	scriptFile := filepath.Join(dir, "chatgpt-init.js")
	if err := os.WriteFile(scriptFile, []byte(chatGPTWebInitScript), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", filepath.Join("testdata", "chatgptweb", "drive.mjs"))
	cmd.Env = append(scrubProxyEnv(os.Environ()),
		"FLIPAI_PLAYWRIGHT_MODULE="+pw,
		"FLIPAI_CHATGPT_SCRIPT_FILE="+scriptFile,
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
			t.Fatalf("ChatGPT browser driver failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
		}
	case <-time.After(90 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("ChatGPT browser driver timed out\nstderr:\n%s", stderr.String())
	}
	var report struct {
		Errors      []string `json:"errors"`
		BoundErrors []struct {
			Code string `json:"code"`
		} `json:"boundErrors"`
		Network struct {
			Text           string `json:"text"`
			Capture        string `json:"capture"`
			ConversationID string `json:"conversationId"`
		} `json:"network"`
		DOM struct {
			Text    string `json:"text"`
			Capture string `json:"capture"`
		} `json:"dom"`
		Submitted []any  `json:"submitted"`
		Composer  string `json:"composer"`
		StateCount int   `json:"stateCount"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &report); err != nil {
		t.Fatalf("could not decode ChatGPT browser report: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(report.Errors) > 0 || len(report.BoundErrors) > 0 {
		t.Fatalf("ChatGPT page script raised errors: page=%v bound=%v", report.Errors, report.BoundErrors)
	}
	if report.Network.Text != "NETWORK ANSWER" || report.Network.Capture != "network" || report.Network.ConversationID != "conv-network-1" {
		t.Fatalf("network capture failed: %+v", report.Network)
	}
	if report.DOM.Text != "DOM ANSWER" || report.DOM.Capture != "dom" {
		t.Fatalf("DOM fallback failed: %+v", report.DOM)
	}
	if len(report.Submitted) != 2 {
		t.Fatalf("submit callback count=%d want 2", len(report.Submitted))
	}
	if report.StateCount == 0 {
		t.Fatal("page state callback never ran")
	}
	if report.Composer != "hello dom" {
		t.Fatalf("composer was not updated through the DOM: %q", report.Composer)
	}
}
