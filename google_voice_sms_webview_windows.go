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

// This monitor deliberately reports state from the page to Go instead of using
// Runtime.evaluate as a heartbeat. v0.46.34 proved why: a WebView can be alive
// while its DevTools evaluation channel is wedged. A provider connection is
// only called ready when the actual page says it is signed in and on Messages.
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
  const ready=()=>signed() && onMessages() && (document.readyState==='interactive'||document.readyState==='complete');
  async function tick(){
    const s=signed(), r=ready(), href=String(location.href||'');
    try{ if(typeof globalThis.flipGoogleVoiceSMSStatus==='function') await globalThis.flipGoogleVoiceSMSStatus(s,r,href); }catch(_){}
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

func googleVoiceSMSProcessAlive() bool {
	return googleVoiceSMSHWND() != 0
}

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

	wasSignedIn := false
	hadConnected := loadGoogleVoiceSMSRuntime(dataDir).Connected
	_ = w.Bind("flipGoogleVoiceSMSStatus", func(signedIn, ready bool, href string) {
		changed := signedIn != wasSignedIn
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.Running = true
			s.Starting = false
			s.Visible = visible
			s.LoginActive = visible
			s.SignedIn = signedIn
			s.ListenerRunning = signedIn
			s.Ready = ready
			s.Page = href
			s.LastProbeAt = time.Now()
			if ready {
				s.Connected = true
				s.LastError = ""
				s.LastEvent = "messages-ready"
			} else if signedIn {
				s.LastEvent = "opening-messages"
				s.LastError = "Opening the Google Voice Messages page"
			} else {
				s.LastEvent = "waiting-for-sign-in"
				s.LastError = "Sign in to Google Voice in the window FlipAi opened"
			}
		})
		if ready && (!hadConnected || changed) {
			hadConnected = true
		}
		wasSignedIn = signedIn
	})
	_ = w.Bind("flipVoiceSMS", func(payload string) {
		if err := appendDirectGoogleVoiceSMS(dataDir, payload); err == nil {
			mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
				s.LastInboundAt = time.Now()
			})
		}
	})
	w.Init(googleVoiceSMSPageMonitorJS)
	w.Init(googleVoiceSMSInitScript)

	dev := newWebViewDevTools(w)
	stop := make(chan struct{})
	defer close(stop)
	go runGoogleVoiceSMSOutboundLoop(dataDir, dev, stop)
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
		s.LastError = "Waiting for Google Voice"
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

func runGoogleVoiceSMSBackgroundSupervisor(ctx context.Context, dataDir string) {
	t := time.NewTicker(1500 * time.Millisecond)
	defer t.Stop()
	var lastAttempt time.Time
	for {
		_, cfgPath, _, _, err := appPaths()
		if err == nil {
			if cfg, cfgErr := loadConfig(cfgPath, dataDir); cfgErr == nil {
				s := loadGoogleVoiceSMSRuntime(dataDir)
				want := cfg.Gmail.Method == GmailMethodGoogleVoice && s.Connected && !s.LoginActive
				if want && !googleVoiceSMSProcessAlive() && !s.Starting && time.Since(lastAttempt) > 5*time.Second {
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

// The old v0.46.34 listener lived as a second WebView in the Google Voice call
// process and shared that process's profile. Keep a tiny compatibility stub so
// the untouched calling code compiles, but force it to fall back without ever
// creating that broken shared-profile observer. Direct SMS now runs only in the
// independent process above.
func createGoogleVoiceSMSObserver(string, func() bool) (webview2.WebView, voiceDevTools, error) {
	return nil, nil, errors.New("direct Google Voice SMS now runs in its independent browser process")
}

func googleVoiceSMSWorkerMode() bool {
	return len(os.Args) > 1 && (os.Args[1] == "--google-voice-sms-worker" || os.Args[1] == "--google-voice-sms-login")
}

func googleVoiceSMSCallProcess() bool {
	return len(os.Args) > 1 && strings.EqualFold(os.Args[1], "--google-voice")
}
