//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/gorilla/websocket"
)

// Google Voice can place calls inside WebView2, but Microsoft explicitly does
// not implement Web Push in WebView2. Google Voice uses browser push to wake an
// incoming call, so an embedded WebView can look fully signed in, make outbound
// calls, and still never ring. The receiver therefore runs in the installed
// Microsoft Edge browser in app mode. Edge is one of Google's supported Voice
// browsers, supports Web Push, and its window is still docked into FlipAi so it
// looks and behaves like the embedded panel.
//
// A loopback-only DevTools connection is used for three things only: granting
// voice.google.com microphone/notification permissions without an unreachable
// browser prompt, reading the call controls/caller ID, and clicking Answer for
// an authorized caller. It never exposes a port outside 127.0.0.1.

var (
	procEdgeSetWindowText = voiceUser32.NewProc("SetWindowTextW")
	procEdgeIsWindow      = voiceUser32.NewProc("IsWindow")
)

const edgeVoiceControlInterval = 650 * time.Millisecond

func runGoogleVoiceEdgeWindow(dataDir string, initiallyVisible bool) error {
	edgePath, err := findMicrosoftEdge()
	if err != nil {
		return err
	}

	profile := edgeVoiceUserDataDir(dataDir)
	if err := os.MkdirAll(profile, 0700); err != nil {
		return fmt.Errorf("create Google Voice Edge profile: %w", err)
	}

	port, err := freeLoopbackPort()
	if err != nil {
		return fmt.Errorf("reserve Google Voice control port: %w", err)
	}

	args := []string{
		"--user-data-dir=" + profile,
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=" + strconv.Itoa(port),
		"--app=" + googleVoiceWebURL,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-session-crashed-bubble",
		"--disable-popup-blocking",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-features=CalculateNativeWinOcclusion,IntensiveWakeUpThrottling,AudioServiceOutOfProcess",
		"--autoplay-policy=no-user-gesture-required",
	}
	cmd := exec.Command(edgePath, args...)
	// Edge is the actual Google Voice surface. Do not apply FlipAi's
	// HideWindow helper here: on GUI programs it can start the app-mode
	// window hidden before the dock controller ever gets its HWND.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Microsoft Edge for Google Voice: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	browser, browserPID, err := waitEdgeBrowser(port, 30*time.Second)
	if err != nil {
		return fmt.Errorf("Microsoft Edge started but its local Google Voice control channel did not open: %w", err)
	}
	defer browser.close()
	if err := grantEdgeVoicePermissions(browser); err != nil {
		return fmt.Errorf("could not grant Google Voice microphone and notification permission in Microsoft Edge: %w", err)
	}

	// The app-mode window belongs to Edge's browser process. If the runtime
	// reports no browser PID for some reason, the process started above is the
	// best fallback.
	if browserPID == 0 && cmd.Process != nil {
		browserPID = uint32(cmd.Process.Pid)
	}
	hwnd := waitForEdgeWindow(browserPID, 30*time.Second)
	if hwnd == 0 {
		return errors.New("Microsoft Edge opened for Google Voice but FlipAi could not find its app window")
	}
	setGoogleVoiceEdgeWindowTitle(hwnd)

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
	activate := func(cfg VoiceCallConfig, agent string) error {
		return activateAgentVoiceWithRouting(dataDir, cfg, agent)
	}
	bridge := newVoiceBridge(dataDir, mainConfig, activate, deactivateAgentVoice)

	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.BrowserRunning = true
		s.LastError = ""
		s.LastEvent = "browser-starting"
		s.RenderMode = "Microsoft Edge (incoming-call capable)"
	})

	dock := newVoiceDockController(hwnd, initiallyVisible)
	if initiallyVisible {
		procVoiceShowWindow.Call(hwnd, voiceSWRestore)
		bringToFront(hwnd)
		recordVoiceOpen(dataDir, "window opened", nil)
	} else {
		procVoiceShowWindow.Call(hwnd, voiceSWShowMinNoActive)
	}

	controlTicker := time.NewTicker(edgeVoiceControlInterval)
	defer controlTicker.Stop()
	dockTicker := time.NewTicker(120 * time.Millisecond)
	defer dockTicker.Stop()

	var page *edgeCDPClient
	var lastDeviceJSON string
	var ringKey string
	var lastCaller, lastLabel string
	var callUp bool
	var noCallControls int

	for {
		select {
		case <-dockTicker.C:
			if quitRequested(dataDir) {
				procVoicePostMessage.Call(hwnd, voiceWMClose, 0, 0)
			}
			// Edge rewrites its caption after navigation. Keep the stable title
			// that the rest of FlipAi uses to find/close/dock this receiver.
			setGoogleVoiceEdgeWindowTitle(hwnd)
			if edgeWindowGone(hwnd) {
				if page != nil {
					page.close()
				}
				_ = browser.call("Browser.close", map[string]any{}, nil)
				return nil
			}
			req := loadVoiceDock(dataDir)
			docked := dock.Apply(req, time.Now(), voiceDockOwner())
			mutateVoiceDockState(dataDir, docked, dock.blocked)

		case <-controlTicker.C:
			if page == nil {
				target, err := edgeVoiceTarget(port)
				if err != nil {
					continue
				}
				page, err = dialEdgeCDP(target.WebSocketDebuggerURL)
				if err != nil {
					page = nil
					continue
				}
			}

			snap, err := page.voiceSnapshot()
			if err != nil {
				page.close()
				page = nil
				continue
			}
			bridge.Page(snap.Href, snap.SignedIn, strings.Join(snap.Controls, " | "))

			if len(snap.Devices) > 0 {
				b, _ := json.Marshal(snap.Devices)
				deviceJSON := string(b)
				if deviceJSON != lastDeviceJSON {
					lastDeviceJSON = deviceJSON
					bridge.Devices(deviceJSON)
					_ = routeGoogleVoiceEdgeAudio(dataDir, loadVoiceCallConfig(dataDir), windowProcessID(hwnd))
				}
			}

			if snap.Caller != "" {
				lastCaller = snap.Caller
			}
			if snap.Label != "" {
				lastLabel = snap.Label
			}

			if snap.Answer {
				noCallControls = 0
				key := normalizeUSPhone(snap.Caller) + "|" + strings.ToLower(normalizeCallerLabel(snap.Label))
				if key == "|" {
					key = strings.Join(snap.Controls, "|")
				}
				if key != ringKey {
					ringKey = key
					if bridge.Incoming(snap.Caller, snap.Label) {
						if err := routeGoogleVoiceEdgeAudio(dataDir, loadVoiceCallConfig(dataDir), windowProcessID(hwnd)); err != nil {
							mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
								s.LastError = "Google Voice call reached Microsoft Edge, but FlipAi could not route Edge to the virtual audio cables: " + err.Error()
							})
						}
						clicked, err := page.clickAnswer()
						if err == nil && clicked {
							// Start the desktop agent as soon as Edge accepted the Answer
							// click. The call media stream opens immediately after this,
							// so both sides are already routed before they begin talking.
							bridge.Answered(snap.Caller, snap.Label)
							callUp = true
						}
					}
				}
				continue
			}

			if snap.Hangup {
				noCallControls = 0
				ringKey = ""
				if !callUp {
					// The user may answer by hand in the docked Edge panel. Bridge
					// that call too, using the caller captured while it rang.
					bridge.Answered(lastCaller, lastLabel)
					callUp = true
				}
				continue
			}

			if callUp {
				noCallControls++
				if noCallControls >= 3 {
					bridge.Ended()
					callUp = false
					lastCaller, lastLabel = "", ""
					noCallControls = 0
				}
			} else {
				ringKey = ""
				noCallControls = 0
			}
		}
	}
}

func findMicrosoftEdge() (string, error) {
	if p, err := exec.LookPath("msedge.exe"); err == nil && p != "" {
		return p, nil
	}
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Edge", "Application", "msedge.exe"),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("Microsoft Edge is required for incoming Google Voice calls. Edge is one of Google's supported Voice browsers and, unlike WebView2, supports the Web Push notifications that incoming calls use")
}

func edgeVoiceUserDataDir(dataDir string) string {
	root := voiceProfilePath(dataDir)
	// WebView2 stores Chromium profile data under EBWebView. Reusing that
	// subfolder lets the supported Edge receiver inherit the Google session the
	// user already signed into in earlier FlipAi versions instead of forcing a
	// new sign-in after this fix.
	eb := filepath.Join(root, "EBWebView")
	if st, err := os.Stat(eb); err == nil && st.IsDir() {
		return eb
	}
	return root
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

type edgeTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type edgeVersion struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func edgeHTTPJSON(port int, path string, out any) error {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Edge DevTools returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func waitEdgeBrowser(port int, timeout time.Duration) (*edgeCDPClient, uint32, error) {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		var v edgeVersion
		if err := edgeHTTPJSON(port, "/json/version", &v); err == nil && v.WebSocketDebuggerURL != "" {
			c, err := dialEdgeCDP(v.WebSocketDebuggerURL)
			if err == nil {
				pid := edgeBrowserPID(c)
				return c, pid, nil
			}
			last = err
		} else if err != nil {
			last = err
		}
		time.Sleep(150 * time.Millisecond)
	}
	if last == nil {
		last = errors.New("timed out waiting for Edge")
	}
	return nil, 0, last
}

func edgeBrowserPID(c *edgeCDPClient) uint32 {
	var out struct {
		ProcessInfo []struct {
			Type string `json:"type"`
			ID   int    `json:"id"`
		} `json:"processInfo"`
	}
	if c.call("SystemInfo.getProcessInfo", map[string]any{}, &out) != nil {
		return 0
	}
	for _, p := range out.ProcessInfo {
		if p.Type == "browser" && p.ID > 0 {
			return uint32(p.ID)
		}
	}
	return 0
}

func grantEdgeVoicePermissions(c *edgeCDPClient) error {
	// Browser.setPermission takes Web Permissions API descriptor names,
	// not the older DevTools PermissionType enum. In particular the
	// microphone descriptor is "microphone" (not "audioCapture") and
	// speaker selection is "speaker-selection". Push is granted too:
	// that is the browser capability Google Voice uses to wake an
	// incoming call while its app window is in the background.
	permissions := []map[string]any{
		{"name": "notifications"},
		{"name": "push", "userVisibleOnly": true},
		{"name": "microphone"},
		{"name": "speaker-selection"},
	}
	for _, permission := range permissions {
		params := map[string]any{
			"permission": permission,
			"setting":    "granted",
			"origin":     "https://voice.google.com",
		}
		if err := c.call("Browser.setPermission", params, nil); err != nil {
			// Speaker selection is useful for enumerating/routing output
			// devices, but microphone permission also exposes speakers on
			// Edge builds that predate this descriptor.
			if permission["name"] == "speaker-selection" {
				continue
			}
			return err
		}
	}
	return nil
}

func edgeVoiceTarget(port int) (edgeTarget, error) {
	var targets []edgeTarget
	if err := edgeHTTPJSON(port, "/json/list", &targets); err != nil {
		return edgeTarget{}, err
	}
	var fallback edgeTarget
	for _, t := range targets {
		if t.Type != "page" || t.WebSocketDebuggerURL == "" {
			continue
		}
		u := strings.ToLower(t.URL)
		if strings.Contains(u, "voice.google.com") {
			return t, nil
		}
		if strings.Contains(u, "accounts.google.com") {
			fallback = t
		}
	}
	if fallback.WebSocketDebuggerURL != "" {
		return fallback, nil
	}
	return edgeTarget{}, errors.New("Google Voice page is not ready yet")
}

type edgeCDPClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
	next int64
}

type edgeCDPEnvelope struct {
	ID     int64           `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func dialEdgeCDP(raw string) (*edgeCDPClient, error) {
	conn, _, err := websocket.DefaultDialer.Dial(raw, nil)
	if err != nil {
		return nil, err
	}
	return &edgeCDPClient{conn: conn}, nil
}

func (c *edgeCDPClient) close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *edgeCDPClient) call(method string, params any, out any) error {
	if c == nil || c.conn == nil {
		return errors.New("Edge DevTools connection is closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	req := map[string]any{"id": id, "method": method, "params": params}
	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := c.conn.WriteJSON(req); err != nil {
		return err
	}
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}
		var msg edgeCDPEnvelope
		if json.Unmarshal(raw, &msg) != nil || msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("%s (%d)", msg.Error.Message, msg.Error.Code)
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	}
}

type edgeRuntimeEval struct {
	Result struct {
		Value json.RawMessage `json:"value"`
	} `json:"result"`
	ExceptionDetails json.RawMessage `json:"exceptionDetails,omitempty"`
}

func (c *edgeCDPClient) eval(expression string, awaitPromise bool, out any) error {
	var got edgeRuntimeEval
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  awaitPromise,
	}
	if err := c.call("Runtime.evaluate", params, &got); err != nil {
		return err
	}
	if len(got.ExceptionDetails) > 0 && string(got.ExceptionDetails) != "null" {
		return errors.New("Google Voice page script failed")
	}
	if out == nil {
		return nil
	}
	if len(got.Result.Value) == 0 {
		return errors.New("Google Voice page returned no value")
	}
	return json.Unmarshal(got.Result.Value, out)
}

type edgeVoicePageSnapshot struct {
	Href     string             `json:"href"`
	SignedIn bool               `json:"signedIn"`
	Answer   bool               `json:"answer"`
	Hangup   bool               `json:"hangup"`
	Caller   string             `json:"caller"`
	Label    string             `json:"label"`
	Controls []string           `json:"controls"`
	Devices  []VoiceAudioDevice `json:"devices"`
}

func (c *edgeCDPClient) voiceSnapshot() (edgeVoicePageSnapshot, error) {
	var out edgeVoicePageSnapshot
	err := c.eval(edgeVoiceSnapshotJS, true, &out)
	return out, err
}

func (c *edgeCDPClient) clickAnswer() (bool, error) {
	var clicked bool
	err := c.eval(edgeVoiceClickAnswerJS, false, &clicked)
	return clicked, err
}

func waitForEdgeWindow(pid uint32, timeout time.Duration) uintptr {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h := edgeWindowForPID(pid); h != 0 {
			return h
		}
		time.Sleep(150 * time.Millisecond)
	}
	return 0
}

func edgeWindowForPID(pid uint32) uintptr {
	if pid == 0 {
		return 0
	}
	var found uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		visible, _, _ := procVoiceIsWindowVisible.Call(hwnd)
		if visible == 0 || windowProcessID(hwnd) != pid {
			return 1
		}
		n, _, _ := procVoiceGetWindowTextLength.Call(hwnd)
		if n == 0 {
			return 1
		}
		found = hwnd
		return 0
	})
	procVoiceEnumWindows.Call(cb, 0)
	return found
}

func setGoogleVoiceEdgeWindowTitle(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	title, err := syscall.UTF16PtrFromString(googleVoiceWindowTitle)
	if err != nil {
		return
	}
	procEdgeSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(title)))
}

func edgeWindowGone(hwnd uintptr) bool {
	if hwnd == 0 {
		return true
	}
	// A docked/owned Edge window can temporarily be non-visible while
	// FlipAi is minimized or while Windows moves it between dock states.
	// Visibility therefore cannot be used as liveness: doing so shut down
	// the incoming-call receiver exactly when the app was put away. Test
	// the HWND itself instead; IsWindow stays true for minimized/hidden
	// windows and becomes false only after the window is destroyed.
	ok, _, _ := procEdgeIsWindow.Call(hwnd)
	return ok == 0
}

func routeGoogleVoiceEdgeAudio(dataDir string, cfg VoiceCallConfig, pid uint32) error {
	if pid == 0 {
		return errors.New("could not identify the Microsoft Edge Google Voice process")
	}
	plan := currentVoiceCablePlan(dataDir)
	if plan.GoogleVoiceOutput == "" && plan.GoogleVoiceInput == "" {
		return errors.New(plan.Warning)
	}
	if err := setAppDefaultEndpoints(dataDir, pid, plan.GoogleVoiceOutput, plan.GoogleVoiceInput); err != nil {
		return err
	}
	return nil
}

// This script is intentionally much smaller than the old WebView2 injection.
// Edge itself handles the incoming-call transport; FlipAi only reads the visible
// call dialog and the audio devices, which keeps the automation independent of
// private Google APIs.
const edgeVoiceSnapshotJS = `(async () => {
  const docs = () => {
    const out = [document];
    for (let i = 0; i < out.length; i++) {
      let frames = [];
      try { frames = out[i].querySelectorAll('iframe,frame'); } catch (_) {}
      for (const f of frames) {
        try {
          const d = f.contentDocument;
          if (d && !out.includes(d)) out.push(d);
        } catch (_) {}
      }
    }
    return out;
  };
  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const buttonName = (b) => ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || '')).replace(/\s+/g, ' ').trim();
  const buttons = [];
  for (const d of docs()) {
    try {
      for (const b of d.querySelectorAll('button,[role="button"]')) {
        if (visible(b)) buttons.push(b);
      }
    } catch (_) {}
  }
  const ANSWER_RE = /(^|\b)(answer|accept|pick\s*up|take\s+call)(\b|$)/i;
  const DECLINE_RE = /(decline|reject|ignore|dismiss|voicemail|block|spam)/i;
  const HANG_RE = /(hang\s*up|end\s+call|leave\s+call|end\s+the\s+call)/i;
  const answerEl = buttons.find(b => ANSWER_RE.test(buttonName(b)) && !DECLINE_RE.test(buttonName(b))) || null;
  const hangEl = buttons.find(b => HANG_RE.test(buttonName(b))) || null;

  let scopeText = '';
  let node = answerEl || hangEl;
  for (let i = 0; node && i < 8; i++, node = node.parentElement) {
    const t = String(node.innerText || node.textContent || '').replace(/\u00a0/g, ' ').trim();
    if (t.length >= 5 && t.length <= 1800) {
      scopeText = t;
      const lineCount = t.split(/\r?\n/).filter(Boolean).length;
      if (lineCount >= 3) break;
    }
  }

  const normPhone = (v) => {
    const d = String(v || '').replace(/\D/g, '');
    if (d.length === 11 && d[0] === '1') return d.slice(1);
    return d.length === 10 ? d : '';
  };
  const PHONE_RE = /(?:\+?1[\s.\-]?)?(?:\([0-9]{3}\)|[0-9]{3})[\s.\-]?[0-9]{3}[\s.\-]?[0-9]{4}/;
  const phoneMatch = scopeText.match(PHONE_RE);
  const caller = phoneMatch ? normPhone(phoneMatch[0]) : '';
  const UI_LINE = /^(answer|accept|decline|reject|ignore|dismiss|hang\s*up|end\s+call|leave\s+call|incoming\s+call|mute|unmute|keypad|hold|more|options|calling|google\s+voice|block|report\s+spam|send\s+to\s+voicemail|voicemail|mobile|work|home|cell|main|iphone|android|\d{1,2}:\d{2}(:\d{2})?)$/i;
  let label = '';
  for (const raw of scopeText.split(/\r?\n/)) {
    const line = raw.replace(/\s+/g, ' ').trim();
    if (!line || line.length > 120 || UI_LINE.test(line) || PHONE_RE.test(line)) continue;
    if (/^incoming\s+call\s+from\s+/i.test(line)) {
      label = line.replace(/^incoming\s+call\s+from\s+/i, '').trim();
      break;
    }
    if (!label) label = line;
  }

  let devices = [];
  try {
    if (navigator.mediaDevices && navigator.mediaDevices.enumerateDevices) {
      devices = (await navigator.mediaDevices.enumerateDevices()).map(d => ({
        kind: d.kind,
        deviceId: d.deviceId || '',
        label: d.label || ''
      })).filter(d => d.kind === 'audioinput' || d.kind === 'audiooutput');
    }
  } catch (_) {}

  const names = [];
  for (const b of buttons) {
    const n = buttonName(b);
    if (n && n.length <= 80 && !names.includes(n)) names.push(n);
    if (names.length >= 40) break;
  }
  const body = (() => { try { return (document.body && document.body.innerText) || ''; } catch (_) { return ''; } })();
  const signedIn = location.hostname.toLowerCase() === 'voice.google.com' && !/^\s*sign\s+in\s*$/im.test(body.slice(0, 1500));
  return {
    href: location.href,
    signedIn,
    answer: !!answerEl,
    hangup: !!hangEl,
    caller,
    label,
    controls: names,
    devices
  };
})()`

const edgeVoiceClickAnswerJS = `(() => {
  const docs = () => {
    const out = [document];
    for (let i = 0; i < out.length; i++) {
      let frames = [];
      try { frames = out[i].querySelectorAll('iframe,frame'); } catch (_) {}
      for (const f of frames) {
        try {
          const d = f.contentDocument;
          if (d && !out.includes(d)) out.push(d);
        } catch (_) {}
      }
    }
    return out;
  };
  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const name = (b) => ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || '')).replace(/\s+/g, ' ').trim();
  const ANSWER_RE = /(^|\b)(answer|accept|pick\s*up|take\s+call)(\b|$)/i;
  const DECLINE_RE = /(decline|reject|ignore|dismiss|voicemail|block|spam)/i;
  for (const d of docs()) {
    let buttons = [];
    try { buttons = d.querySelectorAll('button,[role="button"]'); } catch (_) {}
    for (const b of buttons) {
      const n = name(b);
      if (visible(b) && ANSWER_RE.test(n) && !DECLINE_RE.test(n)) {
        b.click();
        return true;
      }
    }
  }
  return false;
})()`
