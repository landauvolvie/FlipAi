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

// A photo texted to Gemini came back as "This chat has no file picker that
// accepts these attachments" while the picker was on the page: it lived inside
// a shadow root, behind an upload menu, and the finder only ever looked at the
// top-level document.
//
// This runs the real finder against composers that hide their input the way a
// component-built UI does.
func TestAttachmentPickerIsFoundInAComponentComposer(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	pw := playwrightModule(t)

	// The finder as FlipAi actually builds it, for one image attachment.
	findJS := browserChatFindFileInputJS([]browserChatAttachment{
		{Path: filepath.Join(t.TempDir(), "photo.jpg"), MediaType: "image/jpeg"},
	})
	scriptPath := filepath.Join(t.TempDir(), "find-input.js")
	if err := os.WriteFile(scriptPath, []byte(findJS), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("node", filepath.Join("testdata", "browserdrivers", "attachments.mjs"))
	cmd.Env = append(scrubProxyEnv(os.Environ()),
		"FLIPAI_PLAYWRIGHT_MODULE="+pw,
		"FLIPAI_FIND_INPUT_JS="+scriptPath,
	)
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
			t.Fatalf("attachment harness failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(120 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("attachment harness timed out\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}

	var report struct {
		OK       bool     `json:"ok"`
		Report   []string `json:"report"`
		Failures []string `json:"failures"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("harness output was not readable: %v\n%s", err, stdout.String())
	}
	if !report.OK {
		t.Fatalf("the file picker was not found:\n%s", strings.Join(report.Failures, "\n"))
	}
	t.Log(strings.Join(report.Report, "\n"))
}
