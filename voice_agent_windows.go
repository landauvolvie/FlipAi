//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
)

// The Codex voice window is the other half of a phone call: a hidden browser
// FlipAi keeps on chatgpt.com, signed in with the user's own ChatGPT account,
// whose voice mode talks to the caller. It lives inside the same
// --google-voice process as the Google Voice window because the two pages
// exchange their audio handshake through in-process bindings, and because they
// share a lifetime: calls need both or neither.
const (
	codexVoiceWindowTitle = "FlipAi — Codex Voice"
	codexVoiceWebURL      = "https://chatgpt.com/"
)

func codexVoiceProfilePath(dataDir string) string {
	return filepath.Join(dataDir, "codex-voice-webview")
}

func codexVoiceHWND() uintptr {
	title, _ := syscall.UTF16PtrFromString(codexVoiceWindowTitle)
	h, _, _ := procVoiceFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
	return h
}

// voiceWebViewCreateMu serializes WebView2 creation between the Google Voice
// window and the Codex voice window. Creation passes browser switches through
// one process-wide environment variable, so two concurrent creations could
// read each other's switches.
var voiceWebViewCreateMu sync.Mutex

// superviseCodexVoice owns the Codex voice window for the life of the
// --google-voice process: created at startup, recreated if it is ever closed,
// closed when the process goes away.
func superviseCodexVoice(dataDir string, link *voiceAgentLink, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		if quitRequested(dataDir) {
			return
		}
		if err := runCodexVoiceWindow(dataDir, link, stop); err != nil {
			recordCodexVoiceGone(dataDir, err.Error())
		} else {
			recordCodexVoiceGone(dataDir, "")
		}
		select {
		case <-stop:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func runCodexVoiceWindow(dataDir string, link *voiceAgentLink, stop <-chan struct{}) error {
	// A Win32 message pump only works on the thread that created the window,
	// and this window lives on its own thread beside the Google Voice one.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(codexVoiceProfilePath(dataDir), 0700); err != nil {
		return fmt.Errorf("could not create the Codex voice browser profile folder: %w", err)
	}
	voiceWebViewCreateMu.Lock()
	// The same background-timer and autoplay switches the Google Voice window
	// needs: this window is minimized too, and it has to keep decoding and
	// producing audio while nobody looks at it and while the PC is locked.
	_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", voiceBrowserArguments)
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: false,
		DataPath:  codexVoiceProfilePath(dataDir),
		WindowOptions: webview2.WindowOptions{
			Title:  codexVoiceWindowTitle,
			Width:  980,
			Height: 720,
			Center: true,
		},
	})
	voiceWebViewCreateMu.Unlock()
	if w == nil {
		if v := platformWebView2Runtime(); v == "" {
			return errors.New("Windows could not create the Codex voice browser window: the Microsoft Edge WebView2 Runtime is not installed on this PC")
		}
		return errors.New("Windows could not create the Codex voice browser window with the Edge WebView2 Runtime; restarting Windows usually clears this")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(720, 540, webview2.HintMin)
	if chromium := voiceChromium(w); chromium != nil {
		chromium.SetGlobalPermission(edge.CoreWebView2PermissionStateAllow)
	}

	_ = w.Bind("flipCodexRelaySend", link.AgentSend)
	_ = w.Bind("flipCodexRelayRecv", link.AgentRecv)
	_ = w.Bind("flipCodexStatus", func(href string, signedIn, voiceActive bool, controls, lastError string) {
		recordCodexVoiceStatus(dataDir, href, signedIn, voiceActive, controls, lastError)
	})
	w.Init(codexVoiceInitScript)

	watchDone := make(chan struct{})
	defer close(watchDone)
	hwnd := uintptr(w.Window())
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-stop:
				procVoicePostMessage.Call(hwnd, voiceWMClose, 0, 0)
				return
			case <-t.C:
				if quitRequested(dataDir) {
					procVoicePostMessage.Call(hwnd, voiceWMClose, 0, 0)
					return
				}
			}
		}
	}()

	w.Navigate(codexVoiceWebURL)
	// Fully hidden, not minimized: this window only exists to be signed in
	// once and then to talk. Hiding it keeps it out of the taskbar and stops
	// Windows treating it as the process's main window -- the Google Voice
	// window is the one everything else looks for by title. The browser
	// switches shared with the Google Voice window keep its page running at
	// full speed while hidden and while the PC is locked.
	procVoiceShowWindow.Call(hwnd, voiceSWHide)
	w.Run()
	return nil
}

// platformOpenCodexVoice puts the Codex voice window on screen so the user can
// sign in to ChatGPT there. The window normally exists already, minimized
// beside the Google Voice window; if the whole process is down it is started
// first.
func platformOpenCodexVoice(dataDir string) error {
	if h := codexVoiceHWND(); h != 0 {
		procVoiceShowWindow.Call(h, voiceSWRestore)
		bringToFront(h)
		return nil
	}
	if err := platformOpenGoogleVoice(dataDir, false); err != nil {
		return err
	}
	deadline := time.Now().Add(voiceWindowStartup)
	for time.Now().Before(deadline) {
		if h := codexVoiceHWND(); h != 0 {
			procVoiceShowWindow.Call(h, voiceSWRestore)
			bringToFront(h)
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	rt := loadVoiceRuntime(dataDir)
	if rt.CodexVoice.LastError != "" {
		return fmt.Errorf("the Codex voice window did not appear: %s", rt.CodexVoice.LastError)
	}
	return errors.New("the Codex voice window did not appear; restart the Google Voice window from Connections and try again")
}

// voiceSignOutFlagPath marks a pending Google sign-out. The browser profile
// belongs to the --google-voice process, so the deletion has to happen there,
// between one window and the next; the flag is how the request crosses
// processes.
func voiceSignOutFlagPath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-signout")
}

// platformSignOutGoogleVoice forgets the Google account the Google Voice
// window is signed in to by deleting its browser profile. The window is closed
// first so the profile is not held open, and the window process recreates a
// fresh, signed-out window afterwards when calling is on.
func platformSignOutGoogleVoice(dataDir string) error {
	flag := voiceSignOutFlagPath(dataDir)
	if err := os.WriteFile(flag, []byte(time.Now().Format(time.RFC3339)), 0600); err != nil {
		return err
	}
	if !googleVoiceProcessAlive() {
		// Nothing holds the profile; it can be removed right here.
		_ = os.Remove(flag)
		if err := removeVoiceProfile(dataDir); err != nil {
			return err
		}
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.SignedIn = false
			s.LastEvent = "signed-out"
		})
		return nil
	}
	if h := googleVoiceHWND(); h != 0 {
		procVoicePostMessage.Call(h, voiceWMClose, 0, 0)
	}
	// The window process consumes the flag after its window closes: it deletes
	// the profile and starts a fresh window. Waiting for the flag to disappear
	// is waiting for the sign-out to actually have happened.
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(flag); os.IsNotExist(err) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors.New("the Google Voice window did not complete the sign-out; quit FlipAi from the tray, start it again, and retry")
}

// consumePendingVoiceSignOut runs in the window process between windows: if a
// sign-out was requested, the profile is deleted now, while no browser holds
// it.
func consumePendingVoiceSignOut(dataDir string) {
	flag := voiceSignOutFlagPath(dataDir)
	if _, err := os.Stat(flag); err != nil {
		return
	}
	if err := removeVoiceProfile(dataDir); err != nil {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.LastError = "Sign-out could not remove the saved Google session: " + err.Error()
		})
	} else {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.SignedIn = false
			s.LastError = ""
			s.LastEvent = "signed-out"
		})
	}
	_ = os.Remove(flag)
}

// removeVoiceProfile deletes the Google Voice browser profile, retrying while
// the browser processes that held it finish exiting.
func removeVoiceProfile(dataDir string) error {
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for {
		err = os.RemoveAll(voiceProfilePath(dataDir))
		if err == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
