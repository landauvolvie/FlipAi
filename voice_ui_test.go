package main

import (
	"strings"
	"testing"
)

func TestGoogleVoiceLivesOnlyOnConnections(t *testing.T) {
	// Google Voice is a connection. Its controls used to be spread over
	// Settings and both agent panes as well.
	if !strings.Contains(voiceDesktopInitScript, "if(location.pathname!=='/connections')") {
		t.Fatal("the voice controls must be installed on Connections and nowhere else")
	}
	if !strings.Contains(voiceDesktopInitScript, "voiceCard()") {
		t.Fatal("Connections must build the Google Voice card")
	}
	// One card, not two. The summary and the form used to describe the same
	// connection twice, with the switch in one and the status in the other.
	for _, gone := range []string{"connectionsCard(", "settingsCard(", "Google Voice phone bridge"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the voice UI still has the split card: %s", gone)
		}
	}
	// Nothing on the card waits for a Save button, and the switch that turns
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
	for _, gone := range []string{"'/settings'", "agentCard(", "#codex-pane", "#claude-pane"} {
		if strings.Contains(voiceDesktopInitScript, gone) {
			t.Errorf("the voice UI still reaches into %s", gone)
		}
	}
	if strings.Contains(voiceDesktopInitScript, "allowedCallers") || strings.Contains(voiceDesktopInitScript, "allowedLabels") {
		t.Error("who may call is configured with the agent, not in the voice card")
	}
}
