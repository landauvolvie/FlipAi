package main

import (
	"strings"
	"testing"
)

// v0.44 reached the ChatGPT window on a real call but did not enter live Voice.
// The old matcher accepted the first generic voice/microphone/dictation control
// it saw, so opening text chat was enough to steal the click from the actual
// "Start new voice chat" control. Keep live Voice ranked and dictation out.
func TestV044RanksLiveVoiceAboveTextInputControls(t *testing.T) {
	script, err := voiceAgentUIAScript(0x1234, "start")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "$startScore") || !strings.Contains(script, "$score = 100") {
		t.Fatal("the voice driver no longer ranks candidates to prefer the actual live-Voice control")
	}
	if !strings.Contains(script, `new\s+voice\s+chat`) {
		t.Error("the highest-priority match no longer includes Start new voice chat")
	}
	for _, bad := range []string{`\bmicrophone\b`, `\bmic\b`, `\bdictat`} {
		if strings.Contains(script, bad) {
			t.Errorf("text-input control %q is still accepted as a live-Voice start candidate", bad)
		}
	}
	// A standalone word "voice" is too broad: clickable conversation/navigation
	// chrome can carry it. Live Voice must have context such as chat/mode/start.
	if strings.Contains(script, `|\bvoice\b|`) {
		t.Error("the matcher still accepts any clickable element containing only the word voice")
	}
}

// The Windows AudioPolicyConfig ABI takes the device id as a native HSTRING
// handle. v0.44 used CLR string marshaling for Set, which differs from the
// working Windows/EarTrumpet ABI and can make Windows reject the route even
// though the same factory can be read successfully.
func TestV044AudioRouterUsesNativeHStringABI(t *testing.T) {
	if !strings.Contains(routeAppAudioPS, "IntPtr deviceId") {
		t.Fatal("SetPersistedDefaultAudioEndpoint is not using the native HSTRING pointer ABI")
	}
	if strings.Contains(routeAppAudioPS, `[MarshalAs(UnmanagedType.HString)] string deviceId`) {
		t.Error("the broken CLR-string SetPersistedDefaultAudioEndpoint signature returned")
	}
	for _, want := range []string{"WindowsCreateString", "WindowsDeleteString", "SetOne(factory, processId, flow, role, hstring)"} {
		if !strings.Contains(routeAppAudioPS, want) {
			t.Errorf("the audio router is missing %q", want)
		}
	}
	// Console + Multimedia are the roles used by the working per-app routing
	// implementation. Communications is attempted only as best effort, so a
	// build that rejects that optional role cannot turn successful routing into
	// the old misleading "Windows refused it" state.
	if !strings.Contains(routeAppAudioPS, `new[] { 1 /*Multimedia*/, 0 /*Console*/ }`) {
		t.Error("the required persisted audio roles changed")
	}
	if !strings.Contains(routeAppAudioPS, `2 /*Communications*/`) {
		t.Error("voice-app Communications routing is no longer attempted")
	}
}
