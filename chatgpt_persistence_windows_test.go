//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestChatGPTSavedSessionAutoStartsFromTrayOnly(t *testing.T) {
	b, err := os.ReadFile("chatgpt_tray_supervisor_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`os.Args[1] != "--tray"`, "runChatGPTBackgroundSupervisor", "context.Background()"} {
		if !strings.Contains(s, want) {
			t.Fatalf("ChatGPT tray auto-start implementation missing %q", want)
		}
	}
}

func TestChatGPTBackgroundWorkerWaitsForRestoredAuth(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"waitForChatGPTPageSignedIn",
		"session-restoring",
		"Saved ChatGPT sign-in was restored",
		"background-restart-pending",
		"the ChatGPT WebView did not answer Runtime.evaluate",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("persistent ChatGPT worker implementation missing %q", want)
		}
	}
	if strings.Contains(s, "the Google Voice page did not answer Runtime.evaluate") {
		t.Fatal("ChatGPT diagnostics regressed to misleading Google Voice Runtime.evaluate wording")
	}
}
