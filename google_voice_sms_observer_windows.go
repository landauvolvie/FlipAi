//go:build windows

package main

import (
	"errors"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

const googleVoiceSMSObserverWindowTitle = "FlipAi — Google Voice SMS Listener"

// googleVoiceSMSObserverIsolationScript makes the dedicated SMS listener a
// read/send surface only. It shares the signed-in Google Voice profile with the
// normal Voice window, but it never opens a microphone, plays call audio, or
// raises a second Windows notification. The visible/call WebView is untouched.
const googleVoiceSMSObserverIsolationScript = `
(() => {
  if (window.__flipAiSMSObserverIsolated) return;
  window.__flipAiSMSObserverIsolated = true;
  try {
    if (navigator.mediaDevices && navigator.mediaDevices.getUserMedia) {
      navigator.mediaDevices.getUserMedia = () => Promise.reject(new DOMException('Disabled in FlipAi SMS listener', 'NotAllowedError'));
    }
    if (navigator.mediaDevices && navigator.mediaDevices.getDisplayMedia) {
      navigator.mediaDevices.getDisplayMedia = () => Promise.reject(new DOMException('Disabled in FlipAi SMS listener', 'NotAllowedError'));
    }
  } catch (_) {}
  try {
    HTMLMediaElement.prototype.play = function() {
      try { this.muted = true; this.volume = 0; } catch (_) {}
      return Promise.resolve();
    };
  } catch (_) {}
  try {
    const QuietNotification = function(title, options) {
      this.title = String(title || '');
      this.body = String((options && options.body) || '');
      this.tag = String((options && options.tag) || '');
      this.data = options && options.data;
    };
    QuietNotification.prototype.close = function() {};
    QuietNotification.prototype.addEventListener = function() {};
    QuietNotification.prototype.removeEventListener = function() {};
    QuietNotification.prototype.dispatchEvent = function() { return false; };
    QuietNotification.requestPermission = () => Promise.resolve('granted');
    Object.defineProperty(QuietNotification, 'permission', {get: () => 'granted'});
    window.Notification = QuietNotification;
    if (window.ServiceWorkerRegistration && ServiceWorkerRegistration.prototype.showNotification) {
      ServiceWorkerRegistration.prototype.showNotification = () => Promise.resolve();
    }
  } catch (_) {}
})()`

func googleVoiceMessagesURLFromPage(page string) string {
	const fallback = "https://voice.google.com/u/0/messages"
	u, err := url.Parse(strings.TrimSpace(page))
	if err != nil || !strings.EqualFold(u.Hostname(), "voice.google.com") {
		return fallback
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "u" {
		if _, err := strconv.Atoi(parts[1]); err == nil {
			return "https://voice.google.com/u/" + parts[1] + "/messages"
		}
	}
	return fallback
}

func googleVoiceSMSObserverURL(dataDir string) string {
	return googleVoiceMessagesURLFromPage(loadVoiceRuntime(dataDir).Page)
}

func setGoogleVoiceSMSListenerError(dataDir string, err error) {
	mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
		s.ListenerRunning = false
		s.Ready = false
		if err != nil {
			s.LastError = err.Error()
		}
	})
}

// createGoogleVoiceSMSObserver creates a second, off-screen Google Voice view
// only while Direct Google Voice SMS is selected. It uses the exact same
// WebView2 user-data folder as the normal Google Voice window, so the user signs
// in once, but the two views no longer fight over navigation: calls can remain
// on whatever surface they need while this view stays on Messages.
func createGoogleVoiceSMSObserver(dataDir string, enabled func() bool) (webview2.WebView, voiceDevTools, error) {
	// Do not change WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS here. The main Google
	// Voice view has already chosen the render mode that actually works on this
	// PC, and WebView2 requires every environment sharing one user-data folder
	// to use compatible options. Inherit that exact environment setting.
	parkX, parkY := parkedWindowOrigin()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: false,
		DataPath:  voiceProfilePath(dataDir),
		WindowOptions: webview2.WindowOptions{
			Title:      googleVoiceSMSObserverWindowTitle,
			Width:      900,
			Height:     700,
			X:          parkX - 120,
			Y:          parkY - 120,
			Position:   true,
			ExStyle:    wsExToolWin | wsExNoActivate,
			NoActivate: true,
		},
	})
	if w == nil {
		err := errors.New("Windows could not create the dedicated Google Voice SMS listener")
		setGoogleVoiceSMSListenerError(dataDir, err)
		return nil, nil, err
	}
	c := voiceChromium(w)
	if c == nil {
		w.Destroy()
		err := errors.New("the Google Voice SMS listener has no WebView2 control channel")
		setGoogleVoiceSMSListenerError(dataDir, err)
		return nil, nil, err
	}
	_ = w.Bind("flipVoiceSMS", func(payload string) {
		if enabled != nil && !enabled() {
			return
		}
		if err := appendDirectGoogleVoiceSMS(dataDir, payload); err != nil {
			log.Printf("Google Voice SMS listener: %v", err)
			return
		}
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.LastInboundAt = time.Now()
		})
	})
	w.Init(googleVoiceSMSObserverIsolationScript)
	w.Init(googleVoiceSMSInitScript)
	target := googleVoiceSMSObserverURL(dataDir)
	mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
		s.ListenerRunning = true
		s.Ready = false
		s.Page = target
		s.LastError = "Starting the Google Voice Messages listener"
	})
	w.Navigate(target)
	d := newWebViewDevTools(w)
	// This process exists exactly as long as the Google Voice window. A nil stop
	// channel therefore intentionally means "probe until this process exits".
	go runGoogleVoiceSMSHealthLoop(dataDir, d, nil)
	return w, d, nil
}

type googleVoiceSMSProbe struct {
	Href  string `json:"href"`
	Host  string `json:"host"`
	Path  string `json:"path"`
	Ready string `json:"ready"`
}

const googleVoiceSMSProbeJS = `({href:String(location.href||''),host:String(location.hostname||'').toLowerCase(),path:String(location.pathname||''),ready:String(document.readyState||'')})`

func probeGoogleVoiceSMSListener(d voiceDevTools) (googleVoiceSMSProbe, error) {
	var p googleVoiceSMSProbe
	if err := voiceEval(d, googleVoiceSMSProbeJS, false, &p); err != nil {
		return p, err
	}
	if p.Host != "voice.google.com" {
		return p, errors.New("Google Voice SMS listener left voice.google.com")
	}
	if !strings.Contains(strings.ToLower(p.Path), "/messages") {
		return p, errors.New("Google Voice SMS listener is not on the Messages page")
	}
	if p.Ready != "interactive" && p.Ready != "complete" {
		return p, errors.New("Google Voice SMS Messages page is still loading")
	}
	return p, nil
}

func runGoogleVoiceSMSHealthLoop(dataDir string, d voiceDevTools, stop <-chan struct{}) {
	if d == nil {
		setGoogleVoiceSMSListenerError(dataDir, errors.New("Google Voice SMS listener has no browser control channel"))
		return
	}
	probe := func() {
		p, err := probeGoogleVoiceSMSListener(d)
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.ListenerRunning = true
			s.LastProbeAt = time.Now()
			if p.Href != "" {
				s.Page = p.Href
			}
			if err != nil {
				s.Ready = false
				s.LastError = err.Error()
				return
			}
			s.Ready = true
			s.LastError = ""
		})
	}
	probe()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	if stop != nil {
		defer mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.ListenerRunning = false
			s.Ready = false
		})
	}
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			probe()
		}
	}
}
