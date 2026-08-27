package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are architecture tests. They cannot ring a phone, but they can hold the
// two properties that the real-world failures came down to:
//
//   - Google Voice is FlipAi's own browser view and nothing else. The previous
//     version started Microsoft Edge as a separate application and then tried
//     to move its windows around, which is where "a browser window keeps
//     appearing", "a second Edge window opened" and "Google Voice is not where
//     it should be" all came from.
//   - only one place in the program decides that a call is in progress.

func goSources(t *testing.T) map[string]string {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	return out
}

// Nothing in FlipAi may start an external browser for Google Voice. Edge is
// still the engine -- WebView2 is Edge -- but it is embedded in a window FlipAi
// created, not an application FlipAi launched.
func TestNothingLaunchesAnExternalBrowserForGoogleVoice(t *testing.T) {
	for name, body := range goSources(t) {
		for _, forbidden := range []string{"msedge.exe", "chrome.exe", "--user-data-dir=", "--app=http"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s starts an external browser (%q); Google Voice must live in FlipAi's own WebView2 view", name, forbidden)
			}
		}
	}
}

// The Google Voice view is created with the same runtime the FlipAi window
// uses, with FlipAi's anti-throttling switches and its own loopback control
// port.
func TestGoogleVoiceViewIsFlipAiOwned(t *testing.T) {
	body := goSources(t)["voice_receiver_windows.go"]
	if body == "" {
		t.Fatal("the Google Voice receiver is gone")
	}
	for _, want := range []string{
		"createGoogleVoiceWebView(dataDir)",
		"googleVoiceInitScript",
		"webViewVoicePermissions",
		"dock.park()",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the receiver no longer does %s", want)
		}
	}
	// The window must never be brought to the front: there is nowhere for it
	// to be except inside the FlipAi panel.
	for _, forbidden := range []string{"bringToFront", "voiceSWRestore"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the receiver still uses %s, which would put Google Voice on the desktop", forbidden)
		}
	}
}

// The DevTools channel opens no port at all. An earlier version asked WebView2
// for a loopback debugging port, which the runtime turns out to ignore -- so
// the channel silently did not exist, and with it the second way of pressing
// Answer and the ability to send an image. It is reached in-process now, and
// nothing may put a debugging port back.
func TestTheControlChannelOpensNoDebuggingPort(t *testing.T) {
	for name, body := range goSources(t) {
		for _, forbidden := range []string{"--remote-debugging-port", "--remote-allow-origins"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s asks WebView2 for %s; the DevTools channel is in-process", name, forbidden)
			}
		}
	}
	body := goSources(t)["voice_cdp_windows.go"]
	if !strings.Contains(body, "CallDevToolsProtocolMethod") {
		t.Fatal("the control channel no longer uses WebView2's own in-process DevTools call")
	}
	// Every WebView2 call has to be made on the thread that created it.
	if !strings.Contains(body, "d.view.Dispatch(") {
		t.Error("the control channel calls WebView2 off its own thread")
	}
	if !strings.Contains(body, "voiceDevToolsTimeout") {
		t.Error("a DevTools call that never answers would stall the observation loop")
	}
}

// The vendored WebView2 binding carries the local change that makes the
// in-process channel possible.
func TestTheWebViewBindingCanCallDevTools(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("third_party", "go-webview2", "pkg", "edge", "chromium.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "func (e *Chromium) CallDevToolsProtocolMethod(") {
		t.Fatal("the WebView2 binding lost the local change that reaches DevTools in-process")
	}
}

// The one thing the Google Voice process does listen on is FlipAi's own
// endpoint, so the host can ask it to send an image through the signed-in
// session it owns. It must be loopback-only and must refuse anything that does
// not carry FlipAi's local token.
func TestTheGoogleVoiceEndpointIsLocalAndAuthenticated(t *testing.T) {
	body := goSources(t)["voice_receiver_windows.go"]
	i := strings.Index(body, "func startVoiceWindowEndpoint(")
	if i < 0 {
		t.Fatal("the Google Voice process serves no endpoint, so an image can never be sent")
	}
	endpoint := body[i:]
	if !strings.Contains(endpoint, `net.Listen("tcp", "127.0.0.1:0")`) {
		t.Error("the Google Voice endpoint is not bound to loopback on a free port")
	}
	if !strings.Contains(endpoint, `r.Header.Get("X-FlipAi-Token") == token`) {
		t.Error("the Google Voice endpoint does not check its token")
	}
	if !strings.Contains(endpoint, `return token != "" &&`) {
		t.Error("an endpoint with no token to check must refuse rather than allow")
	}
}

// That token is what stands between anything else on this machine and a
// signed-in Google Voice session, so the desktop UI must never be handed it.
func TestTheGoogleVoiceEndpointTokenIsNeverServedToAPage(t *testing.T) {
	dir := t.TempDir()
	mutateVoiceRuntime(dir, func(s *VoiceRuntimeState) {
		s.ControlPort = 5555
		s.ControlToken = "a-secret-nobody-else-may-have"
	})
	snap := voiceSnapshot(dir, func() Config { return Config{} })
	if snap.Runtime.ControlToken != "" {
		t.Fatalf("the status a page reads carries the endpoint token: %q", snap.Runtime.ControlToken)
	}
	if snap.Runtime.ControlPort != 5555 {
		t.Errorf("the port was stripped along with the token: %d", snap.Runtime.ControlPort)
	}
	if got := loadVoiceRuntime(dir).ControlToken; got == "" {
		t.Error("stripping the token for the page also removed it from disk")
	}
}

// The answer ladder must really be a ladder: three different mechanisms, not
// the same one three times. Rung 1 is the page's own click, rung 2 a real
// pointer press through the browser's input pipeline, rung 3 the Windows
// accessibility Invoke.
func TestTheAnswerLadderHasThreeDistinctRungs(t *testing.T) {
	if voiceAnswerLadder != 3 {
		t.Fatalf("the ladder claims %d rungs", voiceAnswerLadder)
	}
	body := goSources(t)["voice_receiver_windows.go"]
	i := strings.Index(body, "func (c *voiceControlChannel) pressAnswer(")
	if i < 0 {
		t.Fatal("pressAnswer is gone")
	}
	end := strings.Index(body[i:], "\n}\n")
	press := body[i : i+end]
	for rung, want := range map[int]string{
		1: "voiceClickAnswerScripted",
		2: "voiceClickAnswerTrusted",
		3: "invokeGoogleVoiceAnswerAccessibly",
	} {
		if !strings.Contains(press, want) {
			t.Errorf("rung %d (%s) is missing from the answer ladder", rung, want)
		}
	}
}

// A caller hears a working conversation only when a voice session is actually
// running, so exactly one place in the program may write that fact down.
func TestOnlyTheBridgeDecidesThatACallIsInProgress(t *testing.T) {
	for name, body := range goSources(t) {
		if name == "voice_call.go" {
			continue
		}
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "s.InCall =") || strings.HasPrefix(trimmed, "s.CallPhase =") {
				t.Errorf("%s writes the call state directly (%q); only the bridge may", name, trimmed)
			}
		}
	}
	// And in voice_call.go it is only ever written by writeCallState.
	body := goSources(t)["voice_call.go"]
	i := strings.Index(body, "func (b *voiceBridge) writeCallState(")
	if i < 0 {
		t.Fatal("writeCallState is gone")
	}
	end := strings.Index(body[i:], "\n}\n")
	rest := body[:i] + body[i+end:]
	if strings.Contains(rest, "s.InCall = ") {
		t.Error("something other than writeCallState sets the in-call flag")
	}
}

// The guards that used to press Answer and start voice mode on their own
// schedules are gone. Two of them pressing the same control is how a call could
// be answered twice and how voice mode could be toggled straight back off.
func TestTheCompetingCallGuardsAreGone(t *testing.T) {
	for _, gone := range []string{
		"aaa_voice_answer_windows.go",
		"aab_voice_receiver_watchdog_windows.go",
		"aac_agent_voice_guard_windows.go",
		"voice_edge_windows.go",
	} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s is back; there must be one call state machine, not several", gone)
		}
	}
	for name, body := range goSources(t) {
		if strings.Contains(body, "runGoogleVoiceReceiverWatchdog") || strings.Contains(body, "runAgentVoiceActivationGuard") {
			t.Errorf("%s still runs a competing voice guard", name)
		}
	}
}

// The binding shows its window, and gives it focus, before it embeds the
// browser -- and embedding takes seconds while WebView2 starts. If the Google
// Voice view is created any other way, an empty titled window appears on the
// user's desktop at every start and takes focus from whatever they are doing.
// That is the complaint this whole change exists to answer, so the creation
// options are held here.
func TestTheGoogleVoiceViewIsCreatedAlreadyOutOfSight(t *testing.T) {
	body := goSources(t)["voice_call_windows.go"]
	i := strings.Index(body, "func createGoogleVoiceWebView(")
	if i < 0 {
		t.Fatal("createGoogleVoiceWebView is gone")
	}
	end := strings.Index(body[i:], "\n}\n")
	create := body[i : i+end]
	for _, want := range []string{
		"parkedWindowOrigin()",
		"Position:   true",
		"NoActivate: true",
		"wsExToolWin | wsExNoActivate",
	} {
		if !strings.Contains(create, want) {
			t.Errorf("the Google Voice view is created without %s, so it can appear on the desktop", want)
		}
	}
	if strings.Contains(create, "Center: true") {
		t.Error("the Google Voice view is still created in the middle of the screen")
	}
	if strings.Contains(create, "AutoFocus: true") {
		t.Error("the parked Google Voice view would take focus from the user")
	}
}

// The vendored WebView2 binding carries a local change for the above. A
// dependency update that dropped it would silently bring the flashing window
// back, so the change is asserted rather than assumed.
func TestTheWebViewBindingCanCreateAWindowOutOfSight(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("third_party", "go-webview2", "webview.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(b), "\r\n", "\n")
	for _, want := range []string{
		"NoActivate bool",
		"ExStyle    uint32",
		"if opts.Position {",
		"uintptr(opts.ExStyle),",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the WebView2 binding lost the local change that lets FlipAi create a window out of sight (%s)", want)
		}
	}
}
