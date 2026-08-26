package main

import (
	"strings"
	"testing"
)

// The feature lives on two pages, each doing one job: Settings carries the
// switch, the sign-ins, and every status check; Connections carries the live
// Google Voice preview and nothing else. This holds the script to that split.
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
	for _, gone := range []string{"agentCard(", "#codex-pane", "#claude-pane"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the voice UI still reaches into %s", gone)
		}
	}
	if strings.Contains(voiceDesktopInitScript, "allowedCallers") || strings.Contains(voiceDesktopInitScript, "allowedLabels") {
		t.Error("who may call is configured with the agent, not in the voice card")
	}
}

// The audio path is automatic now. Any picker for an audio endpoint on the
// page would be the old cable design leaking back in.
func TestVoiceUIHasNoAudioDevicePickers(t *testing.T) {
	for _, gone := range []string{
		"googleVoiceInput", "googleVoiceOutput", "agentInput", "agentOutput",
		"ringOutput", "deviceSelect(", "audioinput", "audiooutput", "virtual cable",
	} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the voice UI still carries manual audio wiring: %s", gone)
		}
	}
}

// Settings owns setup: sign in, sign out, the ChatGPT sign-in for the built-in
// Codex voice window, and the status rows the task of "is a call going to
// work" is answered by.
func TestVoiceSettingsCardCarriesSetupAndStatus(t *testing.T) {
	for _, want := range []string{
		"post('/signout')", "post('/open')", "post('/codex-open')",
		"vcs-google", "vcs-codex", "vcs-audio", "vcs-webview2", "vcs-permissions",
	} {
		if !strings.Contains(voiceDesktopInitScript, want) {
			t.Errorf("the Settings card is missing %s", want)
		}
	}
	// Codex needs no desktop-app configuration: the old ChatGPT window-title
	// and shortcut fields must not come back.
	for _, gone := range []string{"vcc-title", "vcc-shortcut", "vcc-command", "vcc-enabled"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the Settings card still configures a Codex desktop app: %s", gone)
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
