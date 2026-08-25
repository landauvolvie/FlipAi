//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
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
	if chromium != nil {
		chromium.SetPermission(edge.CoreWebView2PermissionKindMicrophone, edge.CoreWebView2PermissionStateAllow)
		chromium.SetPermission(edge.CoreWebView2PermissionKindNotifications, edge.CoreWebView2PermissionStateAllow)
		chromium.SetPermission(edge.CoreWebView2PermissionKindCamera, edge.CoreWebView2PermissionStateDeny)
		chromium.SetPermission(edge.CoreWebView2PermissionKindGeolocation, edge.CoreWebView2PermissionStateDeny)
		chromium.SetPermission(edge.CoreWebView2PermissionKindOtherSensors, edge.CoreWebView2PermissionStateDeny)
		chromium.SetPermission(edge.CoreWebView2PermissionKindClipboardRead, edge.CoreWebView2PermissionStateDeny)
	}

	_ = w.Bind("flipVoiceAudioSettings", func() map[string]string {
		cfg := loadVoiceCallConfig(dataDir)
		return map[string]string{"input": cfg.GoogleVoiceInput, "output": cfg.GoogleVoiceOutput, "ring": cfg.RingOutput}
	})
	_ = w.Bind("flipVoiceIncoming", func(caller string) bool {
		cfg := loadVoiceCallConfig(dataDir)
		agent, allowed := voiceAgentForCaller(cfg, caller)
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.Caller = normalizeUSPhone(caller)
			s.Agent = agent
			if allowed {
				s.LastEvent = "authorized-call-ringing"
			} else {
				s.LastEvent = "blocked-call-ringing"
			}
		})
		return allowed && cfg.AutoAnswer
	})
	_ = w.Bind("flipVoiceAnswered", func(caller string) bool {
		cfg := loadVoiceCallConfig(dataDir)
		agent, allowed := voiceAgentForCaller(cfg, caller)
		if !allowed {
			mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
				s.InCall = true
				s.Caller = normalizeUSPhone(caller)
				s.Agent = ""
				s.LastEvent = "unbridged-call"
			})
			return false
		}
		if err := activateAgentVoice(cfg, agent); err != nil {
			mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
				s.InCall = true
				s.Caller = normalizeUSPhone(caller)
				s.Agent = agent
				s.LastError = err.Error()
				s.LastEvent = "agent-voice-error"
			})
			return false
		}
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.InCall = true
			s.Caller = normalizeUSPhone(caller)
			s.Agent = agent
			s.LastError = ""
			s.LastEvent = "call-bridged"
		})
		return true
	})
	_ = w.Bind("flipVoiceEnded", func() {
		s := loadVoiceRuntime(dataDir)
		cfg := loadVoiceCallConfig(dataDir)
		if s.Agent == "A" || s.Agent == "C" {
			_ = deactivateAgentVoice(cfg, s.Agent)
		}
		mutateVoiceRuntime(dataDir, func(st *VoiceRuntimeState) {
			st.InCall = false
			st.Caller = ""
			st.Agent = ""
			st.LastEvent = "call-ended"
		})
	})
	_ = w.Bind("flipVoiceDevices", func(raw string) {
		var devices []VoiceAudioDevice
		if json.Unmarshal([]byte(raw), &devices) != nil {
			return
		}
		if len(devices) > 80 {
			devices = devices[:80]
		}
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.Devices = devices
			s.LastEvent = "audio-devices"
		})
	})
	_ = w.Bind("flipVoicePage", func(href string, signedIn bool) {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.BrowserRunning = true
			s.Page = href
			s.SignedIn = signedIn
			if s.LastEvent == "" || s.LastEvent == "browser-starting" {
				s.LastEvent = "browser-ready"
			}
		})
	})

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
	procVoiceShowWindow.Call(hwnd, voiceSWRestore)
	procVoiceSetForegroundWindow.Call(hwnd)
	time.Sleep(180 * time.Millisecond)
	if target.VoiceShortcut != "" {
		return sendVoiceShortcut(target.VoiceShortcut)
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
	procVoiceSetForegroundWindow.Call(hwnd)
	if invokeVoiceButton(hwnd, true) == nil {
		return nil
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

func findWindowContaining(needle string) uintptr {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return 0
	}
	var found uintptr
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
		title := strings.ToLower(syscall.UTF16ToString(buf))
		if strings.Contains(title, needle) {
			found = hwnd
			return 0
		}
		return 1
	})
	procVoiceEnumWindows.Call(cb, 0)
	return found
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

const googleVoiceInitScript = `
(() => {
  if (window.__flipVoiceInstalled) return;
  window.__flipVoiceInstalled = true;

  const allowedTopLevel = (href) => {
    try {
      const h = new URL(href, location.href).hostname.toLowerCase();
      return h === 'voice.google.com' || h === 'accounts.google.com';
    } catch (_) { return false; }
  };
  document.addEventListener('click', (e) => {
    const a = e.target && e.target.closest ? e.target.closest('a[href]') : null;
    if (a && !allowedTopLevel(a.href)) e.preventDefault();
  }, true);

  const normPhone = (v) => {
    const d = String(v || '').replace(/\D/g, '');
    if (d.length === 11 && d[0] === '1') return d.slice(1);
    return d.length === 10 ? d : '';
  };
  const phoneFrom = (text) => {
    const m = String(text || '').match(/(?:\+?1[\s.\-]?)?(?:\([0-9]{3}\)|[0-9]{3})[\s.\-]?[0-9]{3}[\s.\-]?[0-9]{4}/);
    return m ? normPhone(m[0]) : '';
  };
  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const buttonName = (b) => ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || '')).trim();
  const buttons = () => Array.from(document.querySelectorAll('button,[role="button"]')).filter(visible);
  const findAnswer = () => buttons().find(b => /^(answer|accept)(\s+call)?$/i.test(buttonName(b)) || /^answer\b/i.test(buttonName(b)));
  const findHangup = () => buttons().find(b => /(hang\s*up|end\s+call|leave\s+call)/i.test(buttonName(b)));

  let rememberedCaller = '';
  let inCall = false;
  let answerBusy = false;
  let lastDeviceJSON = '';

  async function audioSettings() {
    try { return await window.flipVoiceAudioSettings(); } catch (_) { return {input:'',output:'',ring:''}; }
  }
  async function currentDevices() {
    try { return await navigator.mediaDevices.enumerateDevices(); } catch (_) { return []; }
  }
  async function reportDevices() {
    const ds = await currentDevices();
    const out = ds.filter(d => d.kind === 'audioinput' || d.kind === 'audiooutput').map((d, i) => ({
      kind: d.kind,
      deviceId: d.deviceId || '',
      label: d.label || (d.kind === 'audioinput' ? 'Microphone ' : 'Speaker ') + (i + 1)
    }));
    const raw = JSON.stringify(out);
    if (raw !== lastDeviceJSON) {
      lastDeviceJSON = raw;
      try { await window.flipVoiceDevices(raw); } catch (_) {}
    }
    return ds;
  }
  async function deviceIdFor(kind, wanted) {
    if (!wanted) return '';
    const ds = await currentDevices();
    const exact = ds.find(d => d.kind === kind && d.label === wanted);
    if (exact) return exact.deviceId;
    const loose = ds.find(d => d.kind === kind && d.label && d.label.toLowerCase().includes(String(wanted).toLowerCase()));
    return loose ? loose.deviceId : '';
  }
  async function routeElement(el) {
    if (!el || typeof el.setSinkId !== 'function') return;
    const s = await audioSettings();
    const id = await deviceIdFor('audiooutput', s.output);
    if (id) { try { await el.setSinkId(id); } catch (_) {} }
  }

  // Force Google Voice's requested microphone onto the virtual endpoint selected
  // in FlipAi. The original constraints are preserved except for deviceId.
  if (navigator.mediaDevices && navigator.mediaDevices.getUserMedia) {
    const gum = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    navigator.mediaDevices.getUserMedia = async function(constraints) {
      let next = constraints;
      try {
        const s = await audioSettings();
        if (constraints && constraints.audio && s.input) {
          const id = await deviceIdFor('audioinput', s.input);
          if (id) {
            const a = constraints.audio === true ? {} : Object.assign({}, constraints.audio);
            a.deviceId = {exact:id};
            next = Object.assign({}, constraints, {audio:a});
          }
        }
      } catch (_) {}
      const stream = await gum(next);
      reportDevices();
      return stream;
    };
    navigator.mediaDevices.addEventListener('devicechange', reportDevices);
  }

  // Google Voice normally renders the remote party through HTML media elements.
  // Apply the selected virtual speaker before playback and whenever new media is
  // inserted. This leaves system-wide Windows audio defaults untouched.
  const nativePlay = HTMLMediaElement.prototype.play;
  HTMLMediaElement.prototype.play = function(...args) {
    routeElement(this);
    return nativePlay.apply(this, args);
  };
  new MutationObserver(() => {
    document.querySelectorAll('audio,video').forEach(routeElement);
  }).observe(document.documentElement, {childList:true, subtree:true});

  async function tick() {
    try {
      const href = location.href;
      const signedIn = location.hostname === 'voice.google.com' && !/sign\s*in/i.test((document.body && document.body.innerText || '').slice(0, 2500));
      await window.flipVoicePage(href, signedIn);

      // If script-driven navigation escapes Voice or Google's sign-in surface,
      // return to Voice. Normal page resources are not affected by this check.
      if (!allowedTopLevel(href)) {
        location.replace('https://voice.google.com/');
        return;
      }

      const answer = findAnswer();
      if (answer && !answerBusy && !inCall) {
        const scope = answer.closest('[role="dialog"]') || answer.parentElement || document.body;
        rememberedCaller = phoneFrom((scope && scope.innerText) || buttonName(answer)) || rememberedCaller;
        answerBusy = true;
        let auto = false;
        try { auto = !!(await window.flipVoiceIncoming(rememberedCaller)); } catch (_) {}
        if (auto && answer.isConnected) answer.click();
        setTimeout(() => { answerBusy = false; }, 1200);
      }

      const hang = findHangup();
      if (hang && !inCall) {
        inCall = true;
        try { await window.flipVoiceAnswered(rememberedCaller); } catch (_) {}
      } else if (!hang && inCall) {
        inCall = false;
        rememberedCaller = '';
        try { await window.flipVoiceEnded(); } catch (_) {}
      }
      document.querySelectorAll('audio,video').forEach(routeElement);
      reportDevices();
    } catch (_) {}
  }
  setInterval(tick, 900);
  setTimeout(tick, 250);
})();
`

// Keep strconv linked on Windows builds where future shortcut expansion uses
// numeric virtual-key names; it is also useful in debugger expressions and
// avoids a separate platform helper for integer formatting.
var _ = strconv.Itoa
