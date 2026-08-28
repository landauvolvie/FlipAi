package main

import (
	"strings"
	"testing"
)

// The Codex and ChatGPT desktop apps are Chromium/Electron, and Chromium builds
// its UI Automation tree only after a client attaches, and not instantly. A
// single quick query -- a fresh client that asks once and exits -- sees only the
// top-level window, which is why voice mode never started and the report read
// "it offered: ChatGPT" and nothing else. The driver script must keep one client
// alive and re-scan until the web content appears.
func TestTheVoiceScriptWaitsForTheElectronAccessibilityTree(t *testing.T) {
	script, err := voiceAgentUIAScript(0x1234, "start")
	if err != nil {
		t.Fatal(err)
	}
	// It re-scans in a loop rather than querying once.
	if !strings.Contains(script, "while ($true)") || !strings.Contains(script, "Start-Sleep") {
		t.Error("the driver script no longer waits for the Electron tree to populate")
	}
	// A single FindAll that returned only the window used to end the search; now
	// a sparse tree keeps it waiting until the deadline.
	if !strings.Contains(script, "$all.Count -gt 4") {
		t.Error("the script no longer treats a bare window as a not-yet-built tree")
	}
	// And the wait is bounded, so a genuinely empty app still reports back.
	if !strings.Contains(script, "AddSeconds(9)") {
		t.Error("the accessibility wait is unbounded")
	}
	// The report keys the Go side parses must survive the rewrite.
	for _, key := range []string{"found=1", "active=", "start=", "end=", "control="} {
		if !strings.Contains(script, key) {
			t.Errorf("the rewritten script no longer emits %q", key)
		}
	}
}

// Which id format SetPersistedDefaultAudioEndpoint accepts differs across
// Windows builds; guessing one and failing is the "Windows refused it" reported
// from the field. Both the SWD wrapper and the raw MMDevice id must be tried.
func TestTheAudioRouterTriesBothEndpointIdForms(t *testing.T) {
	if !strings.Contains(routeAppAudioPS, "PersistEither") {
		t.Fatal("the router no longer has a two-format fallback")
	}
	// It tries the SWD wrapper and the raw id, in that order, and only fails
	// when neither is accepted.
	if !strings.Contains(routeAppAudioPS, "Persist(factory, processId, flow, SwdId(flow, mmDeviceId))") {
		t.Error("the router no longer tries the SWD device-path form")
	}
	if !strings.Contains(routeAppAudioPS, "Persist(factory, processId, flow, mmDeviceId)") {
		t.Error("the router no longer falls back to the raw MMDevice id")
	}
	// The endpoint lookup is still by friendly name, wired through the fallback.
	if !strings.Contains(routeAppAudioPS, "PersistEither(factory, processId, 0, FindEndpointId(0, renderName))") {
		t.Error("the playback endpoint is no longer routed through the fallback")
	}
	if !strings.Contains(routeAppAudioPS, "PersistEither(factory, processId, 1, FindEndpointId(1, captureName))") {
		t.Error("the recording endpoint is no longer routed through the fallback")
	}
}
