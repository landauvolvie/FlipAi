//go:build windows

package main

import "time"

func init() {
	// The portable delegation logic asks this process whether it has a desktop.
	// On Windows that is a real question; everywhere else every session counts.
	voiceSessionInteractive = voiceInteractiveSession
}

// startVoiceDesktopWorker runs everything about Google Voice that needs a
// signed-in desktop, in the one FlipAi process that is guaranteed to have one:
// the tray. See voice_desktop_worker.go for why this cannot live in the
// background host.
//
// It is idempotent and safe to call from a process that turns out not to be
// interactive -- the Session 0 tray the power-on task also starts -- because it
// does nothing there. Only the tray in the user's own session takes over Google
// Voice and the audio-bridge setup endpoint.
func startVoiceDesktopWorker(dataDir, mainListen string) {
	if !voiceInteractiveSession() {
		return
	}
	go startVoiceAudioInstallServer(dataDir, mainListen)
	go superviseGoogleVoice(dataDir)
	go serveVoiceDesktopRequests(dataDir)
}

// serveVoiceDesktopRequests performs the desktop actions the background host
// handed off because it could not do them itself. It polls faster than the
// supervisor's own 4-second cadence so a button press is not left waiting.
func serveVoiceDesktopRequests(dataDir string) {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		if quitRequested(dataDir) {
			return
		}
		switch takeVoiceDesktopAction(dataDir) {
		case voiceDesktopOpen:
			go startGoogleVoiceInBackground(dataDir)
		case voiceDesktopRestart:
			go platformRestartGoogleVoice(dataDir)
		}
	}
}
