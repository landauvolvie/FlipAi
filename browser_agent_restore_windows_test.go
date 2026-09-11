//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestTrayRestoresEveryConnectedBrowserAgent(t *testing.T) {
	raw, err := os.ReadFile("chatgpt_tray_supervisor_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"prepareChatGPTRuntimeForTray",
		"prepareClaudeChatRuntimeForTray",
		"prepareGeminiChatRuntimeForTray",
		"prepareGrokChatRuntimeForTray",
		"prepareCopilotChatRuntimeForTray",
		"prepareMuseChatRuntimeForTray",
		"runChatGPTBackgroundSupervisor",
		"runClaudeChatBackgroundSupervisor",
		"runGeminiChatBackgroundSupervisor",
		"runGrokChatBackgroundSupervisor",
		"runCopilotChatBackgroundSupervisor",
		"runMuseChatBackgroundSupervisor",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("tray startup lost browser restore hook %q", want)
		}
	}
}
