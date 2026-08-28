package main

import (
	"fmt"
	"strings"
)

// FlipAi needs two independent virtual speaker/microphone pairs to carry a phone
// call between Google Voice and the desktop AI app. Getting them onto a PC is a
// one-time job, and this file is the whole of what FlipAi knows about doing it.
//
// It used to be a one-click button that downloaded a kernel driver and installed
// it. That could never work, and this is worth writing down so it is not tried
// again: on 64-bit Windows 10 and 11, the Code Integrity engine loads a
// kernel-mode driver only when its catalog is signed by the Microsoft Windows
// Hardware Compatibility Publisher -- the signature you get by submitting the
// driver to Microsoft's Partner Center. The driver FlipAi downloaded is signed
// by SignPath Foundation through GlobalSign, which is a perfectly valid
// Authenticode code-signing certificate and completely irrelevant to loading a
// driver. Its own project README says as much: it "requires test signing to be
// enabled".
//
// So Windows verified the package, added it to the driver store, created the
// device node, and then refused to start it -- CM_PROB_UNSIGNED_DRIVER, problem
// code 52. The only ways past that are turning on test-signing or turning off
// Secure Boot, which FlipAi will not do to somebody's PC to make a feature look
// like it works.
//
// What replaces it is a driver Microsoft has actually signed. VB-Audio's cables
// are free and ship a signature Windows accepts, so they install on a stock
// Windows 11 with Secure Boot left on. FlipAi points at them, waits, and wires
// both directions itself the moment the endpoints appear -- which is the part
// that always had to be automatic, and still is.

// voiceAudioSource is one free, Microsoft-signed way to get a virtual pair.
type voiceAudioSource struct {
	// Name is what the user will see on the vendor's page.
	Name string
	// URL is the vendor's own download page. FlipAi opens it rather than
	// fetching an installer itself: these are third-party packages with their
	// own licence screens and their own reboot, and quietly running someone
	// else's installer is not FlipAi's to do.
	URL string
	// Gives says what the PC ends up with, in the words Windows will use.
	Gives string
	// Why explains, in one line, what this one is for.
	Why string
}

// voiceAudioSources is the order FlipAi recommends them in, which is also the
// order planVoiceCables ranks them: the dedicated cable carries the caller to
// the agent, and VoiceMeeter's own pair carries the agent's voice back.
var voiceAudioSources = []voiceAudioSource{
	{
		Name:  "VB-CABLE Virtual Audio Device",
		URL:   "https://vb-audio.com/Cable/",
		Gives: "CABLE Input / CABLE Output",
		Why:   "carries the caller's voice to the desktop app",
	},
	{
		Name:  "VoiceMeeter",
		URL:   "https://vb-audio.com/Voicemeeter/",
		Gives: "VoiceMeeter Input / VoiceMeeter Output",
		Why:   "carries the app's reply back to the caller",
	},
}

// voiceAudioBridgeSetup is what is left to do, and why.
type voiceAudioBridgeSetup struct {
	// Done is true when two independent pairs are already installed.
	Done bool
	// Next is the source to install next, when one is needed.
	Next voiceAudioSource
	// Headline is the one sentence the status row shows.
	Headline string
	// Steps are the numbered instructions, ready to display.
	Steps []string
}

// planVoiceAudioBridge says what the PC still needs. It takes the cable plan
// rather than the raw device list so there is exactly one idea in the product of
// what counts as a usable pair.
func planVoiceAudioBridge(plan voiceCablePlan) voiceAudioBridgeSetup {
	have := len(plan.Cables)
	if plan.complete() {
		return voiceAudioBridgeSetup{
			Done:     true,
			Headline: "The audio bridge is ready. FlipAi wires both directions itself on every call.",
		}
	}

	// Which source to point at next depends on what is already there, not on
	// how many are there: someone who installed VoiceMeeter first should be
	// sent to the cable, not told to install VoiceMeeter again.
	next := voiceAudioSources[0]
	for _, source := range voiceAudioSources {
		if !cablePlanHasSource(plan, source) {
			next = source
			break
		}
	}

	headline := "FlipAi needs two free virtual audio pairs. Neither is installed yet."
	if have == 1 {
		headline = "One virtual audio pair is installed. FlipAi needs a second, independent one."
	}

	steps := []string{
		fmt.Sprintf("Open %s and download %s. It is free.", next.URL, next.Name),
		"Run the installer, accept the prompts, and restart the PC when it asks.",
		"Come back here. FlipAi finds the new endpoints and wires them itself.",
	}
	if have == 0 {
		steps = append(steps, fmt.Sprintf("Then do the same for %s (%s), which is the second pair.", voiceAudioSources[1].Name, voiceAudioSources[1].URL))
	}

	return voiceAudioBridgeSetup{Next: next, Headline: headline, Steps: steps}
}

// cablePlanHasSource reports whether a plan already includes this source's
// family. The families are the ones cableFamily() returns.
func cablePlanHasSource(plan voiceCablePlan, source voiceAudioSource) bool {
	want := "cable"
	if strings.Contains(strings.ToLower(source.Name), "voicemeeter") {
		want = "voicemeeter"
	}
	for _, family := range plan.Cables {
		if family == want || strings.HasPrefix(family, want+"-") {
			return true
		}
	}
	return false
}

// voiceAudioBridgeWhyNotAutomatic is the explanation FlipAi owes anyone who
// remembers the button that used to be here. It is shown where that button was.
const voiceAudioBridgeWhyNotAutomatic = "FlipAi used to install a driver for you here. It could not work: Windows only loads a virtual audio driver that Microsoft itself has signed, and the free driver FlipAi used is not one -- Windows rejected it with problem code 52. Turning that check off would mean switching on test-signing or turning off Secure Boot, which FlipAi will not do to your PC. The two below are free and are signed the way Windows requires, so they just install."
