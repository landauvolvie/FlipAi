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

// Electron controls routinely ignore UI Automation Invoke, so the driver must
// press the control with a real synthesized click, and try that before the
// pattern-based methods.
func TestTheVoiceScriptClicksTheControlForReal(t *testing.T) {
	script, err := voiceAgentUIAScript(0x1234, "start")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "mouse_event") || !strings.Contains(script, "SetCursorPos") {
		t.Error("the driver no longer performs a real pointer click")
	}
	clickAt := strings.Index(script, "$done = ClickElement $target")
	invokeAt := strings.Index(script, "InvokePattern")
	if clickAt < 0 {
		t.Fatal("the real click is not the primary action")
	}
	if invokeAt >= 0 && invokeAt < clickAt {
		t.Error("UI Automation Invoke is still tried before the real click")
	}
	// The voice control the ChatGPT desktop app actually offers -- "Start new
	// voice chat" and its headphone icon -- must be recognized.
	for _, want := range []string{"new\\s+voice\\s+chat", "headphone", "headset"} {
		if !strings.Contains(script, want) {
			t.Errorf("the control match does not recognize %q", want)
		}
	}
}

// The voice front-end for a call is the ChatGPT desktop app, which drives Codex;
// the standalone Codex app is only the fallback. Launching, the Start Menu
// shortcut and the window search must all prefer ChatGPT.
func TestChatGPTIsPreferredAsTheVoiceFrontEnd(t *testing.T) {
	if got := agentAppTitles("C"); got[0] != "ChatGPT" {
		t.Errorf("window search does not prefer ChatGPT: %v", got)
	}
	if got := agentAppShortcutNames("C"); got[0] != "ChatGPT" {
		t.Errorf("the shortcut search does not prefer ChatGPT: %v", got)
	}
	exes := agentAppExecutables("C", `C:\u\AppData\Local`, `C:\Program Files`, "")
	chatIdx, codexIdx := -1, -1
	for i, p := range exes {
		lp := strings.ToLower(p)
		if chatIdx < 0 && strings.Contains(lp, "chatgpt.exe") {
			chatIdx = i
		}
		if codexIdx < 0 && strings.Contains(lp, "codex.exe") {
			codexIdx = i
		}
	}
	if chatIdx < 0 || codexIdx < 0 || chatIdx > codexIdx {
		t.Errorf("ChatGPT is not launched before the standalone Codex app: chat=%d codex=%d", chatIdx, codexIdx)
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
	// On failure it reads back at the same slot, so the error can tell a moved
	// vtable slot (read also fails) apart from a rejected device id (read works).
	if !strings.Contains(routeAppAudioPS, "ProbeSlot") || !strings.Contains(routeAppAudioPS, "GetPersistedDefaultAudioEndpoint") {
		t.Error("the router no longer reads back to diagnose why Windows refused it")
	}
	// The endpoint lookup is still by friendly name, wired through the fallback.
	if !strings.Contains(routeAppAudioPS, "PersistEither(factory, processId, 0, FindEndpointId(0, renderName))") {
		t.Error("the playback endpoint is no longer routed through the fallback")
	}
	if !strings.Contains(routeAppAudioPS, "PersistEither(factory, processId, 1, FindEndpointId(1, captureName))") {
		t.Error("the recording endpoint is no longer routed through the fallback")
	}
}
