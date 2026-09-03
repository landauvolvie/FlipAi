//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// This is the Google Voice environment inside FlipAi.
//
// It is FlipAi's own browser view -- the same Edge WebView2 runtime the rest of
// the app is drawn with -- created, owned and kept alive by FlipAi. There is no
// second browser to start, nothing to hand a profile to, and no external
// application whose windows can appear on the desktop. Google Voice is signed
// in once and stays loaded, ringing, dialling and holding calls, for as long as
// FlipAi is running.
//
// Two things watch the call, and neither of them decides anything:
//
//   - the script injected into the page (voice_page_script.go), which sees a
//     ring the instant the DOM changes;
//   - the control loop below, reading the page through WebView2's own
//     in-process DevTools call, which keeps working when the page's script has
//     been replaced by a navigation or has wedged. No port is opened for it and
//     nothing listens: this is the same process talking to its own view.
//
// Both report to the one call machine in voice_session.go. That is what makes
// an allowed caller reliably answered instead of occasionally answered.

const (
	// voiceControlInterval is how often the control loop reads the page. It is
	// fast enough to catch a ring the page script missed well inside the ~25
	// seconds before Google Voice gives up and takes the call to voicemail.
	voiceControlInterval = 600 * time.Millisecond
	// voiceDockInterval is how often the panel position is applied. Fast
	// enough that dragging the FlipAi window does not leave Google Voice
	// trailing behind it.
	voiceDockInterval = 120 * time.Millisecond
)

// runGoogleVoiceWindow creates the Google Voice view and does not return until
// it closes.
func runGoogleVoiceWindow(dataDir string, showInPanel bool) error {
	// A Win32 message pump only works on the thread that created the window.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := os.MkdirAll(voiceProfilePath(dataDir), 0700); err != nil {
		recordVoiceOpen(dataDir, "could not create the Google Voice browser profile folder", err)
		return err
	}

	w, err := createGoogleVoiceWebView(dataDir)
	if err != nil {
		recordVoiceOpen(dataDir, "WebView2 could not create the Google Voice view", err)
		return err
	}
	defer w.Destroy()
	hwnd := uintptr(w.Window())

	applyFlipAiWindowIcon(hwnd)
	w.SetSize(640, 480, webview2.HintMin)
	webViewVoicePermissions(voiceChromium(w))

	// The agents -- and with them the numbers allowed to call -- live in the
	// main configuration, which this process reads fresh so a change made in the
	// FlipAi window applies to the next call without a restart.
	mainConfig := func() Config {
		_, cfgPath, _, _, err := appPaths()
		if err != nil {
			return Config{}
		}
		cfg, err := loadConfig(cfgPath, dataDir)
		if err != nil {
			return Config{}
		}
		return cfg
	}

	// The DevTools channel is in-process: WebView2's own CallDevToolsProtocolMethod,
	// against this view and no other, with nothing listening on the machine.
	control := newVoiceControlChannel(newWebViewDevTools(w))

	bridge := newVoiceBridge(dataDir, mainConfig,
		func(cfg VoiceCallConfig, agent string) error {
			return startAgentVoiceSessionVerified(dataDir, cfg, agent)
		},
		stopAgentVoiceSession)
	bridge.route = func(cfg VoiceCallConfig, agent string) { routeAgentAppAudio(dataDir, cfg, agent) }
	bridge.press = control.pressAnswer
	// The bindings below run on this window's message thread. Starting a
	// desktop voice session takes seconds, so it must not happen there.
	bridge.RunEffectsInBackground()

	_ = w.Bind("flipVoiceAudioSettings", bridge.AudioSettings)
	_ = w.Bind("flipVoiceIncoming", bridge.Incoming)
	_ = w.Bind("flipVoiceAnswered", bridge.Answered)
	_ = w.Bind("flipVoiceEnded", bridge.Ended)
	_ = w.Bind("flipVoiceDevices", bridge.Devices)
	_ = w.Bind("flipVoicePage", bridge.Page)

	w.Init(googleVoiceInitScript)

	// The one thing that does listen is FlipAi's own endpoint, so the host
	// process can ask this one to send an image through the signed-in Google
	// Voice session it owns. It is loopback-only and needs FlipAi's local token.
	port, endpoint := startVoiceWindowEndpoint(dataDir, control)
	if endpoint != nil {
		defer endpoint.Close()
	}

	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.BrowserRunning = true
		s.ControlPort = port
		s.LastError = ""
		s.LastEvent = "browser-starting"
	})

	// The view was created parked, as a tool window, without focus. This only
	// records that, and takes the title bar off the frame the binding gave it.
	dock := newVoiceDockController(hwnd)
	dock.park()
	if showInPanel {
		// The panel is what the user asked for, and the Connections page is
		// already reporting where it is; the dock loop below picks it up on its
		// first tick. Nothing is brought to the front, because there is no
		// separate window to bring anywhere.
		recordVoiceOpen(dataDir, "Google Voice is running inside FlipAi", nil)
	}

	stop := make(chan struct{})
	defer close(stop)
	go runVoiceReceiverLoops(dataDir, hwnd, bridge, control, dock, stop)

	w.Navigate(googleVoiceWebURL)
	w.Run()

	// The window is gone. Anything it started has to go with it, or the desktop
	// app is left listening to a cable nobody is talking into -- and this
	// process exits as soon as this function returns, so the teardown is waited
	// for rather than merely queued.
	bridge.Ended()
	bridge.Drain(15 * time.Second)
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.BrowserRunning = false
		s.ControlPort = 0
		s.ControlToken = ""
		s.Docked = false
		s.LastEvent = "browser-closed"
	})
	return nil
}

// runVoiceReceiverLoops is everything that happens while the window is up: the
// panel position, the independent view of the call, and the quit watch.
func runVoiceReceiverLoops(dataDir string, hwnd uintptr, bridge *voiceBridge, control *voiceControlChannel, dock *voiceDockController, stop <-chan struct{}) {
	dockTicker := time.NewTicker(voiceDockInterval)
	defer dockTicker.Stop()
	controlTicker := time.NewTicker(voiceControlInterval)
	defer controlTicker.Stop()

	quitCheck := 0
	lastDevices := ""

	for {
		select {
		case <-stop:
			return
		case <-dockTicker.C:
			quitCheck++
			if quitCheck >= 4 {
				quitCheck = 0
				if quitRequested(dataDir) {
					procVoicePostMessage.Call(hwnd, voiceWMClose, 0, 0)
					return
				}
			}
			req := loadVoiceDock(dataDir)
			docked := dock.Apply(req, time.Now(), voiceDockOwner())
			mutateVoiceDockState(dataDir, docked, dock.blocked)

		case <-controlTicker.C:
			snap, err := control.snapshot()
			if err != nil {
				// The page could not be read. That is never a statement that
				// the call is over -- see voiceObservation.Unreadable.
				bridge.Observe(voiceObservation{Unreadable: true})
				continue
			}
			bridge.Page(snap.Href, snap.SignedIn, joinControls(snap.Controls))
			if len(snap.Devices) > 0 {
				if raw := marshalDevices(snap.Devices); raw != "" && raw != lastDevices {
					lastDevices = raw
					bridge.Devices(raw)
				}
			}
			bridge.Observe(snap.observation())
		}
	}
}

func joinControls(names []string) string {
	out := ""
	for _, n := range names {
		if out != "" {
			out += " | "
		}
		out += n
		if len(out) > 2000 {
			break
		}
	}
	return out
}

func marshalDevices(devices []VoiceAudioDevice) string {
	b, err := json.Marshal(devices)
	if err != nil {
		return ""
	}
	return string(b)
}

// voiceControlChannel is FlipAi's own view of its own Google Voice page.
type voiceControlChannel struct {
	devTools voiceDevTools
}

func newVoiceControlChannel(d voiceDevTools) *voiceControlChannel {
	if d == nil {
		return &voiceControlChannel{}
	}
	return &voiceControlChannel{devTools: d}
}

func (c *voiceControlChannel) available() bool { return c != nil && c.devTools != nil }

func (c *voiceControlChannel) snapshot() (voicePageSnapshot, error) {
	if !c.available() {
		return voicePageSnapshot{}, errNoVoiceControlChannel
	}
	return voiceReadSnapshot(c.devTools)
}

// pressAnswer is the answer ladder. Rung 1 is the page's own click, rung 2 is a
// real mouse press delivered through the browser's input pipeline, rung 3 is
// the Windows accessibility Invoke -- the same action a screen reader performs,
// which reaches a control the page will not let a script touch.
func (c *voiceControlChannel) pressAnswer(effect voiceCallEffect) error {
	switch effect.Attempt {
	case 1:
		if !c.available() {
			return errNoVoiceControlChannel
		}
		pressed, err := voiceClickAnswerScripted(c.devTools)
		if err != nil {
			return err
		}
		if !pressed {
			return errors.New("the ringing card has no Answer control on it yet")
		}
		return nil
	case 2:
		if !c.available() {
			return errNoVoiceControlChannel
		}
		pressed, err := voiceClickAnswerTrusted(c.devTools)
		if err != nil {
			return err
		}
		if !pressed {
			return errors.New("the ringing card has no Answer control to aim at yet")
		}
		return nil
	default:
		return invokeGoogleVoiceAnswerAccessibly()
	}
}

// invokeGoogleVoiceAnswerAccessibly presses Answer through Windows UI
// Automation, without coordinates, in the window currently showing Google
// Voice. It is deliberately coordinate-free: the panel can be docked, resized,
// moved or shown at a different display scale and the same accessible control
// is still the one pressed.
func invokeGoogleVoiceAnswerAccessibly() error {
	hwnd := googleVoiceHWND()
	if hwnd == 0 {
		return errors.New("the Google Voice window is not available")
	}
	script, err := googleVoiceAnswerUIAScript(hwnd)
	if err != nil {
		return err
	}
	state, err := runVoicePowerShell(script)
	if err != nil {
		return err
	}
	switch {
	case !state.Found:
		return errors.New("Windows accessibility cannot read the Google Voice window")
	case state.Result == "clicked":
		return nil
	case state.Result == "not-found":
		return fmt.Errorf("the incoming call is visible but Windows accessibility found no Answer control; it offered: %s", truncate(strings.Join(state.Controls, ", "), 240))
	}
	return errors.New("Windows refused to press the Answer control")
}

// platformVoiceControlEndpoint is the loopback endpoint the running Google
// Voice process serves, for the host process. The port is 0 when there is none.
func platformVoiceControlEndpoint(dataDir string) (int, string) {
	rt := loadVoiceRuntime(dataDir)
	return rt.ControlPort, rt.ControlToken
}

// startVoiceWindowEndpoint opens the one loopback endpoint the Google Voice
// process serves.
//
// It exists because WebView2 is reached in-process: there is no port for the
// FlipAi host to connect to, so when the host has an image to send it asks this
// process to send it. The endpoint is bound to 127.0.0.1 and every request has
// to carry FlipAi's own local token, so nothing else on the machine can drive
// the signed-in Google Voice session.
//
// It returns the port it is listening on, which goes into the runtime state
// file for the host to find, and 0 when no endpoint could be opened -- in which
// case calls still work and only sending an image does not.
func startVoiceWindowEndpoint(dataDir string, control *voiceControlChannel) (int, io.Closer) {
	if !control.available() {
		return 0, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.RoutingNote = "FlipAi could not open its local Google Voice endpoint, so sending an image over Google Voice is unavailable: " + err.Error()
		})
		return 0, nil
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// The endpoint's own secret, made here and written down beside its port.
	// Borrowing the main configuration's token would mean depending on a file
	// another process creates, and racing it on a first run.
	token, err := secureRandomToken(24)
	if err != nil {
		_ = listener.Close()
		return 0, nil
	}
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) { s.ControlToken = token })

	mux := http.NewServeMux()
	authorized := func(r *http.Request) bool {
		// A token that is not set cannot be checked, and an endpoint that
		// cannot check is one that does not answer.
		return token != "" && r.Header.Get("X-FlipAi-Token") == token
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(w, "FlipAi local token required", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	// /probe proves the in-process DevTools channel actually works on this
	// machine, which is what the second rung of the answer ladder and sending an
	// image both depend on. Constructing the channel proves nothing; making a
	// call through it does.
	mux.HandleFunc("/probe", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(w, "FlipAi local token required", http.StatusForbidden)
			return
		}
		var answer int
		if err := voiceEval(control.devTools, "1+1", false, &answer); err != nil {
			http.Error(w, "the Google Voice page did not answer: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if answer != 2 {
			http.Error(w, "the Google Voice page answered nonsense", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/mms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if !authorized(r) {
			http.Error(w, "FlipAi local token required", http.StatusForbidden)
			return
		}
		var body struct {
			Phone   string `json:"phone"`
			Caption string `json:"caption"`
			Image   string `json:"image"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
			http.Error(w, "could not read the request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := sendGoogleVoiceMMSInPage(control.devTools, body.Phone, body.Caption, body.Image); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 150 * time.Second}
	go func() { _ = server.Serve(listener) }()
	return port, server
}
