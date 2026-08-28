//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// This is the whole of "put the caller through to the desktop app", and the
// whole of taking it back down again. It replaces three overlapping guards that
// each pressed the app's Voice control on their own schedule.
//
// The sequence is fixed, and every step is checked:
//
//  1. find the desktop app's window, launching the app if it is not running;
//  2. point that app's microphone and speaker at the virtual cables, before
//     anything opens an audio stream;
//  3. make sure no voice session is already running -- a call must start a new
//     conversation, not join whatever was left over;
//  4. make Chromium/Electron expose its renderer controls when an already-open
//     app is exposing only its native window frame;
//  5. start voice mode;
//  6. confirm it started. A call is only reported as connected once this is
//     true.
//
// Tearing down is the same in reverse, and is equally checked: a desktop app
// left in voice mode after the caller hangs up would still be listening to the
// cable when the next call arrives.

const (
	// agentVoiceStartTimeout is how long voice mode is given to come up. Each
	// accessibility read waits, inside one client, for the Electron app to build
	// its UI Automation tree, so a single read can take several seconds.
	agentVoiceStartTimeout = 45 * time.Second
	// agentVoiceStopTimeout is the same for shutting it down. Nothing is waiting
	// on it, but the next call is.
	agentVoiceStopTimeout = 20 * time.Second
	// agentVoicePoll is the gap between checks of the accessibility tree. Each
	// check is a PowerShell process, so this is deliberately not tight.
	agentVoicePoll = 900 * time.Millisecond
)

var (
	procVoiceShellExecute                  = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	agentVoiceUser32                       = syscall.NewLazyDLL("user32.dll")
	procAgentVoiceGetWindowThreadProcessID = agentVoiceUser32.NewProc("GetWindowThreadProcessId")
	procAgentVoiceIsWindow                 = agentVoiceUser32.NewProc("IsWindow")
)

// startAgentVoiceSession is the checked path from an answered Google Voice call
// into the desktop app's actual voice conversation.
func startAgentVoiceSession(dataDir string, cfg VoiceCallConfig, agent string) error {
	target := voiceAgentConfig(cfg, agent)
	hwnd, err := ensureAgentAppWindow(agent, target)
	if err != nil {
		// Record why nothing was wired, so the status page can say the app is
		// missing rather than blaming the audio path.
		routeAgentAppAudio(dataDir, cfg, agent)
		return err
	}

	// Bring the app to the front before anything else. A Chromium/Electron app
	// that is minimized or fully occluded can throttle its renderer and delay its
	// accessibility tree.
	bringToFront(hwnd)

	// The cables first. Voice mode opens its microphone the moment it starts,
	// and Windows hands a process the endpoint that was assigned when that stream
	// opened -- not one assigned a second later.
	routeAgentAppAudio(dataDir, cfg, agent)

	accessibilityRestarted := false

	// A conversation left over from a previous call would carry its own history
	// and audio stream into this one. Every call gets a fresh session. This first
	// read also gives us the earliest possible chance to repair the exact field
	// failure where an already-running Electron app exposes only its title bar.
	if state, stateErr := readAgentVoiceState(hwnd); stateErr == nil {
		recordAgentVoiceObservation(dataDir, target.AppTitle, state)
		if state.Active {
			_ = stopAgentVoiceSession(cfg, agent)
			// Ending voice mode tears the old window down and builds a new one on
			// some releases, so the handle is looked up again rather than reused.
			if h, ensureErr := ensureAgentAppWindow(agent, target); ensureErr == nil {
				hwnd = h
			}
		} else if shouldRestartAgentForAccessibility(target.AppTitle, state) {
			// This is not an ordinary "voice button renamed" failure. Seeing only
			// [App, Minimize, Maximize, Close] means the Electron renderer is not
			// exposed to Windows at all. The force-renderer-accessibility switch
			// only takes effect at process start, so recover automatically instead
			// of asking the caller/user to quit the app by hand.
			state.Result = "restarting-for-accessibility"
			recordAgentVoiceObservation(dataDir, target.AppTitle, state)
			h, restartErr := restartAgentAppForAccessibility(agent, target, hwnd)
			if restartErr != nil {
				return fmt.Errorf("%s was running with renderer accessibility off and FlipAi could not restart it for voice: %w", target.AppTitle, restartErr)
			}
			hwnd = h
			accessibilityRestarted = true
			bringToFront(hwnd)
			// The restart creates a new process id, so the per-app audio route has
			// to be written again before voice opens its stream.
			routeAgentAppAudio(dataDir, cfg, agent)
		}
	}

	deadline := time.Now().Add(agentVoiceStartTimeout)
	var last agentVoiceState
	var lastErr error
	for time.Now().Before(deadline) {
		state, actionErr := runAgentVoiceAction(hwnd, "start")
		if actionErr != nil {
			lastErr = actionErr
		} else {
			last = state
			recordAgentVoiceObservation(dataDir, target.AppTitle, state)
			if state.Active || state.Result == "already-active" {
				return nil
			}

			// The preflight normally catches this, but Electron can expose only its
			// frame after a navigation/relaunch as well. Recover once per call, then
			// continue using the newly-created HWND. This is the missing piece in
			// the previous implementation: it diagnosed accessibility-off but kept
			// driving the same inaccessible process forever.
			if !accessibilityRestarted && shouldRestartAgentForAccessibility(target.AppTitle, state) {
				state.Result = "restarting-for-accessibility"
				recordAgentVoiceObservation(dataDir, target.AppTitle, state)
				h, restartErr := restartAgentAppForAccessibility(agent, target, hwnd)
				if restartErr != nil {
					lastErr = fmt.Errorf("could not restart %s with renderer accessibility enabled: %w", target.AppTitle, restartErr)
				} else {
					hwnd = h
					accessibilityRestarted = true
					bringToFront(hwnd)
					routeAgentAppAudio(dataDir, cfg, agent)
					// Give the fresh renderer a full voice-start budget. The caller is
					// already connected; failing because the old process consumed the
					// timer would turn a successful recovery into silence.
					deadline = time.Now().Add(agentVoiceStartTimeout)
					continue
				}
			}

			// The app was pressed. Give it a moment and ask again rather than
			// pressing repeatedly: a second press on a toggle can turn voice off.
			if state.Result == "clicked" {
				if confirmed := waitForAgentVoice(hwnd, true, deadline); confirmed {
					return nil
				}
			}

			// No accessible Voice control. A configured keyboard shortcut remains
			// the fallback for apps/releases that genuinely provide one.
			if state.Result == "not-found" && target.VoiceShortcut != "" {
				if shortcutErr := sendAgentVoiceShortcut(target); shortcutErr != nil {
					lastErr = shortcutErr
				} else if waitForAgentVoice(hwnd, true, deadline) {
					return nil
				}
			}
		}
		time.Sleep(agentVoicePoll)
	}
	if lastErr != nil && !last.Found {
		return fmt.Errorf("could not drive %s: %w", target.AppTitle, lastErr)
	}
	return agentVoiceStartFailure(target.AppTitle, last)
}

// restartAgentAppForAccessibility replaces the exact Electron process whose
// renderer is inaccessible, then starts that same executable with Chromium's
// accessibility switch. It is called only for the title-bar-only signature and
// at most once per call, so an ordinary unknown/renamed Voice control never
// causes the user's app to be restarted.
func restartAgentAppForAccessibility(agent string, target VoiceAgentCallConfig, hwnd uintptr) (uintptr, error) {
	pid, exePath := agentVoiceWindowProcess(hwnd)
	if pid == 0 {
		return 0, errors.New("could not identify the desktop app process")
	}

	// Electron keeps renderer/helper processes beside the browser process. Kill
	// the process tree so a surviving helper cannot keep the old accessibility
	// state alive. This recovery is used only after Windows proved the renderer
	// is inaccessible and a live phone call is waiting for voice.
	kill := exec.Command("taskkill.exe", "/PID", fmt.Sprintf("%d", pid), "/T", "/F")
	hideWindow(kill)
	out, killErr := kill.CombinedOutput()
	if killErr != nil && agentVoiceWindowExists(hwnd) {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = killErr.Error()
		}
		return 0, fmt.Errorf("Windows would not close the inaccessible desktop app: %s", truncate(text, 240))
	}

	closeDeadline := time.Now().Add(8 * time.Second)
	for agentVoiceWindowExists(hwnd) && time.Now().Before(closeDeadline) {
		time.Sleep(150 * time.Millisecond)
	}
	if agentVoiceWindowExists(hwnd) {
		return 0, errors.New("the old desktop app window did not close")
	}

	// Prefer the executable that owned the actual window. This covers custom
	// install locations and configured commands without guessing. If Windows
	// does not expose that path (some packaged apps do not), fall back to the
	// normal discovery path.
	var launchErr error
	if exePath != "" {
		launchErr = shellOpenArgs(exePath, "--force-renderer-accessibility")
	}
	if exePath == "" || launchErr != nil {
		launchErr = launchAgentApp(agent, target)
	}
	if launchErr != nil {
		return 0, launchErr
	}

	openDeadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(openDeadline) {
		time.Sleep(350 * time.Millisecond)
		if h := findAgentAppWindow(agent, target); h != 0 {
			return h, nil
		}
	}
	return 0, fmt.Errorf("FlipAi restarted %s with renderer accessibility enabled, but its window did not reappear", agentAppDescription(agent, target))
}

// agentVoiceWindowProcess returns the process that owns a desktop app window and
// its executable path when Windows allows that path to be read. Keeping the path
// before terminating the process lets FlipAi restart the exact installed app,
// even when it lives somewhere outside the usual candidate folders.
func agentVoiceWindowProcess(hwnd uintptr) (uint32, string) {
	if hwnd == 0 {
		return 0, ""
	}
	var pid uint32
	r, _, _ := procAgentVoiceGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if r == 0 || pid == 0 {
		return 0, ""
	}

	script := fmt.Sprintf("$p=Get-Process -Id %d -ErrorAction SilentlyContinue; if($null -ne $p){$p.Path}", pid)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return pid, ""
	}
	path := strings.TrimSpace(string(out))
	if i := strings.IndexAny(path, "\r\n"); i >= 0 {
		path = strings.TrimSpace(path[:i])
	}
	if path == "" {
		return pid, ""
	}
	if st, statErr := os.Stat(path); statErr != nil || st.IsDir() {
		return pid, ""
	}
	return pid, path
}

func agentVoiceWindowExists(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procAgentVoiceIsWindow.Call(hwnd)
	return r != 0
}

// recordAgentVoiceObservation writes what the accessibility scan of the desktop
// app saw into runtime state so the Connections page can explain every attempt.
func recordAgentVoiceObservation(dataDir, appTitle string, state agentVoiceState) {
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.AgentVoiceReadable = state.Found
		s.AgentVoiceControls = append([]string(nil), state.Controls...)
		s.AgentVoiceStart = state.StartControl
		s.AgentVoiceResult = state.Result
		s.AgentVoiceApp = appTitle
		s.AgentVoiceAt = time.Now()
	})
}

// stopAgentVoiceSession ends the voice session started for a call.
func stopAgentVoiceSession(cfg VoiceCallConfig, agent string) error {
	target := voiceAgentConfig(cfg, agent)
	hwnd := findAgentAppWindow(agent, target)
	if hwnd == 0 {
		return nil
	}
	deadline := time.Now().Add(agentVoiceStopTimeout)
	var last agentVoiceState
	for time.Now().Before(deadline) {
		state, err := runAgentVoiceAction(hwnd, "stop")
		if err == nil {
			last = state
			if !state.Active || state.Result == "already-stopped" {
				return nil
			}
			if state.Result == "clicked" && waitForAgentVoice(hwnd, false, deadline) {
				return nil
			}
		}
		time.Sleep(agentVoicePoll)
	}
	// Escape is the last resort and is deliberately last: it goes to whatever
	// window has focus, so it is only sent to an app that really came forward.
	if bringToFront(hwnd) {
		if err := sendKeysLiteral("{ESC}"); err == nil && waitForAgentVoice(hwnd, false, time.Now().Add(3*time.Second)) {
			return nil
		}
	}
	if last.Active {
		return fmt.Errorf("%s is still in voice mode after the call ended", target.AppTitle)
	}
	return nil
}

// waitForAgentVoice waits for voice mode to actually reach the wanted state.
func waitForAgentVoice(hwnd uintptr, wantActive bool, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		time.Sleep(agentVoicePoll)
		state, err := readAgentVoiceState(hwnd)
		if err == nil && state.Found && state.Active == wantActive {
			return true
		}
	}
	return false
}

func readAgentVoiceState(hwnd uintptr) (agentVoiceState, error) {
	return runAgentVoiceAction(hwnd, "state")
}

func runAgentVoiceAction(hwnd uintptr, action string) (agentVoiceState, error) {
	script, err := voiceAgentUIAScript(hwnd, action)
	if err != nil {
		return agentVoiceState{}, err
	}
	return runVoicePowerShell(script)
}

// runVoicePowerShell runs one accessibility script and parses its report.
func runVoicePowerShell(script string) (agentVoiceState, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	hideWindow(cmd)
	out, combineErr := cmd.CombinedOutput()
	state := parseAgentVoiceReport(string(out))
	if !state.Found && combineErr != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = combineErr.Error()
		}
		return state, fmt.Errorf("Windows accessibility could not read the desktop app: %s", truncate(text, 300))
	}
	return state, nil
}

// ensureAgentAppWindow finds the desktop app's window, starting the app when it
// is not running.
func ensureAgentAppWindow(agent string, target VoiceAgentCallConfig) (uintptr, error) {
	if hwnd := findAgentAppWindow(agent, target); hwnd != 0 {
		return hwnd, nil
	}
	if err := launchAgentApp(agent, target); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		if hwnd := findAgentAppWindow(agent, target); hwnd != 0 {
			return hwnd, nil
		}
	}
	return 0, fmt.Errorf("FlipAi started %s but its window did not appear", agentAppDescription(agent, target))
}

// findAgentAppWindow looks for the desktop app's own window, never a browser
// tab showing the same product and never one of FlipAi's own windows.
func findAgentAppWindow(agent string, target VoiceAgentCallConfig) uintptr {
	for _, needle := range agentWindowNeedles(agent, target) {
		if hwnd := findWindowContaining(needle); hwnd != 0 {
			return hwnd
		}
	}
	return 0
}

// agentWindowNeedles is the configured window title first, then the titles
// FlipAi knows. A user who has told FlipAi which window to drive is never
// second-guessed.
func agentWindowNeedles(agent string, target VoiceAgentCallConfig) []string {
	var out []string
	if t := strings.TrimSpace(target.AppTitle); t != "" {
		out = append(out, t)
	}
	for _, t := range agentAppTitles(agent) {
		if !containsFold(out, t) {
			out = append(out, t)
		}
	}
	return out
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func agentAppDescription(agent string, target VoiceAgentCallConfig) string {
	if t := strings.TrimSpace(target.AppTitle); t != "" {
		return "the " + t + " desktop app"
	}
	if agent == "A" {
		return "the Claude desktop app"
	}
	return "the Codex desktop app"
}

// launchAgentApp starts the desktop app, trying, in order: the command the user
// configured, known executable locations, and the Start Menu shortcut.
func launchAgentApp(agent string, target VoiceAgentCallConfig) error {
	if cmd := strings.TrimSpace(target.AppCommand); cmd != "" {
		return startConfiguredVoiceApp(cmd)
	}
	for _, p := range agentAppExecutables(agent, os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return shellOpenArgs(p, "--force-renderer-accessibility")
		}
	}
	if link := findAgentStartMenuShortcut(agent); link != "" {
		return shellOpen(link)
	}
	return fmt.Errorf("FlipAi could not find %s on this PC. Install it, or set its launch command in FlipAi's Google Voice settings", agentAppDescription(agent, target))
}

// findAgentStartMenuShortcut looks for the app's Start Menu entry, in the
// user's own Start Menu first and then the machine-wide one.
func findAgentStartMenuShortcut(agent string) string {
	roots := []string{
		filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs"),
		filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs"),
	}
	wanted := agentAppShortcutNames(agent)
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		for _, name := range wanted {
			var found string
			_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil || found != "" {
					return nil
				}
				if d.IsDir() {
					return nil
				}
				base := strings.ToLower(d.Name())
				if base == strings.ToLower(name)+".lnk" {
					found = path
					return filepath.SkipAll
				}
				return nil
			})
			if found != "" {
				return found
			}
		}
	}
	return ""
}

// shellOpen starts something the way double-clicking it would, which is the
// only way to start a Store-packaged app.
func shellOpen(path string) error { return shellOpenArgs(path, "") }

// shellOpenArgs starts something the way double-clicking it would, optionally
// with command-line arguments. Arguments are honoured for a real executable;
// packaged shortcuts can ignore them, which is why recovery first records the
// exact process executable before closing the inaccessible app.
func shellOpenArgs(path, args string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var argsPtr *uint16
	if strings.TrimSpace(args) != "" {
		if argsPtr, err = syscall.UTF16PtrFromString(args); err != nil {
			return err
		}
	}
	r, _, callErr := procVoiceShellExecute.Call(0,
		uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(argsPtr)), 0, 1)
	if r <= 32 {
		return fmt.Errorf("Windows would not start %s (ShellExecuteW=%d): %v", filepath.Base(path), r, callErr)
	}
	return nil
}

// sendAgentVoiceShortcut is the configured keyboard fallback. It refuses to
// type into a window that did not come forward: the keystroke would otherwise
// land in whatever the user is actually working in.
func sendAgentVoiceShortcut(target VoiceAgentCallConfig) error {
	hwnd := findWindowContaining(target.AppTitle)
	if hwnd == 0 {
		return errors.New("the desktop app window went away before its Voice shortcut could be sent")
	}
	if !bringToFront(hwnd) {
		return fmt.Errorf("Windows would not bring %s to the front, so FlipAi did not send its Voice shortcut to another app by mistake", target.AppTitle)
	}
	return sendVoiceShortcut(target.VoiceShortcut)
}
