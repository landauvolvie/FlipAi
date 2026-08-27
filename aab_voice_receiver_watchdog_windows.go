//go:build windows

package main

import (
	"os"
	"time"
)

var procVoiceRedrawWindow = voiceUser32.NewProc("RedrawWindow")

const (
	rdwInvalidate  = 0x0001
	rdwErase       = 0x0004
	rdwAllChildren = 0x0080
	rdwUpdateNow   = 0x0100
)

// The Google Voice receiver is supposed to be infrastructure, not a window the
// user has to babysit. Keep a second lightweight liveness watch in the signed-in
// desktop session so a crashed/closed Edge receiver is restarted promptly, and
// force a repaint while it is docked. The repaint cures the black native-window
// surface that can otherwise remain after Windows moves an Edge app-mode window
// between minimized/background and docked states.
func init() {
	if len(os.Args) < 2 || (os.Args[1] != "--host" && os.Args[1] != "--watchdog") {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	go runGoogleVoiceReceiverWatchdog(dataDir)
}

func runGoogleVoiceReceiverWatchdog(dataDir string) {
	time.Sleep(3 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if quitRequested(dataDir) {
			return
		}
		if !voiceInteractiveSession() {
			continue
		}
		cfg := loadVoiceCallConfig(dataDir)
		if !cfg.Enabled {
			continue
		}
		hwnd := googleVoiceHWND()
		if hwnd == 0 {
			_ = platformOpenGoogleVoice(dataDir, false)
			continue
		}
		if loadVoiceRuntime(dataDir).Docked {
			procVoiceRedrawWindow.Call(hwnd, 0, 0, rdwInvalidate|rdwErase|rdwAllChildren|rdwUpdateNow)
		}
	}
}
