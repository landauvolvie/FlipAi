package main

import (
	"errors"
	"time"
)

const googleVoiceSMSDesktopRequestTTL = 45 * time.Second

func requestGoogleVoiceSMSDesktopLogin(dataDir string) {
	mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
		s.DesktopRequest = "login"
		s.DesktopRequestAt = time.Now()
		s.Starting = true
		s.LastEvent = "waiting-for-interactive-desktop"
		s.LastError = "Waiting for FlipAi's interactive tray to open the Google Voice SMS sign-in window"
	})
}

func takeGoogleVoiceSMSDesktopLogin(dataDir string) bool {
	take := false
	mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
		if s.DesktopRequest != "login" {
			return
		}
		if !s.DesktopRequestAt.IsZero() && time.Since(s.DesktopRequestAt) <= googleVoiceSMSDesktopRequestTTL {
			take = true
		}
		s.DesktopRequest = ""
		s.DesktopRequestAt = time.Time{}
	})
	return take
}

func googleVoiceSMSLoginForUI(dataDir string) error {
	if voiceSessionInteractive() {
		return platformStartGoogleVoiceSMSLogin(dataDir)
	}
	requestGoogleVoiceSMSDesktopLogin(dataDir)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s := loadGoogleVoiceSMSRuntime(dataDir)
		if s.Running && s.Visible && s.LoginActive {
			return nil
		}
		if s.LastEvent == "sign-in-window-error" && s.LastError != "" {
			return errors.New(s.LastError)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("FlipAi could not open the Google Voice SMS sign-in window in the signed-in Windows desktop session")
}
