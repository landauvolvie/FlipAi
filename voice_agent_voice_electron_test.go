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
	// The scan ends only when a real voice control (or a live session) is found,
	// or the deadline passes -- never on element count, which the native window
	// frame alone exceeds.
	if !strings.Contains(script, "if ($active -or $null -ne $startEl) { break }") {
		t.Error("the scan no longer waits for an actual voice control before ending")
	}
	// And the wait is bounded, so a genuinely empty app still reports back.
	if !strings.Contains(script, "AddSeconds(12)") {
		t.Error("the accessibility wait is unbounded")
	}
	// The report keys the Go side parses must survive the rewrite.
	for _, key := range []string{"found=1", "active=", "start=", "end=", "control="} {
		if !strings.Contains(script, key) {
			t.Errorf("the rewritten script no longer emits %q", key)
		}
	}
}

// The v0.45 field test proved that a successful mouse_event call is not proof
// that Electron handled the control: FlipAi reported it pressed "Start voice
// chat" while ChatGPT stayed in text mode. v0.46 therefore keeps every input
// mechanism but executes them as separate attempts, UIA Invoke first, and lets
// Go verify that live Voice became active before another method is tried.
func TestTheVoiceScriptUsesSeparateVerifiedActivationMethods(t *testing.T) {
	invoke, err := voiceAgentUIAScript(0x1234, "start-invoke")
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := voiceAgentUIAScript(0x1234, "start-pointer")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pointer, "mouse_event") || !strings.Contains(pointer, "SetCursorPos") {
		t.Error("the pointer fallback was removed")
	}
	if !strings.Contains(invoke, "InvokeElement $target") || !strings.Contains(invoke, "invoke-sent") {
		t.Error("UI Automation Invoke is not a distinct Voice activation attempt")
	}
	if !strings.Contains(pointer, "PointerClickElement $target") || !strings.Contains(pointer, "pointer-sent") {
		t.Error("the real pointer press is not a distinct Voice activation attempt")
	}
	// The Voice control the ChatGPT desktop app actually offers -- "Start new
	// voice chat" / "Start voice chat" and its headphone icon -- must remain
	// recognized regardless of which activation method is being attempted.
	for _, want := range []string{"new\\s+voice\\s+chat", "headphone", "headset"} {
		if !strings.Contains(invoke, want) {
			t.Errorf("the control match does not recognize %q", want)
		}
	}
	// It must not bail on native window chrome. The old count-based early break
	// stopped as soon as the tree had more than a handful of elements -- which
	// the title bar alone exceeds -- so Chromium never got time to build its web
	// tree, and the scan saw only [App, Minimize, Maximize, Close].
	if strings.Contains(invoke, "$all.Count -gt 4") {
		t.Error("the scan still bails on native window chrome before the web tree builds")
	}
	// Only a clickable control can be the voice control. A conversation title
	// ("Voice Chat Topic Summary") is a list/text item and must be excluded.
	if !strings.Contains(invoke, "ControlType.Id") || !strings.Contains(invoke, "$clickable") {
		t.Error("the scan no longer filters out non-clickable items like conversation titles")
	}
}

// The signature of a Chromium app with accessibility off is a scan that returns
// only the window frame. FlipAi has to name that specifically, because the fix
// (reopen the app so it starts with accessibility on) is different from every
// other reason a voice control is missing.
func TestChromeOnlyAccessibilityIsCalledOutSpecifically(t *testing.T) {
	chromeOnly := agentVoiceState{Found: true, Controls: []string{"Claude", "Minimize", "Maximize", "Close"}}
	err := agentVoiceStartFailure("Claude", chromeOnly)
	if err == nil || !strings.Contains(err.Error(), "accessibility off") {
		t.Fatalf("a window exposing only its frame was not diagnosed as accessibility-off: %v", err)
	}
	if !strings.Contains(err.Error(), "reopen") && !strings.Contains(err.Error(), "reopen it") {
		t.Errorf("the message does not tell the user to reopen the app: %v", err)
	}

	// A window that exposed real content is a different, ordinary "no voice
	// control found" -- not the accessibility-off case.
	withContent := agentVoiceState{Found: true, Controls: []string{"New chat", "Send", "Settings"}}
	if err := agentVoiceStartFailure("ChatGPT", withContent); err == nil || strings.Contains(err.Error(), "accessibility off") {
		t.Errorf("a window with real content was mislabelled as accessibility-off: %v", err)
	}

	// The helper itself: the app's own title counts as frame, real content does not.
	if !onlyWindowChrome("Claude", []string{"Claude", "Minimize", "Maximize", "Close"}) {
		t.Error("the classic chrome-only set was not recognized")
	}
	if onlyWindowChrome("ChatGPT", []string{"ChatGPT", "Start new voice chat", "Close"}) {
		t.Error("a window exposing a real control was called chrome-only")
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
// from the field. Both the SWD wrapper and the raw MMDevice id must still be
// tried, now for every live process in the Electron tree rather than only the
// top-level window-owner PID.
func TestTheAudioRouterTriesBothEndpointIdFormsForElectronProcessTree(t *testing.T) {
	if !strings.Contains(routeAppAudioPS, "PersistEither") {
		t.Fatal("the router no longer has a two-format fallback")
	}
	if !strings.Contains(routeAppAudioPS, "Persist(factory, processId, flow, SwdId(flow, mmDeviceId))") {
		t.Error("the router no longer tries the SWD device-path form")
	}
	if !strings.Contains(routeAppAudioPS, "Persist(factory, processId, flow, mmDeviceId)") {
		t.Error("the router no longer falls back to the raw MMDevice id")
	}
	// v0.45's read-back probe returned E_INVALIDARG too. Comparing against
	// EarTrumpet showed the COM layout was already correct, so v0.46 must not
	// turn that read-back into a false vtable diagnosis again.
	if strings.Contains(routeAppAudioPS, "ProbeSlot") || strings.Contains(routeAppAudioPS, "read-back HRESULT") {
		t.Error("the disproven read-back/vtable diagnostic returned")
	}
	// The endpoint lookup is still by friendly name, but the resulting MMDevice
	// id is applied across the whole Electron process tree. This reaches the
	// child/utility PID that owns the actual audio session after Voice starts.
	if !strings.Contains(routeAppAudioPS, "PersistProcessTree(factory, processId, 0, FindEndpointId(0, renderName)") {
		t.Error("the playback endpoint is not routed through the process-tree fallback")
	}
	if !strings.Contains(routeAppAudioPS, "PersistProcessTree(factory, processId, 1, FindEndpointId(1, captureName)") {
		t.Error("the recording endpoint is not routed through the process-tree fallback")
	}
	if !strings.Contains(routeAppAudioPS, "CandidateProcessIds(rootProcessId)") {
		t.Error("the router no longer reaches Electron child processes")
	}
}
