package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func voiceEndpoint(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	h := voiceControlHandler(dir, "127.0.0.1:8765", func() Config { return voiceTestConfig(t) },
		activityLogForStatePath(filepath.Join(dir, "state.json")))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func voicePost(t *testing.T, srv *httptest.Server, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, strings.TrimSpace(string(b))
}

// Turning calling on is one switch and it writes on its own. It used to travel
// inside the whole-card save, where any other field could -- and on a normal PC
// did -- refuse the write, leaving the status saying Off however many times the
// switch was flipped.
func TestTurningCallingOnSavesByItself(t *testing.T) {
	dir := t.TempDir()
	// The state that used to make the save impossible: every endpoint holding
	// the same default device name.
	stuck := defaultVoiceCallConfig()
	stuck.GoogleVoiceInput = "Default - Remote Audio"
	stuck.GoogleVoiceOutput = "Default - Remote Audio"
	stuck.AgentInput = "Default - Remote Audio"
	stuck.AgentOutput = "Default - Remote Audio"
	if err := saveVoiceCallConfig(dir, stuck); err != nil {
		t.Fatalf("a fresh PC's default endpoints could not even be saved: %v", err)
	}

	srv := voiceEndpoint(t, dir)
	code, body := voicePost(t, srv, "/enable", `{"enabled":true}`)
	if code != http.StatusOK {
		t.Fatalf("/enable returned %d: %s", code, body)
	}
	var snap voiceControlSnapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("could not read the answer: %v", err)
	}
	if !snap.Config.Enabled {
		t.Fatal("the answer says calling is still off")
	}
	if snap.AudioWarning == "" {
		t.Error("the answer should still say the audio path is wrong")
	}
	if !loadVoiceCallConfig(dir).Enabled {
		t.Fatal("calling was not written to disk")
	}

	// And off again.
	if code, body := voicePost(t, srv, "/enable", `{"enabled":false}`); code != http.StatusOK {
		t.Fatalf("/enable off returned %d: %s", code, body)
	}
	if loadVoiceCallConfig(dir).Enabled {
		t.Fatal("calling could not be switched back off")
	}
}

// The Connections page reports where it has reserved room for Google Voice, and
// the window process reads that rectangle. This is what puts the browser inside
// the app instead of in a popup.
func TestTheEmbeddedPanelRectangleReachesTheWindow(t *testing.T) {
	dir := t.TempDir()
	srv := voiceEndpoint(t, dir)

	// The page reports where the panel sits in its own viewport, in physical
	// pixels; FlipAi turns that into a screen position from its own window.
	code, body := voicePost(t, srv, "/dock", `{"visible":true,"x":100,"y":220,"width":900,"height":700}`)
	if code != http.StatusNoContent {
		t.Fatalf("/dock returned %d: %s", code, body)
	}
	got := loadVoiceDock(dir)
	if !got.Visible || got.X != 100 || got.Y != 220 || got.Width != 900 || got.Height != 700 {
		t.Fatalf("the panel rectangle did not survive: %+v", got)
	}
	if !got.Active(time.Now()) {
		t.Fatal("a rectangle just reported is not being treated as current")
	}

	// A page that has gone away stops reporting, and the dock expires rather
	// than leaving a window stranded on top of the app.
	stale := got
	stale.At = time.Now().Add(-2 * voiceDockTTL)
	if stale.Active(time.Now()) {
		t.Error("an old rectangle still counts as current")
	}
	// So does an explicit hide, immediately.
	if code, _ := voicePost(t, srv, "/dock", `{"visible":false}`); code != http.StatusNoContent {
		t.Fatalf("hiding the panel returned %d", code)
	}
	if loadVoiceDock(dir).Active(time.Now()) {
		t.Error("the panel is still docked after the page hid it")
	}

	// A negative offset is not a position inside a window.
	if got := normalizeVoiceDock(VoiceDockRequest{Visible: true, X: -900, Y: -900, Width: 900, Height: 700}, time.Now()); got.X != 0 || got.Y != 0 {
		t.Errorf("an offset outside the window was not clamped: %+v", got)
	}

	// A panel too small to be a browser is not one.
	tiny := VoiceDockRequest{Visible: true, Width: 40, Height: 40, At: time.Now()}
	if tiny.Active(time.Now()) {
		t.Error("a sliver of a panel should not place a window")
	}
	// Nor is a rectangle from a page that has not laid out yet.
	if (VoiceDockRequest{Visible: true, Width: 900, Height: 700}).Active(time.Now()) {
		t.Error("a rectangle with no timestamp should not place a window")
	}
}

// Docking is a position, not a permission. The endpoint is still local-only.
func TestDockEndpointIsLocalOnly(t *testing.T) {
	dir := t.TempDir()
	srv := voiceEndpoint(t, dir)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/dock", strings.NewReader(`{"visible":true,"x":0,"y":0,"width":900,"height":700}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://voice.google.com")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("a page outside FlipAi could move the Google Voice window: %d", res.StatusCode)
	}
	if loadVoiceDock(dir).Active(time.Now()) {
		t.Error("a refused request still moved the window")
	}
}

// The card sends the whole of itself when any field changes, and that payload
// carries whatever "enabled" was when the card was last read. If it could write
// that field, a save already in flight when the switch is touched would land
// afterwards and switch calling back off -- the exact symptom the switch was
// separated out to end. Only /enable may write it.
func TestSavingTheCardCannotTurnCallingOffBehindTheSwitch(t *testing.T) {
	dir := t.TempDir()
	srv := voiceEndpoint(t, dir)

	if code, body := voicePost(t, srv, "/enable", `{"enabled":true}`); code != http.StatusOK {
		t.Fatalf("/enable returned %d: %s", code, body)
	}

	// A card assembled before the switch was touched: it still says off.
	stale := defaultVoiceCallConfig()
	stale.Enabled = false
	stale.GoogleVoiceInput = "Cable B Output (capture)"
	raw, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	code, body := voicePost(t, srv, "/config", string(raw))
	if code != http.StatusOK {
		t.Fatalf("/config returned %d: %s", code, body)
	}

	saved := loadVoiceCallConfig(dir)
	if !saved.Enabled {
		t.Fatal("a stale card save switched calling back off")
	}
	if saved.GoogleVoiceInput != "Cable B Output (capture)" {
		t.Errorf("the rest of the card was not saved: %+v", saved.GoogleVoiceInput)
	}
	var snap voiceControlSnapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatal(err)
	}
	if !snap.Config.Enabled {
		t.Error("the answer to the card save reported calling as off")
	}

	// And turning it off still works, through the switch.
	if code, body := voicePost(t, srv, "/enable", `{"enabled":false}`); code != http.StatusOK {
		t.Fatalf("/enable off returned %d: %s", code, body)
	}
	if loadVoiceCallConfig(dir).Enabled {
		t.Error("the switch could not turn calling off")
	}
}

// A window the user opened is not the supervisor's to close.
//
// The supervisor closed the Google Voice window on every tick where calling was
// switched off. Opening it to sign in to Google -- which is what you have to do
// before calling is any use -- therefore produced a window that vanished four
// seconds later, with nothing said. That is "it opens a popup and it closes
// right away", and it is a background loop doing it.
func TestTheSupervisorNeverClosesAWindowSomebodyAskedFor(t *testing.T) {
	// It starts one when calling is on and there is none.
	if !superviseVoiceShouldStart(true, false, voiceWindowStartup+time.Second) {
		t.Error("the supervisor did not start a missing window while calling was on")
	}
	// It does not stack a second one on top of a window that already exists.
	if superviseVoiceShouldStart(true, true, voiceWindowStartup+time.Second) {
		t.Error("the supervisor started a second window on top of one that exists")
	}
	// It does not start one every tick while the first is still coming up.
	if superviseVoiceShouldStart(true, false, time.Second) {
		t.Error("the supervisor started another window before the last had time to appear")
	}
	// And with calling off it does nothing at all -- neither starting a window
	// nor, crucially, taking one away.
	if superviseVoiceShouldStart(false, false, time.Hour) {
		t.Error("the supervisor started a window while calling was off")
	}

	// The decision has no "close" in it. Turning calling off is what closes the
	// window, at the moment it is turned off.
	source, err := os.ReadFile("voice_call_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(source), "\r\n", "\n")
	i := strings.Index(body, "func superviseGoogleVoice(")
	if i < 0 {
		t.Fatal("the supervisor is gone")
	}
	end := strings.Index(body[i:], "\n}\n")
	if end < 0 {
		t.Fatal("could not read the supervisor")
	}
	if strings.Contains(body[i:i+end], "voiceWMClose") {
		t.Error("the supervisor still closes windows on a timer")
	}
}
