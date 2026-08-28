package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The failure this file exists because of, in one sentence: FlipAi offered a
// one-click button that downloaded a kernel-mode audio driver signed by
// SignPath Foundation, and Windows will not load a kernel driver unless
// Microsoft signed it. Every press ended in problem code 52 on a stock PC.
//
// These tests hold FlipAi to not making that promise again.

func TestFlipAiNeverShipsADriverWindowsCannotLoad(t *testing.T) {
	// Nothing in the product may reach for the driver package again, nor for
	// the device-node tool that only existed to install it.
	banned := map[string]string{
		"VirtualDrivers/Virtual-Audio-Driver": "the SignPath-signed driver Windows rejects with problem code 52",
		"nefarius/nefcon":                     "the device-node tool that only existed to install that driver",
		"nefconc":                             "the device-node tool that only existed to install that driver",
		"create-device-node":                  "creating a device node for a driver Windows will not start",
		"testsigning":                         "test-signing, which FlipAi must never switch on",
		"bcdedit":                             "boot configuration, which FlipAi must never touch",
	}
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// The explanation of why this cannot work is allowed to name these;
		// code that acts on them is not. Only voice_audio_bridge.go, which is
		// that explanation, may mention them at all.
		if path == "voice_audio_bridge.go" {
			continue
		}
		text := strings.ToLower(string(body))
		for needle, why := range banned {
			if strings.Contains(text, strings.ToLower(needle)) {
				t.Errorf("%s references %q -- %s", path, needle, why)
			}
		}
	}
}

func TestTheAudioBridgeSetupNamesAFreeSignedCable(t *testing.T) {
	setup := planVoiceAudioBridge(planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "Speakers (Realtek High Definition Audio)"),
		dev("audioinput", "Microphone (Realtek High Definition Audio)"),
	}))
	if setup.Done {
		t.Fatal("a PC with no cables was told the audio bridge was ready")
	}
	if setup.Next.Name == "" || !strings.HasPrefix(setup.Next.URL, "https://") {
		t.Fatalf("no vendor to send the user to: %+v", setup.Next)
	}
	joined := strings.Join(setup.Steps, " ")
	if !strings.Contains(joined, setup.Next.URL) {
		t.Errorf("the steps never give the address to go to: %q", joined)
	}
	if !strings.Contains(strings.ToLower(joined), "free") {
		t.Errorf("the steps do not say it is free, which is the first thing anyone asks: %q", joined)
	}
	// With nothing installed, the user needs to know there are two of them
	// before they start, not after they finish the first.
	if len(setup.Steps) < 4 {
		t.Errorf("a bare PC was not told it needs both pairs: %q", joined)
	}
}

// Someone who already installed one pair must be sent to the other one, not
// told again to install what they have.
func TestTheSetupSendsYouToThePairYouDoNotHave(t *testing.T) {
	withCable := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "CABLE Input (VB-Audio Virtual Cable)"),
		dev("audioinput", "CABLE Output (VB-Audio Virtual Cable)"),
	})
	setup := planVoiceAudioBridge(withCable)
	if setup.Done {
		t.Fatal("one pair was reported as a complete bridge")
	}
	if !strings.Contains(strings.ToLower(setup.Next.Name), "voicemeeter") {
		t.Errorf("a PC that already has the cable was sent to %q, want the other pair", setup.Next.Name)
	}
	if !strings.Contains(setup.Headline, "second") {
		t.Errorf("headline does not say what is missing: %q", setup.Headline)
	}

	withVoiceMeeter := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "VoiceMeeter Input (VB-Audio VoiceMeeter VAIO)"),
		dev("audioinput", "VoiceMeeter Output (VB-Audio VoiceMeeter VAIO)"),
	})
	setup = planVoiceAudioBridge(withVoiceMeeter)
	if !strings.Contains(strings.ToUpper(setup.Next.Name), "CABLE") {
		t.Errorf("a PC that already has VoiceMeeter was sent to %q, want the cable", setup.Next.Name)
	}
}

// Both recommended pairs must be ones planVoiceCables actually recognizes, or
// the user installs them and FlipAi still says none are found -- which is the
// worst outcome available and exactly what happened with the old driver.
func TestEverySourceFlipAiRecommendsIsOneItCanWire(t *testing.T) {
	plan := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "CABLE Input (VB-Audio Virtual Cable)"),
		dev("audioinput", "CABLE Output (VB-Audio Virtual Cable)"),
		dev("audiooutput", "VoiceMeeter Input (VB-Audio VoiceMeeter VAIO)"),
		dev("audioinput", "VoiceMeeter Output (VB-Audio VoiceMeeter VAIO)"),
		dev("audiooutput", "Speakers (Realtek)"),
		dev("audioinput", "Microphone (Realtek)"),
	})
	if !plan.complete() || plan.Warning != "" {
		t.Fatalf("installing exactly what FlipAi recommends did not produce a working bridge: %+v", plan)
	}
	setup := planVoiceAudioBridge(plan)
	if !setup.Done {
		t.Fatalf("installing both recommended pairs still asked for more: %+v", setup)
	}
	for _, wired := range []string{plan.GoogleVoiceOutput, plan.GoogleVoiceInput, plan.AgentInput, plan.AgentOutput} {
		if strings.Contains(strings.ToLower(wired), "realtek") {
			t.Errorf("the PC's own hardware was wired into the call: %q", wired)
		}
	}
}

// The explanation shown where the old button was has to be honest about all
// three things: that it used to be automatic, why it is not, and that FlipAi
// is not going to weaken the PC to make it automatic again.
func TestFlipAiExplainsWhyItNoLongerInstallsTheDriver(t *testing.T) {
	for _, want := range []string{"52", "Microsoft", "Secure Boot", "test-signing"} {
		if !strings.Contains(voiceAudioBridgeWhyNotAutomatic, want) {
			t.Errorf("the explanation never mentions %q: %q", want, voiceAudioBridgeWhyNotAutomatic)
		}
	}
}
