package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// This is the closest thing to a real call this code can be given without a
// phone line. The production injection script runs in headless Chromium against
// a stand-in Google Voice page, its window.flipVoice* bindings are wired to the
// real voiceBridge, and Chromium's fake audio endpoints stand in for the
// virtual cable ends FlipAi chooses on a real PC. The microphone capture and
// the media-element sink are genuinely applied by the browser, so the routing
// assertions below are real rather than mocked.
//
// What it still cannot cover: Google's own markup, WebView2, the telephony
// itself, and whether the desktop AI app enters voice mode.

const playwrightModule = "/opt/node22/lib/node_modules/playwright/index.mjs"

type harnessScenario struct {
	bridge  *voiceBridge
	dataDir string

	mu            sync.Mutex
	activations   []string
	deactivations []string
}

func (s *harnessScenario) recorded() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.activations...), append([]string(nil), s.deactivations...)
}

type callHarness struct {
	t    *testing.T
	root string

	mu        sync.Mutex
	scenarios map[string]*harnessScenario
}

func (h *callHarness) scenario(name string) *harnessScenario {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.scenarios[name]
}

// harnessConfig is what the driver sends: how the call bridge is set up, and
// the agents that decide who is allowed to call.
type harnessConfig struct {
	Voice  VoiceCallConfig `json:"voice"`
	Agents struct {
		DefaultAgent string        `json:"defaultAgent"`
		Codex        AgentSettings `json:"codex"`
		Claude       AgentSettings `json:"claude"`
	} `json:"agents"`
}

func (h *callHarness) configure(name string, in harnessConfig) error {
	dataDir := filepath.Join(h.root, name)
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	if err := saveVoiceCallConfig(dataDir, in.Voice); err != nil {
		return err
	}
	main := defaultConfig(dataDir)
	main.DefaultAgent = in.Agents.DefaultAgent
	main.Codex.AgentSettings = in.Agents.Codex
	main.Claude.AgentSettings = in.Agents.Claude
	if err := normalizeAgents(&main); err != nil {
		return err
	}
	s := &harnessScenario{dataDir: dataDir}
	s.bridge = newVoiceBridge(dataDir, func() Config { return main },
		func(_ VoiceCallConfig, agent string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.activations = append(s.activations, agent)
			return nil
		},
		func(_ VoiceCallConfig, agent string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.deactivations = append(s.deactivations, agent)
			return nil
		})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.scenarios[name] = s
	return nil
}

// shimHandler is the plain-HTTP side the driver script talks to. It is not
// reachable from the browser page; only the Node process calls it.
func (h *callHarness) shimHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/flipai-init.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(googleVoiceInitScript))
	})
	// The control channel's own scripts, served the same way and for the same
	// reason: the browser runs the exact strings the Windows app evaluates
	// through WebView2's DevTools channel, with no copy to drift out of date.
	mux.HandleFunc("/flipai-probe.json", func(w http.ResponseWriter, r *http.Request) {
		writeResult(w, map[string]string{
			"snapshot": voicePageSnapshotJS,
			"click":    voiceClickAnswerJS,
			"point":    voiceAnswerPointJS,
		})
	})
	mux.HandleFunc("/configure", func(w http.ResponseWriter, r *http.Request) {
		var cfg harnessConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.configure(r.Header.Get("X-FlipAi-Scenario"), cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeResult(w, nil)
	})
	call := func(fn func(*voiceBridge, map[string]any) any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s := h.scenario(r.Header.Get("X-FlipAi-Scenario"))
			if s == nil {
				http.Error(w, "unknown scenario", http.StatusBadRequest)
				return
			}
			params := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&params)
			writeResult(w, fn(s.bridge, params))
		}
	}
	str := func(p map[string]any, k string) string {
		v, _ := p[k].(string)
		return v
	}
	mux.HandleFunc("/audio-settings", call(func(b *voiceBridge, _ map[string]any) any { return b.AudioSettings() }))
	mux.HandleFunc("/incoming", call(func(b *voiceBridge, p map[string]any) any {
		return b.Incoming(str(p, "number"), str(p, "label"))
	}))
	mux.HandleFunc("/answered", call(func(b *voiceBridge, p map[string]any) any {
		return b.Answered(str(p, "number"), str(p, "label"))
	}))
	mux.HandleFunc("/ended", call(func(b *voiceBridge, _ map[string]any) any { b.Ended(); return nil }))
	mux.HandleFunc("/devices", call(func(b *voiceBridge, p map[string]any) any { b.Devices(str(p, "raw")); return nil }))
	mux.HandleFunc("/page", call(func(b *voiceBridge, p map[string]any) any {
		signedIn, _ := p["signedIn"].(bool)
		b.Page(str(p, "href"), signedIn, str(p, "controls"))
		return nil
	}))
	return mux
}

func writeResult(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"result": v})
}

type driverReport struct {
	Scenarios []struct {
		Name   string `json:"name"`
		Errors []string
		Calls  []struct {
			Method string `json:"method"`
			Args   []any  `json:"args"`
		} `json:"calls"`
		Devices []struct {
			Kind     string `json:"kind"`
			DeviceID string `json:"deviceId"`
			Label    string `json:"label"`
		} `json:"devices"`
		Answered     bool              `json:"answered"`
		PollTimers   int               `json:"pollTimers"`
		Capabilities map[string]string `json:"capabilities"`
		Observed     observedPage      `json:"observed"`
		MidCall      observedPage      `json:"midCall"`
		Probe        probedPage        `json:"probe"`
	} `json:"scenarios"`
}

// probedPage is what the control channel saw and did, decoded into the very
// types the product decodes into. The scripts are the product's own strings and
// the structs are the product's own structs, so a script whose shape drifts
// away from what FlipAi expects fails here rather than on a Windows runner.
type probedPage struct {
	Ringing   voicePageSnapshot `json:"ringing"`
	Point     answerPoint       `json:"point"`
	Clicked   bool              `json:"clicked"`
	Live      voicePageSnapshot `json:"live"`
	LivePoint answerPoint       `json:"livePoint"`
	Idle      voicePageSnapshot `json:"idle"`
}

type observedPage struct {
	GumCalls        []any          `json:"gumCalls"`
	GumSettings     map[string]any `json:"gumSettings"`
	GumError        string         `json:"gumError"`
	NoteError       string         `json:"noteError"`
	PlayCalled      bool           `json:"playCalled"`
	PlayError       string         `json:"playError"`
	SinkID          string         `json:"sinkId"`
	SecureContext   bool           `json:"secureContext"`
	HasMediaDevices bool           `json:"hasMediaDevices"`
	HasSetSinkID    bool           `json:"hasSetSinkId"`
}

func (r driverReport) find(t *testing.T, name string) int {
	t.Helper()
	for i, s := range r.Scenarios {
		if s.Name == name {
			return i
		}
	}
	t.Fatalf("driver did not report scenario %q", name)
	return -1
}

func (r driverReport) countCalls(idx int, method string) int {
	n := 0
	for _, c := range r.Scenarios[idx].Calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func deviceIDByLabel(t *testing.T, report driverReport, idx int, kind, label string) string {
	t.Helper()
	for _, d := range report.Scenarios[idx].Devices {
		if d.Kind == kind && d.Label == label {
			return d.DeviceID
		}
	}
	t.Fatalf("headless Chromium did not expose a %s named %q; devices=%+v", kind, label, report.Scenarios[idx].Devices)
	return ""
}

func TestGoogleVoiceCallFlowInRealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser call-flow harness is skipped in -short mode")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available; skipping the browser call-flow harness")
	}
	if _, err := os.Stat(playwrightModule); err != nil {
		t.Skip("playwright is not installed; skipping the browser call-flow harness")
	}

	page, err := os.ReadFile(filepath.Join("testdata", "voicecall", "googlevoice.html"))
	if err != nil {
		t.Fatal(err)
	}
	h := &callHarness{t: t, root: t.TempDir(), scenarios: map[string]*harnessScenario{}}

	shim := httptest.NewServer(h.shimHandler())
	defer shim.Close()

	// The browser has to believe it is on voice.google.com: the injection script
	// navigates away from anything else, and the media APIs it uses need a
	// secure context. A TLS server plus a host-resolver rule gives it both.
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}))
	defer site.Close()
	_, sitePort, err := net.SplitHostPort(strings.TrimPrefix(site.URL, "https://"))
	if err != nil {
		t.Fatal(err)
	}

	ctxTimeout := 4 * time.Minute
	cmd := exec.Command("node", filepath.Join("testdata", "voicecall", "drive.mjs"))
	cmd.Env = append(scrubProxyEnv(os.Environ()),
		"FLIPAI_TEST_BASE=https://voice.google.com/",
		"FLIPAI_TEST_SHIM="+shim.URL+"/",
		"FLIPAI_TEST_MAP="+fmt.Sprintf("MAP voice.google.com:443 127.0.0.1:%s", sitePort),
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("browser driver failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
		}
	case <-time.After(ctxTimeout):
		_ = cmd.Process.Kill()
		t.Fatalf("browser driver timed out\nstderr:\n%s", stderr.String())
	}

	var report driverReport
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("could not read driver report: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	for _, s := range report.Scenarios {
		if len(s.Errors) > 0 {
			t.Errorf("scenario %s raised page errors: %v", s.Name, s.Errors)
		}
	}

	t.Run("authorized call is answered bridged and torn down", func(t *testing.T) {
		i := report.find(t, "authorized-number")
		mid := report.Scenarios[i].MidCall
		if !mid.SecureContext || !mid.HasMediaDevices || !mid.HasSetSinkID {
			t.Fatalf("harness page is not a realistic call surface: %+v", mid)
		}
		wantMic := deviceIDByLabel(t, report, i, "audioinput", "Fake Audio Input 2")
		wantSpeaker := deviceIDByLabel(t, report, i, "audiooutput", "Fake Audio Output 2")

		if mid.GumError != "" {
			t.Fatalf("Google Voice could not open the microphone: %s", mid.GumError)
		}
		// The browser actually opened the endpoint FlipAi chose, rather than the
		// page's default microphone.
		if got, _ := mid.GumSettings["deviceId"].(string); got != wantMic {
			t.Errorf("call microphone = %q, want the configured endpoint %q", got, wantMic)
		}
		// Google Voice's own capture is the one carrying echoCancellation; FlipAi
		// also opens the microphone at startup, so the right request is picked
		// out rather than assuming which came last.
		var pageCall any
		for _, c := range mid.GumCalls {
			if digInto(c, "audio", "echoCancellation") == true {
				pageCall = c
			}
		}
		if pageCall == nil {
			t.Fatalf("Google Voice's own microphone request never reached the browser: %+v", mid.GumCalls)
		}
		if exact := digInto(pageCall, "audio", "deviceId", "exact"); exact != wantMic {
			t.Errorf("microphone constraint = %v, want exact %q", exact, wantMic)
		}
		if mid.SinkID != wantSpeaker {
			t.Errorf("call speaker = %q, want the configured endpoint %q", mid.SinkID, wantSpeaker)
		}
		if !mid.PlayCalled {
			t.Error("the page never started playback, so speaker routing was not exercised")
		}

		acts, deacts := h.scenario("authorized-number").recorded()
		if len(acts) != 1 || acts[0] != "C" {
			t.Errorf("agent activations = %v, want exactly one ChatGPT/Codex activation", acts)
		}
		if len(deacts) != 1 || deacts[0] != "C" {
			t.Errorf("agent deactivations = %v, want exactly one on hang-up", deacts)
		}
		st := loadVoiceRuntime(h.scenario("authorized-number").dataDir)
		if st.InCall || st.Caller != "" || st.Agent != "" {
			t.Errorf("runtime state after hang-up still shows a call: %+v", st)
		}
		if st.LastEvent != "call-ended" {
			t.Errorf("last event = %q, want call-ended", st.LastEvent)
		}
		// The endpoint pickers in Settings are populated from here, so the page
		// has to have reported real named endpoints without waiting for a call.
		if len(st.Devices) == 0 || st.DeviceLabelsHidden {
			t.Errorf("no named audio endpoints reached the settings state: %+v", st.Devices)
		}
	})

	// Google renames the control that ends a call. FlipAi recognizes a call from
	// the controls the page draws, so a name it has never seen must not read as
	// "there is no call" -- that would shut the desktop app's voice session down
	// in the middle of a conversation, leaving the caller talking to nothing.
	t.Run("a renamed hang-up control does not end a live call", func(t *testing.T) {
		i := report.find(t, "hangup-control-renamed")
		if report.countCalls(i, "flipVoiceAnswered") != 1 {
			t.Fatalf("the call was bridged %d times", report.countCalls(i, "flipVoiceAnswered"))
		}
		s := h.scenario("hangup-control-renamed")
		acts, deacts := s.recorded()
		if len(acts) != 1 || acts[0] != "C" {
			t.Fatalf("agent activations = %v, want exactly one", acts)
		}
		// Four ticks passed with nothing FlipAi knows how to name as a hang-up
		// control. If the call had been declared over during them, the agent
		// would have been torn down and started again.
		if len(deacts) != 1 {
			t.Errorf("agent deactivations = %v; a renamed control ended the call early or never ended it", deacts)
		}
		st := loadVoiceRuntime(s.dataDir)
		if st.InCall {
			t.Errorf("the call did not clean up once it really ended: %+v", st)
		}
	})

	t.Run("contact name alone does not authorize a call", func(t *testing.T) {
		i := report.find(t, "contact-name-not-allowed")
		if report.Scenarios[i].Answered {
			t.Fatal("a call showing only a contact name was answered")
		}
		s := h.scenario("contact-name-not-allowed")
		if acts, _ := s.recorded(); len(acts) != 0 {
			t.Errorf("agent was activated for an unauthorized call: %v", acts)
		}
		st := loadVoiceRuntime(s.dataDir)
		if st.CallerLabel != "Jane Appleseed" {
			t.Errorf("caller label recorded as %q, want the name Google Voice displayed", st.CallerLabel)
		}
		if !strings.Contains(st.Blocked, "Jane Appleseed") {
			t.Errorf("block reason %q does not tell the user what was seen", st.Blocked)
		}
	})

	t.Run("approving the displayed name lets the call through", func(t *testing.T) {
		i := report.find(t, "contact-name-allowed")
		if report.countCalls(i, "flipVoiceAnswered") == 0 {
			t.Fatal("an approved contact name was never bridged")
		}
		if acts, _ := h.scenario("contact-name-allowed").recorded(); len(acts) != 1 || acts[0] != "C" {
			t.Errorf("agent activations = %v, want one ChatGPT/Codex activation", acts)
		}
	})

	t.Run("a caller named only on the answer control still matches", func(t *testing.T) {
		i := report.find(t, "caller-named-on-answer-button")
		if report.countCalls(i, "flipVoiceAnswered") == 0 {
			t.Fatal("a caller identified by the Answer button's accessible name was not bridged")
		}
		if acts, _ := h.scenario("caller-named-on-answer-button").recorded(); len(acts) != 1 {
			t.Errorf("agent activations = %v, want exactly one", acts)
		}
	})

	t.Run("unknown number is never answered", func(t *testing.T) {
		i := report.find(t, "unauthorized-number")
		if report.Scenarios[i].Answered {
			t.Fatal("a call from an unlisted number was answered")
		}
		if acts, _ := h.scenario("unauthorized-number").recorded(); len(acts) != 0 {
			t.Errorf("agent was activated for an unlisted number: %v", acts)
		}
	})

	t.Run("a number elsewhere on the page cannot authorize a call", func(t *testing.T) {
		i := report.find(t, "decoy-number-on-page")
		if report.Scenarios[i].Answered {
			t.Fatal("an unidentified call was answered using a number from the thread list")
		}
		if acts, _ := h.scenario("decoy-number-on-page").recorded(); len(acts) != 0 {
			t.Errorf("agent was activated from a decoy number: %v", acts)
		}
	})

	t.Run("overlapping ticks answer a call only once", func(t *testing.T) {
		i := report.find(t, "no-double-answer")
		if n := report.countCalls(i, "flipVoiceIncoming"); n != 1 {
			t.Errorf("flipVoiceIncoming called %d times for one ring, want 1", n)
		}
		if n := report.countCalls(i, "flipVoiceAnswered"); n != 1 {
			t.Errorf("flipVoiceAnswered called %d times for one call, want 1", n)
		}
		if acts, _ := h.scenario("no-double-answer").recorded(); len(acts) != 1 {
			t.Errorf("agent activations = %v, want exactly one", acts)
		}
	})

	// FlipAi runs this window minimized so calls are answered in the background,
	// and Chromium slows a hidden window's timers to a crawl. A call that rings
	// while the poll timer is throttled still has to be answered.
	t.Run("a call is answered without the poll timer", func(t *testing.T) {
		i := report.find(t, "answers-without-the-poll-timer")
		if report.Scenarios[i].PollTimers == 0 {
			t.Fatal("the poll timer was never stopped, so this scenario proves nothing")
		}
		if n := report.countCalls(i, "flipVoiceAnswered"); n != 1 {
			t.Errorf("flipVoiceAnswered called %d times, want 1", n)
		}
		if acts, _ := h.scenario("answers-without-the-poll-timer").recorded(); len(acts) != 1 {
			t.Errorf("agent activations = %v, want exactly one", acts)
		}
	})

	t.Run("a leftover auto-answer setting cannot stop answering", func(t *testing.T) {
		i := report.find(t, "answers-despite-legacy-autoanswer-off")
		if !report.Scenarios[i].Answered {
			t.Fatal("an authorized call was not answered with a legacy autoAnswer=false on disk")
		}
	})

	t.Run("a ring rendered inside a same-origin iframe is answered", func(t *testing.T) {
		i := report.find(t, "ring-inside-iframe")
		if !report.Scenarios[i].Answered {
			t.Fatal("the Answer control inside a frame was never clicked")
		}
		if acts, _ := h.scenario("ring-inside-iframe").recorded(); len(acts) != 1 || acts[0] != "C" {
			t.Errorf("agent activations = %v, want exactly one", acts)
		}
	})

	t.Run("a caller named only by the notification is matched and answered", func(t *testing.T) {
		i := report.find(t, "notification-names-the-caller")
		if !report.Scenarios[i].Answered {
			t.Fatal("a call announced through a notification was not answered")
		}
		if report.Scenarios[i].Observed.NoteError != "" {
			t.Errorf("raising the incoming-call notification failed: %s", report.Scenarios[i].Observed.NoteError)
		}
		st := loadVoiceRuntime(h.scenario("notification-names-the-caller").dataDir)
		if st.Caller != "8455551000" {
			t.Errorf("caller recorded as %q, want the number the notification carried", st.Caller)
		}
	})

	// The case reported from a real PC: Google Voice shows the Contacts name it
	// has for the caller and no number, while the notification for the same ring
	// carries the number -- and the number is what the user allowed on the
	// agent. A card showing a name is not a caller FlipAi failed to identify.
	t.Run("a name on the card does not hide the number in the notification", func(t *testing.T) {
		i := report.find(t, "notification-number-behind-contact-name")
		if !report.Scenarios[i].Answered {
			t.Fatal("a caller shown by name on the card and by number in the notification was not answered")
		}
		s := h.scenario("notification-number-behind-contact-name")
		if acts, _ := s.recorded(); len(acts) != 1 || acts[0] != "C" {
			t.Errorf("agent activations = %v, want exactly one", acts)
		}
		st := loadVoiceRuntime(s.dataDir)
		if st.Caller != "8455551000" {
			t.Errorf("caller recorded as %q, want the number the notification carried", st.Caller)
		}
		if st.CallerLabel != "Me" {
			t.Errorf("caller label = %q, want the name the card displayed", st.CallerLabel)
		}
	})

	// FlipAi's second way into the page. On Windows this is the rung used when
	// the page's own click is ignored, and it is the only way FlipAi can see a
	// call if the injected script has wedged. Everything here is the product's
	// own script string, run in a real browser, decoded into the product's own
	// types.
	t.Run("the control channel reads the call and presses answer", func(t *testing.T) {
		i := report.find(t, "control-channel-probe")
		p := report.Scenarios[i].Probe

		if !p.Ringing.Answer || p.Ringing.Hangup {
			t.Errorf("a ringing call read as answer=%v hangup=%v, want a ring", p.Ringing.Answer, p.Ringing.Hangup)
		}
		if !p.Ringing.SignedIn {
			t.Error("the control channel did not recognize a signed-in Google Voice page")
		}
		// Same fix as above, on the other path: the card shows a name, the
		// notification carries the number, and FlipAi's own read has to agree
		// with the injected script's or the two disagree about who is calling.
		if p.Ringing.Caller != "8455559999" {
			t.Errorf("control channel read the caller as %q, want the notification's number", p.Ringing.Caller)
		}
		if p.Ringing.Label != "Me" {
			t.Errorf("control channel read the label as %q, want the displayed name", p.Ringing.Label)
		}
		if len(p.Ringing.Devices) == 0 {
			t.Error("the control channel's snapshot carried no audio endpoints")
		}
		if !p.Point.Found || p.Point.X <= 0 || p.Point.Y <= 0 {
			t.Errorf("no place to aim a real pointer press at a ringing card: %+v", p.Point)
		}
		if !p.Clicked {
			t.Error("the control channel found nothing to press on a ringing card")
		}

		// Once the call is up the reading has to flip, or FlipAi answers a call
		// that is already answered and never notices it ended.
		if p.Live.Answer || !p.Live.Hangup {
			t.Errorf("a live call read as answer=%v hangup=%v, want a call in progress", p.Live.Answer, p.Live.Hangup)
		}
		if !p.Live.CallControls {
			t.Error("a live call did not offer the mute and keypad controls that keep it alive")
		}
		if p.LivePoint.Found {
			t.Error("the control channel would have pressed Answer during a live call")
		}

		// And back to nothing, which is what lets a call end. The ordinary page
		// offers mute and a keypad of its own, so this deliberately does not
		// require callControls to be false -- it requires it to be powerless,
		// which is the machine's job and is tested in voice_session_test.go.
		if p.Idle.Answer || p.Idle.Hangup {
			t.Errorf("the hung-up page still read as a call: %+v", p.Idle)
		}
	})

	t.Run("the call window keeps only the microphone", func(t *testing.T) {
		i := report.find(t, "capabilities-removed")
		caps := report.Scenarios[i].Capabilities
		for name, want := range map[string]string{
			"camera":      "NotAllowedError",
			"geolocation": "denied",
			"clipboard":   "NotAllowedError",
			"screen":      "NotAllowedError",
		} {
			if caps[name] != want {
				t.Errorf("%s = %q, want %q", name, caps[name], want)
			}
		}
	})
}

// digInto walks a decoded JSON object by key, returning nil if the path is not
// present. It keeps the constraint assertions readable.
func digInto(v any, path ...string) any {
	for _, key := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[key]
	}
	return v
}

// scrubProxyEnv removes the outbound HTTPS proxy from the driver's environment.
// The harness only ever talks to loopback, and the proxy cannot route the
// synthetic voice.google.com host the browser is pointed at.
func scrubProxyEnv(env []string) []string {
	out := env[:0:0]
	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		switch strings.ToLower(name) {
		case "http_proxy", "https_proxy", "all_proxy", "no_proxy":
			continue
		}
		out = append(out, e)
	}
	return out
}
