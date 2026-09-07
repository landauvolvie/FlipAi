//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

const (
	googleVoiceSMSWebURL      = "https://voice.google.com/u/0/messages"
	googleVoiceSMSWindowTitle = "FlipAi — Google Voice SMS"
)

// The page establishes and preserves the user's Google session, passively
// exposes the inbox traffic Google Voice is already receiving, and now owns
// outbound delivery too: replies are typed into the real message composer and
// sent through the page's own Send control. The monitor itself never opens or
// selects a conversation; the outbound worker does that only for a queued send.
const googleVoiceSMSPageMonitorJS = `
(() => {
  if (globalThis.__flipAiGoogleVoiceSMSMonitor) return;
  globalThis.__flipAiGoogleVoiceSMSMonitor = true;
  const norm=v=>String(v||'').replace(/\s+/g,' ').trim();
  const loginPage=()=>{
    const h=String(location.hostname||'').toLowerCase();
    if(h==='accounts.google.com'||h.endsWith('.accounts.google.com')) return true;
    const body=norm(document.body?.innerText||'').slice(0,2500);
    return !!document.querySelector('input[type="email"],input[autocomplete="username"],a[href*="accounts.google.com/ServiceLogin"]') || /^sign in\b/i.test(body);
  };
  const onVoice=()=>String(location.hostname||'').toLowerCase()==='voice.google.com';
  const onMessages=()=>onVoice() && /\/messages(?:\/|$)/i.test(String(location.pathname||''));
  const signed=()=>onVoice() && !loginPage() && !!document.body;
  async function tick(){
    const s=signed(), href=String(location.href||'');
    try{ if(typeof globalThis.flipGoogleVoiceSMSStatus==='function') await globalThis.flipGoogleVoiceSMSStatus(s,onMessages(),href); }catch(_){}
    if(s && !onMessages()){
      const m=String(location.pathname||'').match(/^\/u\/(\d+)/i);
      const target=m?('/u/'+m[1]+'/messages'):'/u/0/messages';
      if(location.pathname!==target) location.replace(target);
    }
  }
  setInterval(tick,1000);
  addEventListener('load',tick);
  document.addEventListener('visibilitychange',tick);
  setTimeout(tick,250);
})();`

func googleVoiceSMSHWND() uintptr {
	title, err := syscall.UTF16PtrFromString(googleVoiceSMSWindowTitle)
	if err != nil {
		return 0
	}
	h, _, _ := procVoiceFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
	return h
}

func googleVoiceSMSProcessAlive() bool { return googleVoiceSMSHWND() != 0 }

func platformStartGoogleVoiceSMSLogin(dataDir string) error {
	_ = platformStopGoogleVoiceSMSWorker(dataDir)
	waitForGoogleVoiceSMSStopped(dataDir, 4*time.Second)
	mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Starting = true
		s.Running = false
		s.Visible = true
		s.LoginActive = true
		s.SignedIn = false
		s.ListenerRunning = false
		s.Ready = false
		s.LastEvent = "sign-in-window-starting"
		s.LastError = ""
	})
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--google-voice-sms-login")
	if err := cmd.Start(); err != nil {
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.Starting = false
			s.LoginActive = false
			s.LastError = err.Error()
		})
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformEnsureGoogleVoiceSMSWorker(dataDir string) error {
	s := loadGoogleVoiceSMSRuntime(dataDir)
	if s.Running && googleVoiceSMSProcessAlive() {
		return nil
	}
	if s.Starting && time.Since(s.UpdatedAt) < 15*time.Second {
		return nil
	}
	mutateGoogleVoiceSMSRuntime(dataDir, func(v *GoogleVoiceSMSRuntimeState) {
		v.Starting = true
		v.Running = false
		v.Visible = false
		v.LoginActive = false
		v.SignedIn = false
		v.ListenerRunning = false
		v.Ready = false
		v.LastEvent = "background-starting"
		v.LastError = ""
	})
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--google-voice-sms-worker")
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		mutateGoogleVoiceSMSRuntime(dataDir, func(v *GoogleVoiceSMSRuntimeState) {
			v.Starting = false
			v.LastError = err.Error()
		})
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformStopGoogleVoiceSMSWorker(dataDir string) error {
	if h := googleVoiceSMSHWND(); h != 0 {
		procVoicePostMessage.Call(h, voiceWMClose, 0, 0)
		waitForGoogleVoiceSMSStopped(dataDir, 5*time.Second)
	}
	mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Running = false
		s.Starting = false
		s.Visible = false
		s.LoginActive = false
		s.SignedIn = false
		s.ListenerRunning = false
		s.Ready = false
	})
	return nil
}

func platformDisconnectGoogleVoiceSMS(dataDir string) error {
	_ = platformStopGoogleVoiceSMSWorker(dataDir)
	if err := os.RemoveAll(googleVoiceSMSProfilePath(dataDir)); err != nil {
		return err
	}
	_ = os.Remove(googleVoiceSMSRuntimePath(dataDir))
	_ = os.Remove(googleVoiceSMSAPIStatePath(dataDir))
	return nil
}

func waitForGoogleVoiceSMSStopped(dataDir string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !googleVoiceSMSProcessAlive() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func runGoogleVoiceSMSWebView(dataDir string, visible bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(googleVoiceSMSProfilePath(dataDir), 0700); err != nil {
		return err
	}
	opts := webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: visible,
		DataPath:  googleVoiceSMSProfilePath(dataDir),
		WindowOptions: webview2.WindowOptions{
			Title:  googleVoiceSMSWindowTitle,
			Width:  1120,
			Height: 820,
			Center: visible,
		},
	}
	if !visible {
		opts.WindowOptions.Center = false
		opts.WindowOptions.Position = true
		opts.WindowOptions.X = -30000
		opts.WindowOptions.Y = -30000
		opts.WindowOptions.ExStyle = wsExToolWin | wsExNoActivate
		opts.WindowOptions.NoActivate = true
	}
	w := webview2.NewWithOptions(opts)
	if w == nil {
		return errors.New("Microsoft Edge WebView2 Runtime could not create the Google Voice SMS browser")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(800, 600, webview2.HintMin)

	_ = w.Bind("flipGoogleVoiceSMSStatus", func(signedIn, _ bool, href string) {
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.Running = true
			s.Starting = false
			s.Visible = visible
			s.LoginActive = visible
			s.SignedIn = signedIn
			s.Page = href
			if !signedIn {
				// Connected is the persisted user intent: it means this private
				// browser profile completed sign-in before. A renderer can report
				// signed-out briefly while WebView2 restores that profile after a
				// reboot. Clearing Connected here turns that transient page state
				// into a permanent disconnect and prevents the supervisor from
				// restoring the hidden worker. Only explicit Disconnect may clear
				// the saved connection/profile.
				s.ListenerRunning = false
				s.Ready = false
				if s.Connected && !visible {
					s.LastEvent = "session-restoring"
					s.LastError = "Waiting for the saved Google Voice session to restore"
				} else {
					s.LastEvent = "waiting-for-sign-in"
					s.LastError = "Sign in to Google Voice in the window FlipAi opened"
				}
			} else {
				s.Connected = true
				if !s.Ready && s.LastEvent != "background-api-error" {
					s.LastEvent = "waiting-for-background-api"
					s.LastError = "Waiting for Google Voice background connection"
				}
			}
		})
	})
	// Installed before any page script on every navigation, so the page's own
	// calls to the Google Voice web service are wrapped rather than a copy the
	// app already captured. This is what lets FlipAi read the conversation
	// updates Google Voice is already receiving, and sign its own requests the
	// way Google's client signs them.
	w.Init(googleVoiceSMSNetworkCaptureJS)
	w.Init(googleVoiceSMSPageMonitorJS)

	dev := newWebViewDevTools(w)
	stop := make(chan struct{})
	defer close(stop)
	go runGoogleVoiceSMSAPIInboxLoop(dataDir, dev, stop)
	go runGoogleVoiceSMSOutboundLoop(dataDir, dev, stop)
	go runGoogleVoiceSMSMediaCaptureLoop(dataDir, dev, stop)
	quitStop := watchQuitAndClose(uintptr(w.Window()))
	defer close(quitStop)

	mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Running = true
		s.Starting = false
		s.Visible = visible
		s.LoginActive = visible
		s.ListenerRunning = false
		s.SignedIn = false
		s.Ready = false
		s.LastEvent = "browser-starting"
		s.LastError = "Waiting for Google Voice background connection"
	})
	w.Navigate(googleVoiceSMSWebURL)
	w.Run()
	mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
		s.Running = false
		s.Starting = false
		s.Visible = false
		s.LoginActive = false
		s.SignedIn = false
		s.ListenerRunning = false
		s.Ready = false
		s.LastEvent = "browser-closed"
	})
	return nil
}

// runGoogleVoiceSMSBackgroundSupervisor is what makes one sign-in last. Once
// Connected is recorded it silently restores the hidden SMS browser whenever it
// is missing: after the sign-in window is closed, after FlipAi restarts, and
// after Windows restarts. It never opens a sign-in window on its own.
//
// Running, Starting and LoginActive describe a process, so a state file that
// outlived its process describes one that no longer exists. Believing those
// stale flags is what left a connected install permanently down after a reboot:
// a persisted Starting blocked the restart guard, and a persisted LoginActive
// made the supervisor think a sign-in window was still open. Both are checked
// against the real window before they are trusted.
func runGoogleVoiceSMSBackgroundSupervisor(ctx context.Context, dataDir string) {
	t := time.NewTicker(1500 * time.Millisecond)
	defer t.Stop()
	var lastAttempt time.Time
	for {
		_, cfgPath, _, _, err := appPaths()
		if err == nil {
			if cfg, cfgErr := loadConfig(cfgPath, dataDir); cfgErr == nil {
				s := loadGoogleVoiceSMSRuntime(dataDir)
				alive := googleVoiceSMSProcessAlive()
				// A live window writes to this state every second, so state that
				// has not moved in 20 seconds with no window belongs to a
				// process that is gone. A sign-in window that is merely still
				// opening is therefore never mistaken for a dead one.
				stale := time.Since(s.UpdatedAt) > 20*time.Second
				if !alive && stale && (s.Running || s.Starting || s.Visible || s.LoginActive) {
					mutateGoogleVoiceSMSRuntime(dataDir, func(v *GoogleVoiceSMSRuntimeState) {
						v.Running = false
						v.Starting = false
						v.Visible = false
						v.LoginActive = false
						v.SignedIn = false
						v.ListenerRunning = false
						v.Ready = false
						v.LastEvent = "background-restart-pending"
					})
					s = loadGoogleVoiceSMSRuntime(dataDir)
				}
				want := cfg.Gmail.Method == GmailMethodGoogleVoice && s.Connected && !s.LoginActive
				if want && !alive && !s.Starting && time.Since(lastAttempt) > 5*time.Second {
					lastAttempt = time.Now()
					_ = platformEnsureGoogleVoiceSMSWorker(dataDir)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func createGoogleVoiceSMSObserver(string, func() bool) (webview2.WebView, voiceDevTools, error) {
	return nil, nil, errors.New("direct Google Voice SMS runs in its independent background browser process")
}

func googleVoiceSMSWorkerMode() bool {
	return len(os.Args) > 1 && (os.Args[1] == "--google-voice-sms-worker" || os.Args[1] == "--google-voice-sms-login")
}

func googleVoiceSMSCallProcess() bool {
	return len(os.Args) > 1 && strings.EqualFold(os.Args[1], "--google-voice")
}
