package main

import (
	"strings"
	"testing"
)

// The feature lives on two pages, each doing one job: Settings carries the
// switch, the sign-in/sign-out, the desktop apps, and every status check;
// Connections carries the live Google Voice preview and nothing else. This
// holds the script to that split.
func TestGoogleVoiceSplitsBetweenSettingsAndConnections(t *testing.T) {
	if !strings.Contains(voiceDesktopInitScript, "if(here==='/settings') settingsCard();") {
		t.Fatal("Settings must build the Google Voice calling card")
	}
	if !strings.Contains(voiceDesktopInitScript, "else connectionsCard();") {
		t.Fatal("Connections must build the live preview card")
	}
	if !strings.Contains(voiceDesktopInitScript, "here!=='/connections'&&here!=='/settings'") {
		t.Fatal("the voice controls must be installed on Settings and Connections and nowhere else")
	}
	// Nothing on either card waits for a Save button, and the switch that turns
	// calling on writes through an endpoint that cannot be held up by the rest
	// of the page.
	if !strings.Contains(voiceDesktopInitScript, "post('/enable'") {
		t.Error("the calling switch must save on its own, through /enable")
	}
	if strings.Contains(voiceDesktopInitScript, "Save voice settings") {
		t.Error("the card must not depend on a Save button any more")
	}
	// The Google Voice window is shown inside the app, not as a popup.
	for _, want := range []string{"gv-embed-slot", "post('/dock'"} {
		if !strings.Contains(voiceDesktopInitScript, want) {
			t.Errorf("the embedded Google Voice panel is missing %s", want)
		}
	}
	if strings.Contains(voiceDesktopInitScript, "allowedCallers") || strings.Contains(voiceDesktopInitScript, "allowedLabels") {
		t.Error("who may call is configured with the agent, not in the voice card")
	}
}

// Answering is not an option. With calling enabled, an authorized caller is
// answered and an unauthorized one is not; a separate auto-answer switch was
// an unnecessary second gate and must not come back.
func TestVoiceUIHasNoAutoAnswerSwitch(t *testing.T) {
	for _, gone := range []string{"vc-auto", "autoAnswer", "Auto-answer"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the voice UI still carries the retired auto-answer switch: %s", gone)
		}
	}
}

// The audio wiring is automatic. Pickers would be the old manual design
// leaking back in; the page shows the chosen wiring, it never asks for it.
func TestVoiceUIHasNoAudioDevicePickers(t *testing.T) {
	for _, gone := range []string{"deviceSelect(", "audioinput", "audiooutput", "vc-gv-in", "vc-gv-out", "vc-agent-in", "vc-agent-out", "vc-ring"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the voice UI still carries manual audio wiring: %s", gone)
		}
	}
}

// The call goes to the desktop app, not to any hidden browser agent: the
// ChatGPT-in-a-browser sign-in must be gone, and the desktop app fields for
// both agents must be present.
func TestVoiceSettingsCardTargetsTheDesktopApps(t *testing.T) {
	for _, gone := range []string{"codex-open", "Sign in to ChatGPT", "vcs-codex", "chatgpt.com"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the Settings card still points at the hidden browser agent: %s", gone)
		}
	}
	for _, want := range []string{
		"post('/signout')", "post('/open')",
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

// Leaving or closing the Connections preview must never stop Google Voice: the
// page only withdraws the panel; the window itself runs on.
func TestConnectionsPreviewOnlyWithdrawsThePanel(t *testing.T) {
	for _, want := range []string{"withdrawPanel", "sendBeacon", "keeps running in the background"} {
		if !strings.Contains(voiceDesktopInitScript, want) {
			t.Errorf("the Connections preview is missing %s", want)
		}
	}
}
