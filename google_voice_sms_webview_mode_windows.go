//go:build windows

package main

import (
	"context"
	"os"
)

var googleVoiceSMSInstanceRelease func()

func init() {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	if mode != "--google-voice-sms-login" && mode != "--google-voice-sms-worker" {
		return
	}
	release, owner, err := acquireNamedInstance(`Local\FlipAi-GoogleVoice-SMS-WebView`, "Google Voice SMS WebView owner")
	if err == nil {
		if !owner {
			os.Exit(0)
		}
		googleVoiceSMSInstanceRelease = release
		defer release()
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		os.Exit(2)
	}
	if err := ensureDataDir(dataDir); err != nil {
		os.Exit(2)
	}
	if err := runGoogleVoiceSMSWebView(dataDir, mode == "--google-voice-sms-login"); err != nil {
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.Running = false
			s.Starting = false
			s.ListenerRunning = false
			s.Ready = false
			s.LastEvent = "browser-error"
			s.LastError = err.Error()
		})
	}
	os.Exit(0)
}

// The tray is the signed-in desktop owner. If a user already connected the
// dedicated Google Voice SMS profile, restore its hidden listener after app or
// Windows restart without involving the Google Voice calling process.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	go runGoogleVoiceSMSBackgroundSupervisor(context.Background(), dataDir)
}
