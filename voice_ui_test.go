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

// "Virtual audio cables: Not found" is where the user is looking when the audio
// path is broken, so it is where the way to fix it has to be. A button further
// down the page is a button that does not get found -- which is how a PC ends
// up with a call connected and no sound.
func TestTheMissingCableRowOffersTheInstall(t *testing.T) {
	if !strings.Contains(voiceDesktopInitScript, "function cablesCell()") {
		t.Fatal("the cables row no longer builds its own cell, so it cannot offer the install")
	}
	if !strings.Contains(voiceDesktopInitScript, "set('vcs-cables',cablesCell())") {
		t.Fatal("the cables row is not using the cell that offers the install")
	}
	if !strings.Contains(voiceDesktopInitScript, "__flipaiInstallAudioBridge") {
		t.Fatal("the cables row does not reach the audio-bridge installer")
	}
	// And the installer exposes it for the row to call.
	if !strings.Contains(voiceAudioDesktopScript, "globalThis.__flipaiInstallAudioBridge=async()=>{") {
		t.Fatal("the audio-bridge installer no longer exposes a way for the status row to start it")
	}
	// One cable is still a broken call, so that case must offer the second.
	if !strings.Contains(voiceDesktopInitScript, "'Get the second'") {
		t.Error("a machine with one cable is not offered the missing pair")
	}
	// FlipAi cannot install the driver itself -- Windows loads a virtual audio
	// driver only when Microsoft signed it -- so the button must not say it
	// will. That word is what turned a refusal Windows had already made into
	// the user's problem to debug.
	if strings.Contains(voiceDesktopInitScript, "b.textContent='Installing...'") {
		t.Error("the cables button still claims FlipAi is installing a driver")
	}
}

// "Desktop app audio: Waiting" on a PC with no virtual cable installed sent the
// user to look at the desktop app, which was not the problem. Each outcome now
// says which one it is, and the missing cable is reported once -- by the row
// that can fix it -- rather than twice.
func TestTheRoutingRowSaysWhichThingWentWrong(t *testing.T) {
	if strings.Contains(voiceDesktopInitScript, "/Applied automatically|is wired to the cables/") {
		t.Fatal("the routing row is still guessing its state from the note's wording")
	}
	for _, state := range []string{"'applied'", "'no-cables'", "'waiting-for-app'", "'refused'"} {
		if !strings.Contains(voiceDesktopInitScript, "case "+state+":") {
			t.Errorf("the routing row does not report the %s outcome", state)
		}
	}
	if !strings.Contains(voiceDesktopInitScript, "No cable to route to") {
		t.Error("a PC with no cable is not told that is what is missing")
	}
	// A missing cable belongs to the cables row, which offers the install. The
	// routing row repeating it reads as a second, unrelated failure.
	if !strings.Contains(voiceDesktopInitScript, "rt.routingState!=='no-cables'") {
		t.Error("the missing cable is reported twice")
	}
}
