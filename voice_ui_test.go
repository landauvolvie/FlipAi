package main

import (
	"strings"
	"testing"
)

func TestVoiceDesktopScriptOnlyRunsInDesktopShell(t *testing.T) {
	for _, want := range []string{
		"__flipaiDesktop", "dataset.flipaiDesktop", "'/connections'", "'/settings'",
	} {
		if !strings.Contains(voiceDesktopInitScript, want) {
			t.Errorf("voice desktop script is missing desktop guard/navigation token %s", want)
		}
	}
}

func TestVoiceCallingHasOneEnableSwitchAndNoAutoAnswerSwitch(t *testing.T) {
	for _, want := range []string{
		"vc-enabled", "post('/enable'", "On — answering calls",
	} {
		if !strings.Contains(voiceDesktopInitScript, want) {
			t.Errorf("voice UI is missing %s", want)
		}
	}
	for _, gone := range []string{"vc-auto-answer", "Auto-answer authorized callers"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("voice UI still exposes retired auto-answer control %s", gone)
		}
	}
}

func TestVoiceSettingsCardTargetsTheDesktopApps(t *testing.T) {
	for _, gone := range []string{"codex-open", "Sign in to ChatGPT", "vcs-codex", "chatgpt.com"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the Settings card still points at the hidden browser agent: %s", gone)
		}
	}
	for _, want := range []string{
		"post('/signout')", "location.href='/connections'",
		"vcc-title", "vcc-shortcut", "vcc-command",
		"vca-title", "vca-shortcut", "vca-command",
		"vcs-google", "vcs-cables", "vcs-routing", "vcs-audio", "vcs-webview2", "vcs-permissions",
		"test-agent?agent=",
	} {
		if !strings.Contains(voiceDesktopInitScript, want) {
			t.Errorf("the Settings card is missing %s", want)
		}
	}
}

// Connections is a view of the persistent Google Voice environment, not a
// second lifecycle for it. Leaving, scrolling, or changing pages only withdraws
// the panel; Google Voice itself stays loaded, signed in and listening.
func TestConnectionsPreviewOnlyWithdrawsThePanel(t *testing.T) {
	for _, want := range []string{"withdrawPanel", "sendBeacon", "keeps taking calls while it is out of sight"} {
		if !strings.Contains(voiceDesktopInitScript, want) {
			t.Errorf("the Connections preview is missing %s", want)
		}
	}
	for _, gone := range []string{"Open in its own window", "vc-pop-out"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("Connections still exposes the retired pop-out control %s", gone)
		}
	}
}
