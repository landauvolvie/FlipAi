//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
)

const googleVoiceWindowTitle = "FlipAi — Google Voice"

var (
	voiceKernel32                 = syscall.NewLazyDLL("kernel32.dll")
	voiceUser32                   = syscall.NewLazyDLL("user32.dll")
	procVoiceCreateMutex          = voiceKernel32.NewProc("CreateMutexW")
	procVoiceCloseHandle          = voiceKernel32.NewProc("CloseHandle")
	procVoiceProcessIDToSessionID = voiceKernel32.NewProc("ProcessIdToSessionId")
	procVoiceFindWindow           = voiceUser32.NewProc("FindWindowW")
	procVoiceShowWindow           = voiceUser32.NewProc("ShowWindow")
	procVoiceSetForegroundWindow  = voiceUser32.NewProc("SetForegroundWindow")
	procVoicePostMessage          = voiceUser32.NewProc("PostMessageW")
	procVoiceEnumWindows          = voiceUser32.NewProc("EnumWindows")
	procVoiceIsWindowVisible      = voiceUser32.NewProc("IsWindowVisible")
	procVoiceGetWindowTextLength  = voiceUser32.NewProc("GetWindowTextLengthW")
	procVoiceGetWindowText        = voiceUser32.NewProc("GetWindowTextW")
	procVoiceGetForegroundWindow  = voiceUser32.NewProc("GetForegroundWindow")
	procVoiceSetLastError         = voiceKernel32.NewProc("SetLastError")
	procVoiceDestroyWindow        = voiceUser32.NewProc("DestroyWindow")
)

const (
	voiceSWHide     = 0
	voiceSWMinimize = 6
	// SW_SHOWMINNOACTIVE: minimized, no animation, no focus change.
	voiceSWShowMinNoActive = 7
	voiceSWRestore         = 9
	voiceWMClose           = 0x0010
)

// Voice mode has a dedicated process because Google Voice must stay alive even
// when the FlipAi settings window is closed. The process owns a persistent
// WebView2 profile, while the normal FlipAi host owns the small local settings
// endpoint. A boot-time session-0 host cannot create an interactive WebView;
// the per-user startup invocation below starts it as soon as the Windows user
// signs in, after which locking the PC does not stop it.
func init() {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	dataDir, cfgPath, statePath, _, err := appPaths()
	if err != nil {
		return
	}
	switch mode {
	case "--google-voice":
		_ = ensureDataDir(dataDir)
		visible := len(os.Args) > 2 && os.Args[2] == "--visible"
		if err := runGoogleVoiceProcess(dataDir, visible); err != nil {
			recordVoiceOpen(dataDir, "the Google Voice window process stopped", err)
		}
		os.Exit(0)
	case "--host":
		startVoiceControlServer(dataDir, cfgPath, statePath)
		if voiceInteractiveSession() {
			go superviseGoogleVoice(dataDir)
		}
	case "--watchdog":
		// This runs before the watchdog's duplicate-instance check. That is
		// intentional: if a boot task already owns the watchdog in session 0,
		// the HKCU Run entry at interactive sign-in still gets one chance to
		// create the Google Voice window in the user's desktop session.
		if voiceInteractiveSession() && loadVoiceCallConfig(dataDir).Enabled {
			_ = platformOpenGoogleVoice(dataDir, false)
		}
	}
}

func voiceInteractiveSession() bool {
	var sid uint32
	r, _, _ := procVoiceProcessIDToSessionID.Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&sid)))
	return r != 0 && sid != 0
}

func acquireGoogleVoiceInstance() (func(), bool) {
	name, _ := syscall.UTF16PtrFromString(`Local\FlipAi-GoogleVoice`)
	// Whether this process owns the window is decided by GetLastError, and
	// CreateMutexW does not reset it on a clean creation. Without clearing it
	// first, a leftover ERROR_ALREADY_EXISTS from any earlier call in this
	// process makes a brand new instance believe another one already owns the
	// window -- after which it exits without creating anything, silently.
	procVoiceSetLastError.Call(0)
	h, _, callErr := procVoiceCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return func() {}, false
	}
	// ERROR_ALREADY_EXISTS = 183. syscall returns that value even though the
	// handle itself is valid, so close our duplicate and let the first process
	// continue owning the window.
	if errno, ok := callErr.(syscall.Errno); ok && errno == 183 {
		procVoiceCloseHandle.Call(h)
		return func() {}, false
	}
	return func() { procVoiceCloseHandle.Call(h) }, true
}

// waitForGoogleVoiceWindow polls for the window because it is created by another
// process: there is no handle to wait on, only its appearance.
func waitForGoogleVoiceWindow(d time.Duration) uintptr {
	deadline := time.Now().Add(d)
	for {
		if h := googleVoiceHWND(); h != 0 {
			return h
		}
		if !time.Now().Before(deadline) {
			return 0
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func googleVoiceHWND() uintptr {
	title, _ := syscall.UTF16PtrFromString(googleVoiceWindowTitle)
	h, _, _ := procVoiceFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
	return h
}

// platformOpenGoogleVoice does not return until the window is actually on screen
// or it can say why it is not.
//
// It used to return as soon as a child process had been launched, which made
// every later failure invisible: the window process could refuse to start,
// fail to create a WebView, or exit immediately, and the click still reported
// success. "Open Google Voice does nothing" was that gap.
func platformOpenGoogleVoice(dataDir string, show bool) error {
	started := time.Now()
	if h := googleVoiceHWND(); h != 0 {
		if !show {
			return nil
		}
		return revealGoogleVoiceWindow(dataDir, h)
	}
	if !voiceInteractiveSession() {
		err := errors.New("Google Voice needs a signed-in Windows desktop session; it cannot open at the Windows sign-in screen or from a service")
		recordVoiceOpen(dataDir, "no interactive desktop session", err)
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		recordVoiceOpen(dataDir, "could not locate FlipAi.exe", err)
		return err
	}
	args := []string{"--google-voice"}
	if show {
		args = append(args, "--visible")
	}
	beginVoiceOpen(dataDir, "starting the Google Voice window process")
	if err := spawnDetached(exe, args...); err != nil {
		recordVoiceOpen(dataDir, "could not start the Google Voice window process", err)
		return fmt.Errorf("could not start the Google Voice window process: %w", err)
	}
	if !show {
		return nil
	}

	h := waitForGoogleVoiceWindow(voiceWindowStartup)
	if h == 0 {
		reason := lastVoiceOpenFailure(dataDir, started)
		if reason == "" {
			reason = "the window process started but never created a window."
			if platformWebView2Runtime() == "" {
				reason += " The Microsoft Edge WebView2 Runtime is not installed on this PC, and FlipAi cannot show Google Voice without it. Install Microsoft's free Evergreen Standalone Installer, then try again."
			}
		}
		err := fmt.Errorf("Google Voice did not open: %s", reason)
		recordVoiceOpen(dataDir, "window never appeared", err)
		return err
	}
	if err := confirmGoogleVoiceLoaded(dataDir, started); err != nil {
		return err
	}
	return revealGoogleVoiceWindow(dataDir, h)
}

// confirmGoogleVoiceLoaded is the difference between a window and Google Voice.
//
// The frame is a plain Win32 window, created and shown in a few hundred
// milliseconds; WebView2 is embedded into it afterwards, and that step can
// fail. Anything watching only for the window therefore sees success almost
// immediately -- and if the embed then fails, the window vanishes with the
// process that owned it. That is precisely what "it opens a window, it closes
// straight away, and then it says it is closed" was, with a success message on
// top of it.
//
// So a window is not the answer on its own. This waits for the window process
// to say Google Voice is actually running, for it to record why it could not,
// or for it to be gone -- whichever happens first. A process that is simply
// slow is not a failure: if it is still alive at the deadline, the window it
// has already put on screen is taken at face value.
func confirmGoogleVoiceLoaded(dataDir string, started time.Time) error {
	deadline := started.Add(voiceWindowStartup)
	for {
		if reason := lastVoiceOpenFailure(dataDir, started); reason != "" {
			return errors.New(reason)
		}
		if s := loadVoiceRuntime(dataDir); s.BrowserRunning {
			return nil
		}
		if !googleVoiceProcessAlive() {
			reason := lastVoiceOpenFailure(dataDir, started)
			if reason == "" {
				reason = "the Google Voice window process stopped before Google Voice finished loading."
				if v := platformWebView2Runtime(); v == "" {
					reason += " The Microsoft Edge WebView2 Runtime is not installed on this PC, and FlipAi cannot show Google Voice without it."
				} else {
					reason += " Windows could not embed the Edge WebView2 Runtime " + v + " in it. Restarting Windows usually clears this; if it persists, repair the WebView2 Runtime from Installed apps."
				}
			}
			err := errors.New(reason)
			recordVoiceOpen(dataDir, "window process stopped", err)
			return err
		}
		if !time.Now().Before(deadline) {
			// Alive, with a window on screen, just slow. Nothing to report.
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// googleVoiceProcessAlive reports whether a window process still holds the
// single-instance mutex. It is the one liveness answer available to a process
// that did not create the child and so holds no handle to it.
func googleVoiceProcessAlive() bool {
	release, acquired := acquireGoogleVoiceInstance()
	if acquired {
		// Nobody held it, which means nobody is running. Let go again at once so
		// the next window process is not locked out by this check.
		release()
		return false
	}
	return true
}

// revealGoogleVoiceWindow un-minimizes the window and puts it in front. Windows
// often refuses a foreground change asked for by a background process, and it
// refuses silently, so a window that only restored behind everything else is
// reported rather than treated as a success.
func revealGoogleVoiceWindow(dataDir string, hwnd uintptr) error {
	procVoiceShowWindow.Call(hwnd, voiceSWRestore)
	if bringToFront(hwnd) {
		recordVoiceOpen(dataDir, "window opened", nil)
		return nil
	}
	recordVoiceOpen(dataDir, "window opened behind other windows", nil)
	return nil
}

// platformEnsureGoogleVoice makes sure the window process is running, without
// putting anything on screen. It is what the Connections page calls when it
// wants the embedded panel: the window appears docked inside the app, so there
// is nothing to press and no popup to dismiss.
var voiceEnsureMu sync.Mutex
var voiceEnsureAt time.Time

func platformEnsureGoogleVoice(dataDir string) {
	if googleVoiceHWND() != 0 {
		return
	}
	// The page asks for the panel several times a second, and a first run has
	// to unpack WebView2 before any window exists. Without this the wait for
	// that first window would be spent starting a new window process four
	// times a second, every one of which would find the mutex held and exit.
	voiceEnsureMu.Lock()
	if time.Since(voiceEnsureAt) < voiceWindowStartup {
		voiceEnsureMu.Unlock()
		return
	}
	voiceEnsureAt = time.Now()
	voiceEnsureMu.Unlock()
	go startGoogleVoiceInBackground(dataDir)
}

// startGoogleVoiceInBackground starts the window nobody is waiting on, and then
// waits on it anyway.
//
// A background start used to return the moment the process was launched, so
// when Google Voice failed to load there was nothing to report and nothing to
// look at: the panel simply stayed empty. The user is looking at that panel, so
// the outcome has to reach it.
func startGoogleVoiceInBackground(dataDir string) {
	started := time.Now()
	if err := platformOpenGoogleVoice(dataDir, false); err != nil {
		return
	}
	if h := waitForGoogleVoiceWindow(voiceWindowStartup); h == 0 {
		reason := lastVoiceOpenFailure(dataDir, started)
		if reason == "" {
			reason = "the Google Voice window process started but never created a window."
			if platformWebView2Runtime() == "" {
				reason += " The Microsoft Edge WebView2 Runtime is not installed on this PC, and FlipAi cannot show Google Voice without it."
			}
		}
		recordVoiceOpen(dataDir, "window never appeared", errors.New(reason))
		return
	}
	_ = confirmGoogleVoiceLoaded(dataDir, started)
}

// platformRestartGoogleVoice is the Retry the panel offers. It takes down
// whatever is there -- a window that never loaded, or a process wedged behind
// the single-instance mutex -- and starts again from nothing.
func platformRestartGoogleVoice(dataDir string) {
	if h := googleVoiceHWND(); h != 0 {
		procVoicePostMessage.Call(h, voiceWMClose, 0, 0)
		deadline := time.Now().Add(6 * time.Second)
		for googleVoiceHWND() != 0 && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
	}
	// A retry is the user saying "try now", so the rate limit that keeps the
	// page from starting a process four times a second does not apply.
	voiceEnsureMu.Lock()
	voiceEnsureAt = time.Time{}
	voiceEnsureMu.Unlock()
	go startGoogleVoiceInBackground(dataDir)
}

func platformVoiceConfigChanged(dataDir string, cfg VoiceCallConfig) {
	if cfg.Enabled {
		_ = platformOpenGoogleVoice(dataDir, false)
		return
	}
	if h := googleVoiceHWND(); h != 0 {
		procVoicePostMessage.Call(h, voiceWMClose, 0, 0)
	}
}

func superviseGoogleVoice(dataDir string) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	// A window takes a while to appear on a cold start, and the supervisor
	// cannot see one until it does. Without this the supervisor would launch a
	// fresh process every four seconds for the whole of that startup.
	var lastAttempt time.Time
	for range ticker.C {
		if quitRequested(dataDir) {
			return
		}
		cfg := loadVoiceCallConfig(dataDir)
		if superviseVoiceShouldStart(cfg.Enabled, googleVoiceHWND() != 0, time.Since(lastAttempt)) {
			lastAttempt = time.Now()
			_ = platformOpenGoogleVoice(dataDir, false)
		}
	}
}

func runGoogleVoiceProcess(dataDir string, initiallyVisible bool) error {
	release, owner := acquireGoogleVoiceInstance()
	if !owner {
		return runGoogleVoiceProcessAsSecond(dataDir, initiallyVisible)
	}
	defer release()

	visible := initiallyVisible
	for {
		if quitRequested(dataDir) {
			return nil
		}
		cfg := loadVoiceCallConfig(dataDir)
		// A user may press Open before turning the feature on. That one visible
		// window is allowed for setup/sign-in, but a closed disabled window is
		// never resurrected in the background.
		if !cfg.Enabled && !visible {
			return nil
		}
		if err := runGoogleVoiceWindow(dataDir, visible); err != nil {
			mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
				s.BrowserRunning = false
				s.LastError = err.Error()
				s.LastEvent = "browser-error"
			})
			return err
		}
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.BrowserRunning = false
			s.InCall = false
			s.Caller = ""
			s.Agent = ""
			s.LastEvent = "browser-closed"
		})
		// A sign-out closes the window precisely so the browser profile can be
		// deleted here, while nothing holds it; the loop then opens a fresh,
		// signed-out window.
		consumePendingVoiceSignOut(dataDir)
		visible = false
		if !loadVoiceCallConfig(dataDir).Enabled || quitRequested(dataDir) {
			return nil
		}
		// Closing the call window should not silently turn the always-listening
		// feature off. Recreate it minimized while the optional feature remains on.
		time.Sleep(1500 * time.Millisecond)
	}
}

// runGoogleVoiceProcessAsSecond is what a --google-voice invocation does when
// another process already owns the window.
func runGoogleVoiceProcessAsSecond(dataDir string, initiallyVisible bool) error {
	// Another instance holds the window. A background respawn has nothing to
	// do and nobody waiting on it, so it just stands down.
	if !initiallyVisible {
		return nil
	}
	// A user is waiting, though. The other instance normally has a window
	// already, or is seconds away from one; if it never produces one it is
	// wedged, and returning quietly here is what made repeated clicks on
	// Open do nothing at all, forever -- the caller saw a process start and
	// exit cleanly, with no window and no complaint.
	h := waitForGoogleVoiceWindow(voiceWindowStartup)
	if h == 0 {
		err := errors.New("another FlipAi Google Voice process is already running but has not produced a window; quit FlipAi from the tray and start it again")
		recordVoiceOpen(dataDir, "an existing window process is not responding", err)
		return err
	}
	return revealGoogleVoiceWindow(dataDir, h)
}

// voiceBrowserArguments are the Chromium switches the Google Voice window is
// created with.
//
// FlipAi keeps that window minimized, and a minimized window is one Chromium
// treats as hidden: it counts as occluded, its renderer is backgrounded, and
// its timers are first throttled to once a second and then, after a few
// minutes, to once a minute. The bridge polls for a ringing call about once a
// second, and a call stops ringing in well under a minute, so a window left
// running in the background quietly stopped noticing calls at all. Autoplay is
// relaxed for the same reason: the call audio element is started without a
// click, in a window that never has focus.
//
// This binding passes no environment options to WebView2, and WebView2 reads
// this variable when none are given, so it is how these switches get through.
const voiceBrowserArguments = "--disable-background-timer-throttling " +
	"--disable-backgrounding-occluded-windows " +
	"--disable-renderer-backgrounding " +
	"--disable-features=CalculateNativeWinOcclusion,IntensiveWakeUpThrottling " +
	"--autoplay-policy=no-user-gesture-required " +
	"--disable-popup-blocking"

// createGoogleVoiceWebView makes the window, and keeps trying when the first
// way does not work.
//
// Creating a WebView2 fails as a single silent "no": the binding returns nil
// whether the runtime is missing, the browser arguments were refused, or the
// profile folder was still held by a copy of this process that had not quite
// finished exiting. Those last two are recoverable, and a user watching an
// empty panel has no way to tell any of them apart, so each is tried in turn
// and what worked is written down.
//
// The binding creates and shows its Win32 frame before embedding the browser
// into it, and does not take that frame away when embedding fails. Left alone,
// the leftover frame answers to the window title FlipAi looks for, so the next
// attempt would find "a window" that can never load anything. Each failed
// attempt therefore destroys its own frame first -- from this thread, which is
// the thread that created it.
func createGoogleVoiceWebView(dataDir string) (webview2.WebView, error) {
	ways := googleVoiceRenderModes()
	// A previous window that came up black is remembered, so Retry moves on to
	// the next way of drawing rather than repeating the one that did not work.
	start := loadVoiceRuntime(dataDir).RenderAttempt
	if start < 0 || start >= len(ways) {
		start = 0
	}
	var lastErr error
	for i := 0; i < len(ways); i++ {
		way := ways[(start+i)%len(ways)]
		if way.wait > 0 {
			time.Sleep(way.wait)
		}
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", way.args)
		w := webview2.NewWithOptions(webview2.WebViewOptions{
			Debug:     false,
			AutoFocus: true,
			DataPath:  voiceProfilePath(dataDir),
			WindowOptions: webview2.WindowOptions{
				Title:  googleVoiceWindowTitle,
				Width:  1080,
				Height: 760,
				Center: true,
			},
		})
		if w != nil {
			mode, note := way.name, way.note
			mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
				s.RenderMode = mode
				s.RenderAttempt = (start + i) % len(ways)
				if note != "" {
					s.LastError = note
				}
			})
			return w, nil
		}
		destroyLeftoverGoogleVoiceFrame()
		lastErr = webView2CreateFailure(i)
	}
	return nil, lastErr
}

type googleVoiceRenderMode struct {
	name string
	note string
	args string
	wait time.Duration
}

// googleVoiceRenderModes are the ways FlipAi knows to draw Google Voice, best
// first.
//
// A WebView2 that is created successfully and then paints nothing but black is
// a GPU problem, not a startup problem, and it is the normal outcome over
// Remote Desktop, where there is no GPU to composite with. Software rendering
// draws a phone page perfectly well, so on a remote desktop that is where this
// starts. Retry walks along this list, so a window that comes up black is one
// press away from a window that does not.
func googleVoiceRenderModes() []googleVoiceRenderMode {
	gpu := googleVoiceRenderMode{name: "hardware", args: voiceBrowserArguments}
	software := googleVoiceRenderMode{
		name: "software",
		note: "Google Voice is drawing without the graphics card. That is normal over Remote Desktop, where hardware drawing paints a black window.",
		args: voiceBrowserArguments + " --disable-gpu --disable-gpu-compositing",
	}
	// WebView2 refuses to start at all if it does not like a switch, and it does
	// not say which. A window that throttles in the background is still a window
	// that can be signed in and can take a call.
	plain := googleVoiceRenderMode{
		name: "plain",
		note: "Google Voice started without FlipAi's background-timer switches, because WebView2 refused them. Call pop-ups remain enabled. If one is ever missed while FlipAi is in the background, this render mode is the first thing to look at.",
		args: "--disable-popup-blocking",
	}
	// A copy of this process that was killed a moment ago can still hold the
	// browser profile for a second or two.
	patient := googleVoiceRenderMode{
		name: "retry",
		note: "Google Voice started after waiting for a previous FlipAi window to let go of the browser profile.",
		args: voiceBrowserArguments,
		wait: 3 * time.Second,
	}
	if remoteSession() {
		return []googleVoiceRenderMode{software, gpu, plain, patient}
	}
	return []googleVoiceRenderMode{gpu, software, plain, patient}
}

// webView2CreateFailure says what a failed creation means in words the person
// looking at an empty panel can act on.
func webView2CreateFailure(attempt int) error {
	v := platformWebView2Runtime()
	if v == "" {
		return errors.New("Windows could not create the Google Voice browser window: the Microsoft Edge WebView2 Runtime is not installed on this PC. Microsoft distributes it free as the Evergreen Standalone Installer; install it, then press Retry.")
	}
	if attempt < 2 {
		return fmt.Errorf("Windows could not create the Google Voice browser window with the Edge WebView2 Runtime %s.", v)
	}
	return fmt.Errorf("Windows could not create the Google Voice browser window even though the Edge WebView2 Runtime %s is installed, and it did not work without FlipAi's browser switches or after waiting for a previous window to let go of the browser profile. Restarting Windows usually clears this; if it persists, repair the WebView2 Runtime from Installed apps.", v)
}

// destroyLeftoverGoogleVoiceFrame removes the empty frame a failed embedding
// leaves behind, so it cannot be mistaken for a working Google Voice window.
func destroyLeftoverGoogleVoiceFrame() {
	if h := googleVoiceHWND(); h != 0 {
		procVoiceDestroyWindow.Call(h)
	}
}

func runGoogleVoiceWindow(dataDir string, visible bool) error {
	// A Win32 message pump only works on the thread that created the window.
	// This currently runs inside init(), where the Go runtime happens to hold
	// the main thread, but that is an implementation detail of the runtime and
	// not something the call bridge should depend on staying true.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(voiceProfilePath(dataDir), 0700); err != nil {
		recordVoiceOpen(dataDir, "could not create the Google Voice browser profile folder", err)
		return err
	}
	w, err := createGoogleVoiceWebView(dataDir)
	if err != nil {
		recordVoiceOpen(dataDir, "WebView2 could not create the window", err)
		return err
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(760, 560, webview2.HintMin)
	// Google Voice needs microphone and notification permission before it will
	// behave as a browser that can place and receive calls. FlipAi carries a
	// one-line local fix for the WebView2 binding's permission-kind callback,
	// so these can now be granted explicitly instead of using a global allow.
	// The embedded window therefore never hides a permission prompt the user
	// cannot reach, while unrelated capabilities stay denied.
	if chromium := voiceChromium(w); chromium != nil {
		chromium.SetPermission(edge.CoreWebView2PermissionKindMicrophone, edge.CoreWebView2PermissionStateAllow)
		chromium.SetPermission(edge.CoreWebView2PermissionKindNotifications, edge.CoreWebView2PermissionStateAllow)
		chromium.SetPermission(edge.CoreWebView2PermissionKindCamera, edge.CoreWebView2PermissionStateDeny)
		chromium.SetPermission(edge.CoreWebView2PermissionKindGeolocation, edge.CoreWebView2PermissionStateDeny)
		chromium.SetPermission(edge.CoreWebView2PermissionKindOtherSensors, edge.CoreWebView2PermissionStateDeny)
		chromium.SetPermission(edge.CoreWebView2PermissionKindClipboardRead, edge.CoreWebView2PermissionStateDeny)
	} else {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.LastError = "FlipAi could not configure Google Voice microphone and notification permissions in WebView2. Restart the Google Voice window before testing calls."
		})
	}

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
	// A call is answered by the desktop AI app itself: its per-app audio is
	// pointed at the cables just before its voice mode is started, so the
	// caller talks to the same agent that can actually work on this PC.
	activate := func(cfg VoiceCallConfig, agent string) error {
		return activateAgentVoiceWithRouting(dataDir, cfg, agent)
	}
	bridge := newVoiceBridge(dataDir, mainConfig, activate, deactivateAgentVoice)
	_ = w.Bind("flipVoiceAudioSettings", bridge.AudioSettings)
	_ = w.Bind("flipVoiceIncoming", bridge.Incoming)
	_ = w.Bind("flipVoiceAnswered", bridge.Answered)
	_ = w.Bind("flipVoiceEnded", bridge.Ended)
	_ = w.Bind("flipVoiceDevices", bridge.Devices)
	_ = w.Bind("flipVoicePage", bridge.Page)

	w.Init(googleVoiceInitScript)
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.BrowserRunning = true
		s.LastError = ""
		s.LastEvent = "browser-starting"
	})
	// Quit has to reach this window. Its message loop below blocks until the
	// window closes, so nothing here would otherwise notice the quit flag: the
	// Google Voice process outlived "Quit FlipAi", kept a browser profile open
	// inside the data folder, and so also blocked the uninstaller from removing
	// it and the installer from replacing FlipAi.exe.
	watchDone := make(chan struct{})
	defer close(watchDone)
	dock := newVoiceDockController(uintptr(w.Window()), visible)
	go func() {
		// Fast enough that dragging the FlipAi window does not leave the panel
		// trailing behind it, cheap enough to run for as long as FlipAi does:
		// each tick is a file read and, when nothing has moved, no window call
		// at all.
		t := time.NewTicker(120 * time.Millisecond)
		defer t.Stop()
		quitCheck := 0
		for {
			select {
			case <-watchDone:
				return
			case <-t.C:
				quitCheck++
				if quitCheck >= 4 {
					quitCheck = 0
					if quitRequested(dataDir) {
						procVoicePostMessage.Call(uintptr(w.Window()), voiceWMClose, 0, 0)
						return
					}
				}
				req := loadVoiceDock(dataDir)
				docked := dock.Apply(req, time.Now(), voiceDockOwner())
				mutateVoiceDockState(dataDir, docked, dock.blocked)
			}
		}
	}()

	w.Navigate(googleVoiceWebURL)
	if visible {
		// The window is created by a process the user did not click on, so it
		// does not come forward by itself.
		procVoiceShowWindow.Call(uintptr(w.Window()), voiceSWRestore)
		bringToFront(uintptr(w.Window()))
		recordVoiceOpen(dataDir, "window opened", nil)
	} else {
		// The binding shows the window while it is being created, so a
		// background start would otherwise flash a Google Voice window on
		// screen at every Windows sign-in. SW_SHOWMINNOACTIVE puts it away
		// without an animation and without taking focus from whatever the
		// user is doing.
		procVoiceShowWindow.Call(uintptr(w.Window()), voiceSWShowMinNoActive)
	}
	w.Run()
	return nil
}

func voiceChromium(w webview2.WebView) *edge.Chromium {
	defer func() { _ = recover() }()
	v := reflect.ValueOf(w)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	browser := v.Elem().FieldByName("browser")
	if !browser.IsValid() || !browser.CanAddr() {
		return nil
	}
	browser = reflect.NewAt(browser.Type(), unsafe.Pointer(browser.UnsafeAddr())).Elem()
	c, _ := browser.Interface().(*edge.Chromium)
	return c
}

func platformTestAgentVoice(cfg VoiceCallConfig, agent string) error {
	return activateAgentVoice(cfg, agent)
}

func voiceAgentConfig(cfg VoiceCallConfig, agent string) VoiceAgentCallConfig {
	if agent == "A" {
		return cfg.Claude
	}
	return cfg.Codex
}

func activateAgentVoice(cfg VoiceCallConfig, agent string) error {
	target := voiceAgentConfig(cfg, agent)
	hwnd, err := ensureAgentAppWindow(target)
	if err != nil {
		return err
	}
	return startAgentVoiceMode(target, hwnd)
}

// ensureAgentAppWindow finds the desktop app's window, launching the app first
// when a launch command is configured and it is not already running.
func ensureAgentAppWindow(target VoiceAgentCallConfig) (uintptr, error) {
	if strings.TrimSpace(target.AppTitle) == "" {
		return 0, errors.New("set the desktop app window title for this agent")
	}
	hwnd := findWindowContaining(target.AppTitle)
	if hwnd == 0 && target.AppCommand != "" {
		if err := startConfiguredVoiceApp(target.AppCommand); err != nil {
			return 0, err
		}
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(300 * time.Millisecond)
			hwnd = findWindowContaining(target.AppTitle)
			if hwnd != 0 {
				break
			}
		}
	}
	if hwnd == 0 {
		return 0, fmt.Errorf("could not find the %s desktop window; open the app or set its launch command", target.AppTitle)
	}
	return hwnd, nil
}

// startAgentVoiceMode switches an already-running desktop app into its voice
// mode, by its accessible Voice control or its configured shortcut.
func startAgentVoiceMode(target VoiceAgentCallConfig, hwnd uintptr) error {
	focused := bringToFront(hwnd)
	if target.VoiceShortcut != "" {
		if focused {
			return sendVoiceShortcut(target.VoiceShortcut)
		}
		// The accessibility tree does not need focus, so it is the safe way to
		// start voice when Windows would not let the app come forward.
		if invokeVoiceButton(hwnd, false) == nil {
			return nil
		}
		return fmt.Errorf("Windows would not bring %s to the front, so FlipAi did not send its Voice shortcut to another app by mistake; leave %s visible or unminimized during calls", target.AppTitle, target.AppTitle)
	}
	if err := invokeVoiceButton(hwnd, false); err != nil {
		return fmt.Errorf("could not start voice automatically; configure the app's Voice shortcut in FlipAi: %w", err)
	}
	return nil
}

func deactivateAgentVoice(cfg VoiceCallConfig, agent string) error {
	target := voiceAgentConfig(cfg, agent)
	hwnd := findWindowContaining(target.AppTitle)
	if hwnd == 0 {
		return nil
	}
	// Ending voice mode is tried through the accessibility tree first: it works
	// without focus, and the Escape fallback would otherwise land in whatever
	// window the user has in front of them.
	if invokeVoiceButton(hwnd, true) == nil {
		return nil
	}
	if !bringToFront(hwnd) {
		return fmt.Errorf("could not bring %s forward to end its voice session", target.AppTitle)
	}
	return sendKeysLiteral("{ESC}")
}

func startConfiguredVoiceApp(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return errors.New("empty app command")
	}
	cmd := exec.Command("cmd.exe", "/d", "/c", "start", "", command)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start voice app: %w", err)
	}
	return nil
}

// browserWindowTitle reports whether a window title looks like a web browser
// showing the app rather than the desktop app itself. A Chrome tab on
// chatgpt.com is titled "ChatGPT - Google Chrome" and matches the same needle
// as the real ChatGPT window; sending a Voice shortcut to the browser would do
// nothing useful, so a genuine app window is always preferred.
func browserWindowTitle(title string) bool {
	title = strings.ToLower(title)
	for _, suffix := range []string{
		" - google chrome", " - chromium", " - microsoft edge", " - brave",
		" — mozilla firefox", " - mozilla firefox", " - opera", " - vivaldi",
	} {
		if strings.HasSuffix(title, suffix) {
			return true
		}
	}
	return false
}

func findWindowContaining(needle string) uintptr {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return 0
	}
	var best, fallback uintptr
	cb := syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		visible, _, _ := procVoiceIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		n, _, _ := procVoiceGetWindowTextLength.Call(hwnd)
		if n == 0 || n > 4096 {
			return 1
		}
		buf := make([]uint16, n+1)
		procVoiceGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
		title := syscall.UTF16ToString(buf)
		lower := strings.ToLower(title)
		if !strings.Contains(lower, needle) {
			return 1
		}
		// FlipAi's own windows can carry the agent's name in status text; they
		// are never the desktop app being driven.
		if strings.Contains(lower, strings.ToLower(googleVoiceWindowTitle)) {
			return 1
		}
		if browserWindowTitle(title) {
			if fallback == 0 {
				fallback = hwnd
			}
			return 1
		}
		best = hwnd
		return 0
	})
	procVoiceEnumWindows.Call(cb, 0)
	if best != 0 {
		return best
	}
	return fallback
}

// bringToFront returns whether the window really ended up in the foreground.
// Windows refuses foreground changes requested by a background process in
// several situations, and it does so silently. That matters here because the
// next step may be a keystroke: sending one to a window that never came forward
// types into whatever the user is actually working in.
func bringToFront(hwnd uintptr) bool {
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		procVoiceShowWindow.Call(hwnd, voiceSWRestore)
		procVoiceSetForegroundWindow.Call(hwnd)
		if fg, _, _ := procVoiceGetForegroundWindow.Call(); fg == hwnd {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func sendVoiceShortcut(raw string) error {
	parts := strings.Split(raw, "+")
	mods := ""
	key := ""
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		switch p {
		case "ctrl", "control":
			mods += "^"
		case "alt":
			mods += "%"
		case "shift":
			mods += "+"
		case "win", "windows", "meta":
			return errors.New("Windows-key shortcuts are not supported; choose Ctrl/Alt/Shift in the app")
		case "":
		default:
			if key != "" {
				return errors.New("voice shortcut must contain one non-modifier key")
			}
			key = p
		}
	}
	if key == "" {
		return errors.New("voice shortcut has no key")
	}
	keyMap := map[string]string{
		"space": " ", "enter": "~", "return": "~", "escape": "{ESC}", "esc": "{ESC}",
		"tab": "{TAB}", "up": "{UP}", "down": "{DOWN}", "left": "{LEFT}", "right": "{RIGHT}",
	}
	if v, ok := keyMap[key]; ok {
		key = v
	} else if regexp.MustCompile(`^f(?:[1-9]|1[0-2])$`).MatchString(key) {
		key = "{" + strings.ToUpper(key) + "}"
	} else if len(key) == 1 && ((key[0] >= 'a' && key[0] <= 'z') || (key[0] >= '0' && key[0] <= '9')) {
		// SendKeys accepts this directly.
	} else {
		return errors.New("unsupported shortcut key; use a letter, number, Space, Enter, Esc, arrows, Tab, or F1-F12")
	}
	return sendKeysLiteral(mods + key)
}

func sendKeysLiteral(keys string) error {
	// keys was produced by the strict parser above (or is the fixed {ESC}
	// fallback), so it contains no user-controlled PowerShell syntax.
	script := `$ws = New-Object -ComObject WScript.Shell; Start-Sleep -Milliseconds 120; $ws.SendKeys('` + strings.ReplaceAll(keys, "'", "''") + `')`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("send voice shortcut: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func invokeVoiceButton(hwnd uintptr, ending bool) error {
	pattern := `(?i)(voice|microphone|mic)`
	if ending {
		pattern = `(?i)(end|stop|leave|close).*(voice|call|conversation)|(voice|call|conversation).*(end|stop|leave|close)`
	}
	// UI Automation uses the app's accessibility tree rather than screen
	// coordinates, so moving or resizing the window does not break the click.
	script := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
$root = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]%d)
if ($null -eq $root) { exit 2 }
$cond = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ControlTypeProperty,[System.Windows.Automation.ControlType]::Button)
$buttons = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants,$cond)
foreach ($b in $buttons) {
  $name = $b.Current.Name
  if ($name -match '%s') {
    try {
      $p = $b.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
      $p.Invoke()
      exit 0
    } catch {}
  }
}
exit 3`, hwnd, pattern)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return errors.New("no accessible Voice control was found")
	}
	return nil
}

// Keep strconv linked on Windows builds where future shortcut expansion uses
// numeric virtual-key names; it is also useful in debugger expressions and
// avoids a separate platform helper for integer formatting.
var _ = strconv.Itoa

// platformWebView2Runtime reports the installed Microsoft Edge WebView2 Runtime
// version, or "" when it is not installed. FlipAi's Google Voice window cannot
// exist without it, and its absence otherwise shows up only as a window that
// never appears, so the desktop UI states it up front.
//
// The runtime registers itself under a fixed product GUID, per-machine or
// per-user. The read is cached because the settings page asks on every poll.
var webView2Probe = newCachedBool(5*time.Minute, func() bool { return webView2Version() != "" })

// platformVoiceStillOpen reports whether the Google Voice window is still up,
// so a quit can wait for it rather than leaving it holding the data folder.
func platformVoiceStillOpen() bool { return googleVoiceHWND() != 0 }

func platformWebView2Runtime() string {
	if !webView2Probe.get() {
		return ""
	}
	return webView2Version()
}

func webView2Version() string {
	const clientKey = `Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	for _, root := range []string{
		`HKLM\SOFTWARE\WOW6432Node\` + clientKey,
		`HKLM\SOFTWARE\` + clientKey,
		`HKCU\SOFTWARE\` + clientKey,
	} {
		cmd := exec.Command("reg.exe", "QUERY", root, "/v", "pv")
		hideWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			// pv    REG_SZ    <version>
			if len(fields) >= 3 && strings.EqualFold(fields[0], "pv") {
				if v := strings.TrimSpace(fields[len(fields)-1]); v != "" && v != "0.0.0.0" {
					return v
				}
			}
		}
	}
	return ""
}
