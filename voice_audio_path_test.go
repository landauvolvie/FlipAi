package main

import (
	"strings"
	"testing"
	"time"
)

// A phone conversation needs two independent one-way paths, and each of them is
// a virtual cable. These tests are about the property that makes the call
// audible rather than about which cable package is installed: the caller's
// voice and the agent's voice must never travel on the same wire, and neither
// end must ever ask the user to choose a device.

func twoCables() []VoiceAudioDevice {
	return []VoiceAudioDevice{
		{Kind: "audiooutput", DeviceID: "r1", Label: "CABLE-A Input (VB-Audio Cable A)"},
		{Kind: "audioinput", DeviceID: "c1", Label: "CABLE-A Output (VB-Audio Cable A)"},
		{Kind: "audiooutput", DeviceID: "r2", Label: "CABLE-B Input (VB-Audio Cable B)"},
		{Kind: "audioinput", DeviceID: "c2", Label: "CABLE-B Output (VB-Audio Cable B)"},
	}
}

// The caller hears the agent and the agent hears the caller only if the two
// directions are separate. Sharing one cable makes each side hear itself.
func TestTheTwoDirectionsNeverShareACable(t *testing.T) {
	plan := planVoiceCables(twoCables())
	if !plan.complete() {
		t.Fatalf("two installed cables did not produce a complete plan: %+v", plan)
	}
	if plan.Warning != "" {
		t.Errorf("a fully wired machine warned: %q", plan.Warning)
	}
	// Caller -> agent goes out of Google Voice's speaker and in at the agent's
	// microphone; agent -> caller is the other cable, the other way round.
	if strings.EqualFold(plan.GoogleVoiceOutput, plan.AgentOutput) {
		t.Error("both sides play into the same endpoint, so the agent would hear itself")
	}
	if strings.EqualFold(plan.AgentInput, plan.GoogleVoiceInput) {
		t.Error("both sides record from the same endpoint, so the caller would hear themselves")
	}
	if len(plan.Cables) != 2 || plan.Cables[0] == plan.Cables[1] {
		t.Errorf("the plan does not use two distinct cable families: %v", plan.Cables)
	}
}

// Hand-edited overrides are the escape hatch for an unrecognized cable, and
// they must not be able to wire a call to silence without saying so.
func TestOverridesThatCollapseTheTwoDirectionsAreCalledOut(t *testing.T) {
	devices := twoCables()
	cfg := defaultVoiceCallConfig()
	cfg.AgentOutput = "CABLE-A Input (VB-Audio Cable A)" // the same wire Google Voice plays into
	plan := applyCableOverrides(planVoiceCables(devices), cfg, devices)
	if plan.Warning == "" {
		t.Fatal("overrides that put both sides on one wire produced no warning")
	}
	if !strings.Contains(plan.Warning, "hear itself") {
		t.Errorf("the warning does not say what would go wrong: %q", plan.Warning)
	}
}

// Nobody picks a device. The Google Voice side is pinned inside the page and
// the desktop app side is written into the Windows per-app store, both without
// a picker anywhere.
func TestBothEndsOfTheAudioPathArePinnedAutomatically(t *testing.T) {
	// Google Voice's speaker: every media element the page plays through is
	// pointed at the cable.
	if !strings.Contains(googleVoiceInitScript, "setSinkId") {
		t.Error("the page no longer pins Google Voice's speaker to the cable")
	}
	// Google Voice's microphone: the call's own getUserMedia request is
	// rewritten to the cable rather than the PC's real microphone.
	if !strings.Contains(googleVoiceInitScript, "deviceId = {exact: id}") &&
		!strings.Contains(googleVoiceInitScript, "a.deviceId = {exact: id}") {
		t.Error("the page no longer forces Google Voice's microphone onto the cable")
	}
	// Endpoint names are hidden from a page with no microphone grant, so the
	// page opens the microphone once at startup to reveal them. Without that
	// the cable detection sees a list of blanks and reports no cables at all.
	if !strings.Contains(googleVoiceInitScript, "primeDevices") {
		t.Error("the page no longer reveals endpoint names before the first call")
	}
	// The desktop app's own microphone and speaker are written per-process into
	// the store Windows' own Settings app writes.
	if !strings.Contains(routeAppAudioPS, "SetPersistedDefaultAudioEndpoint") {
		t.Error("the desktop app's audio is no longer pinned per-application")
	}
}

// Wiring the desktop app after its voice session has already opened a stream is
// too late: Windows hands a process the endpoint it had when the stream opened.
func TestTheDesktopAppIsWiredBeforeItsVoiceSessionStarts(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	effects := m.Observe(voiceObservation{InCall: true, Caller: "5551234567"}, time.Now())
	routeAt, startAt := -1, -1
	for i, e := range effects {
		switch e.Kind {
		case voiceEffectRouteAudio:
			routeAt = i
		case voiceEffectStartAgentVoice:
			startAt = i
		}
	}
	if routeAt < 0 || startAt < 0 {
		t.Fatalf("answering a call did not both wire and start the agent: %v", kinds(effects))
	}
	if routeAt > startAt {
		t.Error("the desktop app's voice session is started before its audio is pointed at the cables")
	}
}

// A ringing call wires the desktop app while the phone is still ringing, so
// nothing has to happen between the caller being answered and being heard.
func TestRingingAlreadyWiresTheDesktopApp(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	if !hasKind(m.Observe(voiceObservation{Answer: true, Caller: "5551234567"}, time.Now()), voiceEffectRouteAudio) {
		t.Fatal("an authorized ring did not start wiring the desktop app")
	}
}

// Google Voice decides whether a browser can take calls partly from what the
// browser can do, and WebView2 does not always expose the Notifications API. A
// page that finds it missing can decline to ring here at all -- which looks,
// from outside, exactly like FlipAi failing to notice calls. So it is supplied
// when it is absent, and never when it is present.
func TestTheNotificationsAPIIsSuppliedOnlyWhenMissing(t *testing.T) {
	if !strings.Contains(googleVoiceInitScript, "typeof window.Notification === 'undefined'") {
		t.Fatal("the page no longer supplies the Notifications API to a browser that lacks it")
	}
	if !strings.Contains(googleVoiceInitScript, "notifications supplied by FlipAi") {
		t.Error("a browser that had to be given the Notifications API does not say so on the status page")
	}
	// The shim must answer the two questions a page asks before it decides a
	// browser can notify: what the permission is, and how to request it.
	for _, want := range []string{"Shim.requestPermission", "get: () => 'granted'"} {
		if !strings.Contains(googleVoiceInitScript, want) {
			t.Errorf("the supplied Notifications API is missing %s", want)
		}
	}
	// A real implementation is wrapped for the ring hint, never replaced.
	if !strings.Contains(googleVoiceInitScript, "const RealNotification = window.Notification;") {
		t.Error("the page no longer keeps a real Notifications implementation")
	}
}
