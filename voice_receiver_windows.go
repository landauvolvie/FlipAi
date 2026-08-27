//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
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
//   - the control loop below, reading the page through FlipAi's loopback
//     DevTools channel, which keeps working when the page's own script has been
//     replaced by a navigation or has wedged.
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

	// The control channel's port is chosen before the view is created, because
	// it is a browser argument. A machine that cannot spare a loopback port
	// still gets a working Google Voice window; it just loses the second way of
	// pressing Answer and the ability to send an MMS.
	port, portErr := freeLoopbackPort()
	if portErr != nil {
		port = 0
	}

	w, port, err := createGoogleVoiceWebView(dataDir, port)
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

	control := newVoiceControlChannel(port)
	defer control.close()

	bridge := newVoiceBridge(dataDir, mainConfig,
		func(cfg VoiceCallConfig, agent string) error { return startAgentVoiceSession(dataDir, cfg, agent) },
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
	granted := false
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
			if !granted {
				// Granting at the browser level is what makes Google Voice
				// treat this window as a browser that can take calls at all,
				// and it can only be done once a page exists.
				granted = control.grantPermissions() == nil
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

// voiceControlChannel is FlipAi's loopback view of its own Google Voice page.
// It reconnects on its own: the socket is dropped by every page navigation, and
// a channel that stayed dead after the first sign-in would be no channel at all.
type voiceControlChannel struct {
	port int

	// The observation loop reads the page while the answer queue presses
	// Answer, on two different goroutines. Both go through the one socket, so
	// both go through this lock.
	mu     sync.Mutex
	client *voiceCDPClient
}

func newVoiceControlChannel(port int) *voiceControlChannel {
	return &voiceControlChannel{port: port}
}

func (c *voiceControlChannel) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropLocked()
}

func (c *voiceControlChannel) connected() (*voiceCDPClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port <= 0 {
		return nil, errors.New("the Google Voice control channel is not available on this PC")
	}
	if c.client != nil {
		return c.client, nil
	}
	client, err := connectVoiceCDP(c.port)
	if err != nil {
		return nil, err
	}
	c.client = client
	return client, nil
}

// drop closes the socket after a failure. Every page navigation ends the old
// connection, so this is the ordinary case rather than an exceptional one.
func (c *voiceControlChannel) drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropLocked()
}

func (c *voiceControlChannel) dropLocked() {
	if c.client != nil {
		c.client.close()
		c.client = nil
	}
}

func (c *voiceControlChannel) snapshot() (voicePageSnapshot, error) {
	client, err := c.connected()
	if err != nil {
		return voicePageSnapshot{}, err
	}
	snap, err := client.voiceSnapshot()
	if err != nil {
		c.drop()
		return voicePageSnapshot{}, err
	}
	return snap, nil
}

func (c *voiceControlChannel) grantPermissions() error {
	client, err := c.connected()
	if err != nil {
		return err
	}
	if err := client.grantVoicePermissions(); err != nil {
		c.drop()
		return err
	}
	return nil
}

// pressAnswer is the answer ladder. Rung 1 is the page's own click, rung 2 is a
// real mouse press delivered through the browser's input pipeline, rung 3 is
// the Windows accessibility Invoke -- the same action a screen reader performs,
// which reaches a control the page will not let a script touch.
func (c *voiceControlChannel) pressAnswer(effect voiceCallEffect) error {
	switch effect.Attempt {
	case 1:
		client, err := c.connected()
		if err != nil {
			return err
		}
		pressed, err := client.clickAnswerScripted()
		if err != nil {
			c.drop()
			return err
		}
		if !pressed {
			return errors.New("the ringing card has no Answer control on it yet")
		}
		return nil
	case 2:
		client, err := c.connected()
		if err != nil {
			return err
		}
		pressed, err := client.clickAnswerTrusted()
		if err != nil {
			c.drop()
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

// platformVoiceControlPort is the loopback DevTools port of the running Google
// Voice window, for the host process. It is 0 when there is no window.
func platformVoiceControlPort(dataDir string) int {
	return loadVoiceRuntime(dataDir).ControlPort
}

var errNoVoiceControlChannel = errors.New("the Google Voice window has no control channel open")
