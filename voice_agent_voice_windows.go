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
//  4. start voice mode;
//  5. confirm it started. A call is only reported as connected once this is
//     true.
//
// Tearing down is the same in reverse, and is equally checked: a desktop app
// left in voice mode after the caller hangs up would still be listening to the
// cable when the next call arrives.

const (
	// agentVoiceStartTimeout is how long voice mode is given to come up. Each
	// accessibility read now waits, inside one client, for the Electron app to
	// build its UI Automation tree, so a single read can take several seconds --
	// the overall budget has to hold a few of those plus the app entering voice
	// mode. The caller hears silence while this runs, so it is not open-ended.
	agentVoiceStartTimeout = 45 * time.Second
	// agentVoiceStopTimeout is the same for shutting it down. Nothing is
	// waiting on it, but the next call is.
	agentVoiceStopTimeout = 20 * time.Second
	// agentVoicePoll is the gap between checks of the accessibility tree. Each
	// check is a PowerShell process, so this is deliberately not tight.
	agentVoicePoll = 900 * time.Millisecond
)

var procVoiceShellExecute = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

// startAgentVoiceSession is step 1 to 5 above. It returns only once a voice
// session is really running, or with a reason it is not.
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
	// that is minimized or fully occluded throttles its renderer and may not
	// build the accessibility tree for its web content at all -- so the Voice
	// control FlipAi is about to look for would not exist to be found. The
	// caller is not typing, so taking focus for the app they are calling is
	// exactly right.
	bringToFront(hwnd)

	// The cables first. Voice mode opens its microphone the moment it starts,
	// and Windows hands a process the endpoint that was assigned to it when
	// the stream opened -- not the one assigned a second later.
	routeAgentAppAudio(dataDir, cfg, agent)

	// A conversation left over from a previous call would carry its own
	// history and its own audio stream into this one. Every call gets a fresh
	// session, so anything still running is ended first.
	if state, err := readAgentVoiceState(hwnd); err == nil && state.Active {
		_ = stopAgentVoiceSession(cfg, agent)
		// Ending voice mode tears the old window down and builds a new one on
		// some releases, so the handle is looked up again rather than reused.
		if h, err := ensureAgentAppWindow(agent, target); err == nil {
			hwnd = h
		}
	}

	deadline := time.Now().Add(agentVoiceStartTimeout)
	var last agentVoiceState
	var lastErr error
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		state, err := runAgentVoiceAction(hwnd, "start")
		if err != nil {
			lastErr = err
		} else {
			last = state
			// Write down what the app actually exposed on this attempt. This is
			// the one thing that turns a call stuck on "starting voice" into a
			// diagnosable picture: whether the window was readable, what controls
			// it offered, and whether FlipAi found and pressed the voice one.
			recordAgentVoiceObservation(dataDir, target.AppTitle, state)
			if state.Active || state.Result == "already-active" {
				return nil
			}
			// The app was pressed. Give it a moment and ask again rather than
			// pressing repeatedly: a second press on a toggle turns voice back
			// off, which is exactly how a call ended up with silence.
			if state.Result == "clicked" {
				if confirmed := waitForAgentVoice(hwnd, true, deadline); confirmed {
					return nil
				}
			}
			// No accessible Voice control. The configured keyboard shortcut is
			// the documented way out of that, and it is only ever sent to a
			// window that really came to the front.
			if state.Result == "not-found" && target.VoiceShortcut != "" {
				if err := sendAgentVoiceShortcut(target); err != nil {
					lastErr = err
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

// recordAgentVoiceObservation writes what the accessibility scan of the desktop
// app saw into the runtime state, so the Connections page can show it. Without
// it, a call that could not start voice mode is a dead end with no clue why.
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
		// The app is gone, which is the strongest possible form of "not in
		// voice mode any more".
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

// runVoicePowerShell runs one accessibility script and parses its report. Both
// the desktop app's voice mode and the Google Voice Answer control are driven
// through this, so there is one place where a failure to run PowerShell at all
// is turned into words.
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
	// A desktop app takes a while to draw its first window, and longer on the
	// first run after an update.
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
// configured, the executable where these apps install themselves, and the Start
// Menu shortcut. The shortcut is what covers a Store-installed app, whose
// executable cannot be launched by path at all.
func launchAgentApp(agent string, target VoiceAgentCallConfig) error {
	if cmd := strings.TrimSpace(target.AppCommand); cmd != "" {
		return startConfiguredVoiceApp(cmd)
	}
	for _, p := range agentAppExecutables(agent, os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return shellOpen(p)
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
func shellOpen(path string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, callErr := procVoiceShellExecute.Call(0,
		uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1)
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
