package main

import "testing"

func TestDesktopAccessibilityRecoveryOnlyTargetsChromeOnlyWindows(t *testing.T) {
	chromeOnly := agentVoiceState{
		Found:    true,
		Controls: []string{"ChatGPT", "Minimize", "Maximize", "Close"},
		Result:   "read",
	}
	if !shouldRestartAgentForAccessibility("ChatGPT", chromeOnly) {
		t.Fatal("the title-bar-only Electron state should trigger an automatic accessibility restart")
	}

	withContent := agentVoiceState{
		Found:    true,
		Controls: []string{"ChatGPT", "New chat", "Settings", "Close"},
		Result:   "not-found",
	}
	if shouldRestartAgentForAccessibility("ChatGPT", withContent) {
		t.Fatal("a readable renderer with an unknown Voice control must not restart the app")
	}

	withVoice := agentVoiceState{
		Found:        true,
		StartControl: "Start new voice chat",
		Controls:     []string{"ChatGPT", "Start new voice chat", "Close"},
	}
	if shouldRestartAgentForAccessibility("ChatGPT", withVoice) {
		t.Fatal("an app exposing its real Voice control must not restart")
	}

	active := agentVoiceState{
		Found:    true,
		Active:   true,
		Controls: []string{"ChatGPT", "Minimize", "Maximize", "Close"},
	}
	if shouldRestartAgentForAccessibility("ChatGPT", active) {
		t.Fatal("an active voice session must never be restarted")
	}
}
