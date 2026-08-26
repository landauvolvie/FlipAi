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

// voiceTestConfig builds a main configuration where one number reaches Codex.
func voiceTestConfig(t *testing.T) Config {
	t.Helper()
	cfg := defaultConfig(t.TempDir())
	cfg.DefaultAgent = "C"
	cfg.Codex.Phones = []AgentPhone{
		{Number: "8455551000", Access: AccessAll},
		{Number: "8455554000", Access: AccessSMS},
	}
	cfg.Claude.Phones = []AgentPhone{{Number: "8455552000", Access: AccessVoice}}
	return cfg
}

func enabledVoiceConfig() VoiceCallConfig {
	vc := defaultVoiceCallConfig()
	vc.Enabled = true
	vc.Codex.Enabled = true
	vc.Claude.Enabled = true
	return vc
}

func TestVoiceCallsUseTheSameAllowlistAsTexts(t *testing.T) {
	cfg := voiceTestConfig(t)
	vc := enabledVoiceConfig()

	if d := decideVoiceCall(vc, cfg, "+1 (845) 555-1000", ""); !d.Allowed || d.Agent != "C" {
		t.Fatalf("a number allowed for texts and calls on Codex was refused: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "8455552000", ""); !d.Allowed || d.Agent != "A" {
		t.Fatalf("a calls-only Claude number was refused: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "8455559999", ""); d.Allowed {
		t.Fatal("an unlisted number was allowed to call")
	}
}

func TestVoiceRefusesANumberAllowedForTextsOnly(t *testing.T) {
	cfg := voiceTestConfig(t)
	d := decideVoiceCall(enabledVoiceConfig(), cfg, "8455554000", "")
	if d.Allowed {
		t.Fatal("a texts-only number was allowed to call")
	}
	if !strings.Contains(d.Reason, "not to call") {
		t.Errorf("reason does not explain the permission: %q", d.Reason)
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
	cfg := voiceTestConfig(t)
	cfg.Codex.CallerNames = "Jane Appleseed"
	vc := enabledVoiceConfig()

	// Google Voice shows a name instead of a number whenever the caller is in
	// the user's contacts, which is the normal case for calling your own line.
	if d := decideVoiceCall(vc, cfg, "", "Jane Appleseed"); !d.Allowed || d.Agent != "C" {
		t.Fatalf("approved caller name was refused: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "", "jane   appleseed"); !d.Allowed {
		t.Fatalf("caller name matching must ignore case and spacing: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "", "John Appleseed"); d.Allowed {
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
	cfg := voiceTestConfig(t)
	cfg.Codex.CallerNames = "Unknown"
	if d := decideVoiceCall(enabledVoiceConfig(), cfg, "", "Unknown"); d.Allowed {
		t.Fatal("a placeholder caller ID was authorized")
	}
}

func TestVoiceBlockedCallExplainsItself(t *testing.T) {
	cfg := voiceTestConfig(t)
	vc := enabledVoiceConfig()

	d := decideVoiceCall(vc, cfg, "", "Jane Appleseed")
	if d.Allowed {
		t.Fatal("an unlisted contact name was authorized")
	}
	if !strings.Contains(d.Reason, "Jane Appleseed") || !strings.Contains(d.Reason, "Allowed caller names") {
		t.Errorf("reason does not tell the user what to do: %q", d.Reason)
	}
	if d := decideVoiceCall(vc, cfg, "8455559999", ""); d.Allowed || !strings.Contains(d.Reason, "845") {
		t.Errorf("unlisted number reason = %q", d.Reason)
	}
	if d := decideVoiceCall(vc, cfg, "", ""); d.Allowed || d.Reason == "" {
		t.Errorf("a call with no caller ID must still be explained, got %+v", d)
	}
}

// There is no separate auto-answer switch: an authorized caller is answered
// because calling is enabled, full stop. A legacy autoAnswer=false left in an
// old voice-call.json changes nothing.
func TestAuthorizedCallersAreAlwaysAnswered(t *testing.T) {
	dir := t.TempDir()
	main := voiceTestConfig(t)
	vc := enabledVoiceConfig()
	vc.AutoAnswer = false // the retired switch, off, as an old config might have it
	if err := saveVoiceCallConfig(dir, vc); err != nil {
		t.Fatal(err)
	}
	b := newVoiceBridge(dir, func() Config { return main },
		func(VoiceCallConfig, string) error { return nil },
		func(VoiceCallConfig, string) error { return nil })
	if !b.Incoming("8455551000", "") {
		t.Fatal("an authorized caller was not answered; the retired auto-answer switch is still being honored")
	}
	if b.Incoming("8455559999", "") {
		t.Fatal("an unauthorized caller was answered")
	}
}

func TestVoiceBridgeTurnsAgentVoiceOffWhenTheCallEnds(t *testing.T) {
	dir := t.TempDir()
	main := voiceTestConfig(t)
	if err := saveVoiceCallConfig(dir, enabledVoiceConfig()); err != nil {
		t.Fatal(err)
	}
	var on, off []string
	b := newVoiceBridge(dir, func() Config { return main },
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
	if !strings.Contains(googleVoiceInitScript, "function observeDoc(doc)") ||
		!strings.Contains(googleVoiceInitScript, "if (!root) return false;") {
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
// that text is the only thing the user ever sees.
func TestOpenGoogleVoiceReportsFailureToTheButton(t *testing.T) {
	dir := t.TempDir()
	h := voiceControlHandler(dir, "127.0.0.1:8765", func() Config { return voiceTestConfig(t) },
		activityLogForStatePath(filepath.Join(dir, "state.json")))
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

	restore := openGoogleVoiceWindow
	defer func() { openGoogleVoiceWindow = restore }()

	// The case that used to be swallowed. Opening spans two processes, so the
	// window can fail long after the click; the endpoint must answer with that
	// reason rather than the success it used to report the moment a process
	// had been launched.
	const reason = "the window process started but never created a window"
	openGoogleVoiceWindow = func(string, bool) error { return errors.New(reason) }
	code, body := post("/open", "http://127.0.0.1:8765")
	if code == http.StatusOK {
		t.Fatal("a failed open reported success to the button")
	}
	if body != reason {
		t.Errorf("button would show %q, want %q", body, reason)
	}

	openGoogleVoiceWindow = func(string, bool) error { return nil }
	if code, body := post("/open", "http://127.0.0.1:8765"); code != http.StatusOK {
		t.Errorf("a successful open reported %d: %s", code, body)
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

	// Two processes write these notes: the one handling the click and the one
	// that owns the window. A note from either must never erase the reason the
	// other has just recorded -- that is how a window process which died on
	// startup left "window opened" behind and nothing else, and the click
	// reported success over a window that was already gone.
	recordVoiceOpen(dir, "window opened", nil)
	st := loadVoiceRuntime(dir)
	if st.LastOpen != "window opened" {
		t.Fatalf("successful open recorded as %q", st.LastOpen)
	}
	if got := lastVoiceOpenFailure(dir, started); !strings.Contains(got, "WebView2") {
		t.Fatalf("a success note erased the reason the window process failed: %q", got)
	}

	// Only a new attempt clears it, because only then is the old reason spent.
	next := time.Now()
	beginVoiceOpen(dir, "starting the Google Voice window process")
	if got := lastVoiceOpenFailure(dir, next); got != "" {
		t.Fatalf("a new attempt inherited the previous failure: %q", got)
	}
	if st := loadVoiceRuntime(dir); st.LastOpen != "starting the Google Voice window process" {
		t.Fatalf("a new attempt was not recorded: %+v", st)
	}
}

// Switching phone voice on is what makes Google Voice start at sign-in and stay
// running, and it is also what every incoming call is checked against. It must
// not depend on hardware the user may not have yet.
func TestPhoneVoiceCanBeEnabledWithoutAudioCables(t *testing.T) {
	vc := defaultVoiceCallConfig()
	vc.Enabled = true
	vc.Codex.Enabled = true

	saved, err := normalizeVoiceCallConfig(vc, true)
	if err != nil {
		t.Fatalf("phone voice could not be switched on without cables: %v", err)
	}
	if !saved.Enabled {
		t.Fatal("the setting did not survive saving")
	}

	// And a call from an allowed number is accepted, rather than refused with
	// "phone voice is turned off".
	cfg := voiceTestConfig(t)
	if d := decideVoiceCall(saved, cfg, "8455551000", ""); !d.Allowed || d.Agent != "C" {
		t.Fatalf("an allowed caller was refused with no cables configured: %+v", d)
	}

	// Having nobody to hand a call to does not stop the window from being kept
	// running either -- signing in to Google Voice is the step that comes first.
	// The call itself is what says nothing would answer.
	none := defaultVoiceCallConfig()
	none.Enabled = true
	if _, err := normalizeVoiceCallConfig(none, true); err != nil {
		t.Errorf("keeping Google Voice running before any agent has a number was refused: %v", err)
	}
	if d := decideVoiceCall(none, defaultConfig(t.TempDir()), "8455551000", ""); d.Allowed ||
		!strings.Contains(d.Reason, "Agents page") {
		t.Errorf("a call with no agent on calls should be refused with directions, got %+v", d)
	}
}

// Marking a number "Texts and calls" under an agent is the user saying that
// agent takes calls. Making them also find a second switch elsewhere is how a
// number that was plainly allowed to call still went unanswered.
func TestAllowingANumberToCallPutsItsAgentOnCalls(t *testing.T) {
	cfg := voiceTestConfig(t)
	vc := defaultVoiceCallConfig()
	vc.Enabled = true // and neither vc.Codex.Enabled nor vc.Claude.Enabled

	if d := decideVoiceCall(vc, cfg, "8455551000", ""); !d.Allowed || d.Agent != "C" {
		t.Fatalf("a number allowed to call ChatGPT was refused: %+v", d)
	}
	if d := decideVoiceCall(vc, cfg, "8455552000", ""); !d.Allowed || d.Agent != "A" {
		t.Fatalf("a calls-only number under Claude was refused: %+v", d)
	}
	// An SMS-only number still may not call, and the reason has to say so.
	if d := decideVoiceCall(vc, cfg, "8455554000", ""); d.Allowed ||
		!strings.Contains(d.Reason, "not to call") {
		t.Fatalf("an SMS-only number should not reach a call: %+v", d)
	}
	// A caller name approved for calls counts the same way.
	named := defaultConfig(t.TempDir())
	named.Claude.CallerNames = "Jane Appleseed"
	if d := decideVoiceCall(vc, named, "", "Jane Appleseed"); !d.Allowed || d.Agent != "A" {
		t.Fatalf("an approved caller name was refused: %+v", d)
	}
	// And the snapshot the desktop UI reads reports who can take a call.
	snap := voiceSnapshot(t.TempDir(), func() Config { return cfg })
	if len(snap.CallAgents) != 2 {
		t.Errorf("both agents own a number that may call, snapshot said %v", snap.CallAgents)
	}
	if got := voiceSnapshot(t.TempDir(), func() Config { return defaultConfig(t.TempDir()) }); len(got.CallAgents) != 0 {
		t.Errorf("a fresh install has nobody on calls, snapshot said %v", got.CallAgents)
	}
}

func TestAnsweredCallSaysWhenItHasNoAudioPath(t *testing.T) {
	dir := t.TempDir()
	main := voiceTestConfig(t)
	vc := defaultVoiceCallConfig()
	vc.Enabled = true
	vc.Codex.Enabled = true
	if err := saveVoiceCallConfig(dir, vc); err != nil {
		t.Fatal(err)
	}
	b := newVoiceBridge(dir, func() Config { return main },
		func(VoiceCallConfig, string) error { return nil },
		func(VoiceCallConfig, string) error { return nil })

	// No cables have been found on this machine yet: the call still connects,
	// and the state says exactly why it would be silent.
	if !b.Answered("8455551000", "") {
		t.Fatal("an allowed caller was not bridged")
	}
	st := loadVoiceRuntime(dir)
	if st.LastEvent != "call-bridged" {
		t.Fatalf("call was not bridged: %+v", st)
	}
	if st.LastError == "" {
		t.Error("a silent call must say what is wrong with the audio path")
	}

	// Once two cables are visible, the same call carries no warning.
	b.Devices(`[
		{"kind":"audiooutput","deviceId":"r1","label":"CABLE-A Input (VB-Audio Cable A)"},
		{"kind":"audioinput","deviceId":"c1","label":"CABLE-A Output (VB-Audio Cable A)"},
		{"kind":"audiooutput","deviceId":"r2","label":"CABLE-B Input (VB-Audio Cable B)"},
		{"kind":"audioinput","deviceId":"c2","label":"CABLE-B Output (VB-Audio Cable B)"}]`)
	if !b.Answered("8455551000", "") {
		t.Fatal("an allowed caller was not bridged with cables present")
	}
	if st := loadVoiceRuntime(dir); st.LastError != "" {
		t.Errorf("a fully wired machine still warned: %q", st.LastError)
	}
}
