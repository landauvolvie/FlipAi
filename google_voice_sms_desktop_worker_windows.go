//go:build windows

package main

import (
	"os"
	"time"
)

// The tray always belongs to the signed-in Windows desktop. It consumes login
// requests handed off by a Session-0 background host so Connect can never open
// an invisible Google sign-in window.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			if quitRequested(dataDir) {
				return
			}
			if takeGoogleVoiceSMSDesktopLogin(dataDir) {
				if err := platformStartGoogleVoiceSMSLogin(dataDir); err != nil {
					mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
						s.Starting = false
						s.LastEvent = "sign-in-window-error"
						s.LastError = err.Error()
					})
				}
			}
		}
	}()
}
