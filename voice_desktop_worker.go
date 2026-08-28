package main

import (
	"errors"
	"time"
)

// Google Voice, the desktop AI app, the virtual cables and the browser FlipAi
// opens for the audio-bridge download all need one thing in common: a signed-in
// interactive Windows desktop. None of it can happen in Session 0, the
// non-interactive session a service or a power-on scheduled task runs in.
//
// That matters because of "start before sign-in". It runs FlipAi's whole stack
// -- watchdog, background host, tray -- from a power-on scheduled task, in
// Session 0. The background host is where Google Voice was supervised and where
// the audio-bridge browser was opened, so with that option on, the host sat in
// a session with no desktop: Google Voice reported "no interactive desktop
// session" and never started, and pressing Set up opened a browser nobody could
// see. Signing in did not fix it, because the interactive session found the
// Session 0 host already answering the health port and never started its own.
//
// The fix is to move the desktop-touching work to a process that is always in
// the user's own session: the tray, which cannot exist without a desktop to put
// an icon on. This file is the portable half -- deciding what runs where, and
// the request channel the host uses to hand a desktop action to the interactive
// worker. The Windows half that actually draws the window lives in
// voice_desktop_worker_windows.go.

// voiceSessionInteractive reports whether this process is in an interactive
// desktop session. It is a var so the portable code can be tested and so the
// non-Windows build (which has no such notion) can treat every session as
// usable. The Windows build points it at the real check.
var voiceSessionInteractive = func() bool { return true }

// The desktop actions the host can ask the interactive worker to perform.
const (
	voiceDesktopOpen    = "open"    // show Google Voice in the panel
	voiceDesktopRestart = "restart" // tear a wedged window down and start again
)

// voiceDesktopRequestTTL bounds a handed-off request. A request the worker
// never picked up -- because no interactive session existed when it was made --
// must not fire the next time the user signs in and the worker starts.
const voiceDesktopRequestTTL = 45 * time.Second

// requestVoiceDesktopAction records a desktop action for the interactive worker
// to perform. The host calls this when it cannot act itself.
func requestVoiceDesktopAction(dataDir, action string) {
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.DesktopRequest = action
		s.DesktopRequestAt = time.Now()
	})
}

// takeVoiceDesktopAction returns a fresh pending action and clears it, so the
// worker performs each request exactly once. A stale request is dropped rather
// than run.
func takeVoiceDesktopAction(dataDir string) string {
	var action string
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		if s.DesktopRequest == "" {
			return
		}
		fresh := !s.DesktopRequestAt.IsZero() && time.Since(s.DesktopRequestAt) <= voiceDesktopRequestTTL
		if fresh {
			action = s.DesktopRequest
		}
		// Clear it either way: a stale request is consumed and dropped, never
		// left to fire on the next sign-in.
		s.DesktopRequest = ""
		s.DesktopRequestAt = time.Time{}
	})
	return action
}

// voiceOpenForUI performs the /open action -- put Google Voice in the panel --
// from wherever the host happens to be running. When the host is interactive it
// opens the window directly and reports the outcome synchronously, exactly as
// before. When it is not (Session 0 under "start before sign-in"), it hands the
// work to the interactive tray worker and waits for the outcome the worker
// records, so the button still reports success or a real reason.
func voiceOpenForUI(dataDir string) error {
	if voiceSessionInteractive() {
		return openGoogleVoiceWindow(dataDir, true)
	}
	since := time.Now()
	requestVoiceDesktopAction(dataDir, voiceDesktopOpen)
	return awaitVoiceDesktopOutcome(dataDir, since)
}

// voiceRestartForUI is the same delegation for Retry.
func voiceRestartForUI(dataDir string) {
	if voiceSessionInteractive() {
		platformRestartGoogleVoice(dataDir)
		return
	}
	requestVoiceDesktopAction(dataDir, voiceDesktopRestart)
}

// awaitVoiceDesktopOutcome waits for the interactive worker to bring Google
// Voice up, or to record why it could not, after a request was handed off.
func awaitVoiceDesktopOutcome(dataDir string, since time.Time) error {
	deadline := time.Now().Add(voiceWindowStartup)
	for time.Now().Before(deadline) {
		s := loadVoiceRuntime(dataDir)
		if s.BrowserRunning {
			return nil
		}
		if s.LastOpenError != "" && s.LastOpenAt.After(since) {
			return errors.New(s.LastOpenError)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("Google Voice could not be brought up in a signed-in desktop session. " +
		"If FlipAi is set to start before sign-in, the calling window can only open once you have signed in and opened FlipAi at least once.")
}
