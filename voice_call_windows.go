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
)

const (
	voiceSWMinimize = 6
	voiceSWRestore  = 9
	voiceWMClose    = 0x0010
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
	dataDir, cfgPath, _, _, err := appPaths()
	if err != nil {
		return
	}
	switch mode {
	case "--google-voice":
		_ = ensureDataDir(dataDir)
		visible := len(os.Args) > 2 && os.Args[2] == "--visible"
		_ = runGoogleVoiceProcess(dataDir, visible)
		os.Exit(0)
	case "--host":
		startVoiceControlServer(dataDir, cfgPath)
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

func googleVoiceHWND() uintptr {
	title, _ := syscall.UTF16PtrFromString(googleVoiceWindowTitle)
	h, _, _ := procVoiceFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
	return h
}

func platformOpenGoogleVoice(dataDir string, show bool) error {
	if h := googleVoiceHWND(); h != 0 {
		if show {
			procVoiceShowWindow.Call(h, voiceSWRestore)
			procVoiceSetForegroundWindow.Call(h)
		}
		return nil
	}
	if !voiceInteractiveSession() {
		return errors.New("Google Voice needs a signed-in Windows desktop session; it cannot open at the Windows sign-in screen")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--google-voice"}
	if show {
		args = append(args, "--visible")
	}
	return spawnDetached(exe, args...)
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
	for range ticker.C {
		if quitRequested(dataDir) {
			return
		}
		cfg := loadVoiceCallConfig(dataDir)
		if cfg.Enabled && googleVoiceHWND() == 0 {
			_ = platformOpenGoogleVoice(dataDir, false)
		}
		if !cfg.Enabled {
			if h := googleVoiceHWND(); h != 0 {
				procVoicePostMessage.Call(h, voiceWMClose, 0, 0)
			}
		}
	}
}

func runGoogleVoiceProcess(dataDir string, initiallyVisible bool) error {
	release, owner := acquireGoogleVoiceInstance()
	if !owner {
		if initiallyVisible {
			if h := googleVoiceHWND(); h != 0 {
				procVoiceShowWindow.Call(h, voiceSWRestore)
				procVoiceSetForegroundWindow.Call(h)
			}
		}
		return nil
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
		visible = false
		if !loadVoiceCallConfig(dataDir).Enabled || quitRequested(dataDir) {
			return nil
		}
		// Closing the call window should not silently turn the always-listening
		// feature off. Recreate it minimized while the optional feature remains on.
		time.Sleep(1500 * time.Millisecond)
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
		return err
	}
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
	if w == nil {
		return errors.New("could not create the Google Voice window; Microsoft Edge WebView2 Runtime may be unavailable")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(760, 560, webview2.HintMin)
	chromium := voiceChromium(w)
	if chromium == nil {
		return errors.New("could not reach the Google Voice browser control to grant microphone access")
	}
	// Per-kind permissions do not work with this WebView2 binding: its
	// PermissionRequested handler passes the out-parameter of GetPermissionKind
	// by value instead of by pointer, so every request reads back as kind 0 and
	// falls through to "ask the user". FlipAi keeps the Google Voice window
	// minimized, so that prompt is invisible and unanswerable — Google Voice
	// silently never gets a microphone and the caller hears nothing. Only the
	// global path skips that broken lookup, so the grant is made there and the
	// permissions FlipAi does not want are removed inside the page instead (see
	// googleVoiceInitScript, which deletes camera, geolocation and clipboard
	// access before Google Voice can ask for them).
	chromium.SetGlobalPermission(edge.CoreWebView2PermissionStateAllow)

	bridge := newVoiceBridge(dataDir, activateAgentVoice, deactivateAgentVoice)
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
	w.Navigate(googleVoiceWebURL)
	if !visible {
		procVoiceShowWindow.Call(uintptr(w.Window()), voiceSWMinimize)
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
	if strings.TrimSpace(target.AppTitle) == "" {
		return errors.New("set the desktop app window title for this agent")
	}
	hwnd := findWindowContaining(target.AppTitle)
	if hwnd == 0 && target.AppCommand != "" {
		if err := startConfiguredVoiceApp(target.AppCommand); err != nil {
			return err
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
		return fmt.Errorf("could not find the %s desktop window; open the app or set its launch command", target.AppTitle)
	}
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
