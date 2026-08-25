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
// real voiceBridge, and Chromium's fake audio endpoints stand in for the two
// virtual cables the feature needs on a real PC. The microphone capture and the
// media-element sink are genuinely applied by the browser, so the routing
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
		Capabilities map[string]string `json:"capabilities"`
		Observed     observedPage      `json:"observed"`
		MidCall      observedPage      `json:"midCall"`
	} `json:"scenarios"`
}

type observedPage struct {
	GumCalls        []any          `json:"gumCalls"`
	GumSettings     map[string]any `json:"gumSettings"`
	GumError        string         `json:"gumError"`
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
