package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVoiceCallDefaultsAreOffAndRestrictedToGoogleVoice(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	if cfg.Enabled {
		t.Fatal("voice calling must be opt-in")
	}
	if cfg.GoogleVoiceURL != googleVoiceWebURL {
		t.Fatalf("Google Voice URL = %q", cfg.GoogleVoiceURL)
	}
	if !cfg.AutoAnswer {
		t.Fatal("once enabled, authorized calls should auto-answer by default")
	}
}

func TestVoiceCallConfigNeverBecomesGeneralBrowser(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	cfg.GoogleVoiceURL = "https://example.com/"
	got, err := normalizeVoiceCallConfig(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoogleVoiceURL != googleVoiceWebURL {
		t.Fatalf("normalized URL = %q", got.GoogleVoiceURL)
	}
}

func TestVoiceEnabledAgentRequiresCallerAllowlist(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	cfg.Codex.Enabled = true
	if _, err := normalizeVoiceCallConfig(cfg, true); err == nil {
		t.Fatal("enabled agent without allowed callers should be rejected")
	}
}

func TestVoiceCallerRoutesPerAgentAndDefault(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	cfg.Enabled = true
	cfg.Codex.Enabled = true
	cfg.Claude.Enabled = true
	cfg.Codex.AllowedCallers = "8455551000\n8455553000"
	cfg.Claude.AllowedCallers = "8455552000\n8455553000"

	if agent, ok := voiceAgentForCaller(cfg, "+1 (845) 555-1000"); !ok || agent != "C" {
		t.Fatalf("Codex-only caller routed to %q, %v", agent, ok)
	}
	if agent, ok := voiceAgentForCaller(cfg, "8455552000"); !ok || agent != "A" {
		t.Fatalf("Claude-only caller routed to %q, %v", agent, ok)
	}
	if agent, ok := voiceAgentForCaller(cfg, "8455553000"); !ok || agent != "C" {
		t.Fatalf("shared caller should follow C default, got %q, %v", agent, ok)
	}
	cfg.DefaultAgent = "A"
	if agent, ok := voiceAgentForCaller(cfg, "8455553000"); !ok || agent != "A" {
		t.Fatalf("shared caller should follow A default, got %q, %v", agent, ok)
	}
	if agent, ok := voiceAgentForCaller(cfg, "8455559999"); ok || agent != "" {
		t.Fatalf("unknown caller was authorized as %q", agent)
	}
	if agent, ok := voiceAgentForCaller(cfg, "Private Caller"); ok || agent != "" {
		t.Fatalf("unparseable caller was authorized as %q", agent)
	}
}

func TestVoiceControlOriginOnlyAcceptsFlipAiLoopback(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:8765", "http://localhost:8765"} {
		if !voiceOriginAllowed(origin, "127.0.0.1:8765") {
			t.Fatalf("expected %q to be allowed", origin)
		}
	}
	for _, origin := range []string{"https://voice.google.com", "http://127.0.0.1:9999", "http://evil.example:8765", ""} {
		if voiceOriginAllowed(origin, "127.0.0.1:8765") {
			t.Fatalf("expected %q to be rejected", origin)
		}
	}
}

func TestVoiceConfigIsIndependentFromSMSConfig(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "bridge.json")
	mainCfg := defaultConfig(dir)
	mainCfg.GoogleVoice.AllowedFrom = "8455551111"
	if err := saveConfig(mainPath, mainCfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}

	vc := defaultVoiceCallConfig()
	vc.Codex.Enabled = true
	vc.Codex.AllowedCallers = "8455552222"
	if err := saveVoiceCallConfig(dir, vc); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("saving voice-call settings modified the SMS bridge config")
	}
}

func TestVoiceCallerNameAllowlistCoversContactCallers(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	cfg.Enabled = true
	cfg.GoogleVoiceInput = "Cable B Output"
	cfg.GoogleVoiceOutput = "Cable A Input"
	cfg.Codex.Enabled = true
	cfg.Codex.AllowedLabels = "Jane Appleseed"

	// Google Voice shows a name instead of a number whenever the caller is in
	// the user's contacts, which is the normal case for calling your own line.
	if d := decideVoiceCall(cfg, "", "Jane Appleseed"); !d.Allowed || d.Agent != "C" {
		t.Fatalf("approved caller name was refused: %+v", d)
	}
	if d := decideVoiceCall(cfg, "", "jane   appleseed"); !d.Allowed {
		t.Fatalf("caller name matching must ignore case and spacing: %+v", d)
	}
	if d := decideVoiceCall(cfg, "", "John Appleseed"); d.Allowed {
		t.Fatal("a different name was authorized")
	}
}

func TestVoiceCallerNameAllowlistRejectsPlaceholders(t *testing.T) {
	// "Unknown" is what the network supplies when there is no caller ID at all,
	// so allowing it would authorize every anonymous call.
	for _, name := range []string{"Unknown", "unknown caller", "Private Number", "Anonymous"} {
		if _, err := normalizeAllowedCallerLabels(name, true); err == nil {
			t.Errorf("%q was accepted as an allowed caller name", name)
		}
		if got, err := normalizeAllowedCallerLabels(name, false); err != nil || len(got) != 0 {
			t.Errorf("loading a stored %q should drop it quietly, got %v %v", name, got, err)
		}
	}
	cfg := defaultVoiceCallConfig()
	cfg.Enabled = true
	cfg.Codex.Enabled = true
	cfg.Codex.AllowedLabels = "Unknown"
	if d := decideVoiceCall(cfg, "", "Unknown"); d.Allowed {
		t.Fatal("a placeholder caller ID was authorized")
	}
}

func TestVoiceBlockedCallExplainsItself(t *testing.T) {
	cfg := defaultVoiceCallConfig()
	cfg.Enabled = true
	cfg.Codex.Enabled = true
	cfg.Codex.AllowedCallers = "8455551000"

	d := decideVoiceCall(cfg, "", "Jane Appleseed")
	if d.Allowed {
		t.Fatal("an unlisted contact name was authorized")
	}
	if !strings.Contains(d.Reason, "Jane Appleseed") || !strings.Contains(d.Reason, "Allowed caller names") {
		t.Errorf("reason does not tell the user what to do: %q", d.Reason)
	}
	if d := decideVoiceCall(cfg, "8455559999", ""); d.Allowed || !strings.Contains(d.Reason, "845") {
		t.Errorf("unlisted number reason = %q", d.Reason)
	}
	if d := decideVoiceCall(cfg, "", ""); d.Allowed || d.Reason == "" {
		t.Errorf("a call with no caller ID must still be explained, got %+v", d)
	}
}

func TestVoiceAudioBridgeRejectsSilentWiring(t *testing.T) {
	base := func() VoiceCallConfig {
		cfg := defaultVoiceCallConfig()
		cfg.Enabled = true
		cfg.Codex.Enabled = true
		cfg.Codex.AllowedCallers = "8455551000"
		cfg.GoogleVoiceInput = "Cable B Output (capture)"
		cfg.GoogleVoiceOutput = "Cable A Input (render)"
		cfg.AgentInput = "Cable A Output (capture)"
		cfg.AgentOutput = "Cable B Input (render)"
		return cfg
	}
	if _, err := normalizeVoiceCallConfig(base(), true); err != nil {
		t.Fatalf("a correctly wired pair of cables was rejected: %v", err)
	}

	missing := base()
	missing.GoogleVoiceOutput = ""
	if _, err := normalizeVoiceCallConfig(missing, true); err == nil {
		t.Error("enabling calling without an audio path should be refused")
	}

	shared := base()
	shared.AgentOutput = shared.GoogleVoiceOutput
	if _, err := normalizeVoiceCallConfig(shared, true); err == nil {
		t.Error("both sides sharing one speaker endpoint should be refused")
	}

	sharedMic := base()
	sharedMic.AgentInput = sharedMic.GoogleVoiceInput
	if _, err := normalizeVoiceCallConfig(sharedMic, true); err == nil {
		t.Error("both sides sharing one microphone endpoint should be refused")
	}
}

func TestVoiceBridgeReportsOnlyRealAudioEndpoints(t *testing.T) {
	dir := t.TempDir()
	b := newVoiceBridge(dir, func(VoiceCallConfig, string) error { return nil }, func(VoiceCallConfig, string) error { return nil })

	// Before a page holds a microphone grant the browser returns endpoints with
	// empty names. Inventing names for them would put unselectable placeholders
	// in the settings pickers and hide the real problem.
	b.Devices(`[{"kind":"audioinput","deviceId":"a","label":""},{"kind":"audiooutput","deviceId":"b","label":""}]`)
	st := loadVoiceRuntime(dir)
	if len(st.Devices) != 0 || !st.DeviceLabelsHidden {
		t.Fatalf("unnamed endpoints were not reported as hidden: %+v", st)
	}

	b.Devices(`[{"kind":"audioinput","deviceId":"a","label":"Cable B Output"},{"kind":"videoinput","deviceId":"c","label":"Webcam"}]`)
	st = loadVoiceRuntime(dir)
	if len(st.Devices) != 1 || st.Devices[0].Label != "Cable B Output" {
		t.Fatalf("audio endpoints = %+v, want only the named audio one", st.Devices)
	}
	if st.DeviceLabelsHidden {
		t.Error("named endpoints should clear the hidden-labels warning")
	}
}

func TestVoiceBridgeTurnsAgentVoiceOffWhenTheCallEnds(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultVoiceCallConfig()
	cfg.Enabled = true
	cfg.GoogleVoiceInput = "Cable B Output"
	cfg.GoogleVoiceOutput = "Cable A Input"
	cfg.Codex.Enabled = true
	cfg.Codex.AllowedCallers = "8455551000"
	if err := saveVoiceCallConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	var on, off []string
	b := newVoiceBridge(dir,
		func(_ VoiceCallConfig, agent string) error { on = append(on, agent); return nil },
		func(_ VoiceCallConfig, agent string) error { off = append(off, agent); return nil })

	if !b.Incoming("8455551000", "") {
		t.Fatal("an approved caller was not auto-answered")
	}
	if !b.Answered("8455551000", "") {
		t.Fatal("an approved call was not bridged")
	}
	b.Ended()
	if len(on) != 1 || on[0] != "C" || len(off) != 1 || off[0] != "C" {
		t.Fatalf("agent voice was not switched on and back off exactly once: on=%v off=%v", on, off)
	}
	if st := loadVoiceRuntime(dir); st.InCall || st.Agent != "" {
		t.Fatalf("state after hang-up still shows a call: %+v", st)
	}
}

func TestGoogleVoiceScriptSurvivesDocumentCreatedTiming(t *testing.T) {
	// WebView2 injects this script before <html> exists. Touching the document
	// root at that moment throws, and the throw takes the whole call bridge with
	// it, which is exactly how the feature failed silently before.
	if strings.Contains(googleVoiceInitScript, ".observe(document.documentElement,") {
		t.Fatal("the mutation observer must not assume a document root exists")
	}
	if !strings.Contains(googleVoiceInitScript, "const observeDocument = ") {
		t.Fatal("the mutation observer must wait for a document root")
	}
}

func TestGoogleVoiceScriptIsSingleFlighted(t *testing.T) {
	// The bridge polls, and a poll awaits several host round-trips. Overlapping
	// polls answered one ring twice.
	if strings.Contains(googleVoiceInitScript, "setInterval(tick") {
		t.Fatal("polling must not be able to overlap itself")
	}
	if !strings.Contains(googleVoiceInitScript, "if (ticking) return;") {
		t.Fatal("the poll needs a re-entrancy guard")
	}
}

// The Open button is a POST from the desktop UI to the local voice endpoint.
// Whatever goes wrong behind it has to come back through that response, because
// the response text is the only thing the user ever sees.
func TestOpenGoogleVoiceReportsFailureToTheButton(t *testing.T) {
	dir := t.TempDir()
	h := voiceControlHandler(dir, "127.0.0.1:8765", activityLogForStatePath(filepath.Join(dir, "state.json")))
	srv := httptest.NewServer(h)
	defer srv.Close()

	post := func(path, origin string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(b))
	}

	// Only FlipAi's own desktop window may drive these controls.
	if code, _ := post("/open", "https://voice.google.com"); code != http.StatusForbidden {
		t.Fatalf("a foreign origin got %d, want 403", code)
	}

	// On this platform opening always fails, which is exactly the case that
	// used to be swallowed: the endpoint must answer with the reason, not 200.
	code, body := post("/open", "http://127.0.0.1:8765")
	if code == http.StatusOK {
		t.Fatal("a failed open reported success to the button")
	}
	if body == "" {
		t.Fatal("a failed open gave the button nothing to show")
	}
	if want := platformOpenGoogleVoice(dir, true).Error(); body != want {
		t.Errorf("button would show %q, want the real reason %q", body, want)
	}
}

func TestVoiceOpenAttemptsLeaveAnExplanation(t *testing.T) {
	dir := t.TempDir()
	started := time.Now()

	// Nothing recorded yet: the caller must not invent a reason.
	if got := lastVoiceOpenFailure(dir, started); got != "" {
		t.Fatalf("unexpected recorded failure %q", got)
	}

	recordVoiceOpen(dir, "WebView2 could not create the window", errors.New("runtime missing"))
	got := lastVoiceOpenFailure(dir, started)
	if !strings.Contains(got, "WebView2") || !strings.Contains(got, "runtime missing") {
		t.Fatalf("recorded failure = %q, want the step and the cause", got)
	}
	if st := loadVoiceRuntime(dir); st.LastEvent != "open-failed" || st.LastError == "" {
		t.Fatalf("runtime state does not show the failed open: %+v", st)
	}

	// A record from an earlier attempt must not be reported as the outcome of a
	// later one, or every future click inherits an old error.
	if got := lastVoiceOpenFailure(dir, time.Now().Add(time.Second)); got != "" {
		t.Fatalf("a stale record leaked into a new attempt: %q", got)
	}

	recordVoiceOpen(dir, "window opened", nil)
	if st := loadVoiceRuntime(dir); st.LastOpen != "window opened" {
		t.Fatalf("successful open recorded as %q", st.LastOpen)
	}
}
