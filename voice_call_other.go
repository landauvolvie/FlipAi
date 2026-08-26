//go:build !windows

package main

import "errors"

func platformVoiceConfigChanged(dataDir string, cfg VoiceCallConfig) {}
func platformOpenGoogleVoice(dataDir string, show bool) error {
	return errors.New("Google Voice calling is available only in the Windows FlipAi app")
}
func platformTestAgentVoice(cfg VoiceCallConfig, agent string) error {
	return errors.New("agent voice calling is available only on Windows")
}
func platformWebView2Runtime() string { return "" }

func platformVoiceStillOpen() bool { return false }

// platformEnsureGoogleVoice starts the Google Voice window if it is not
// already running. There is no such window off Windows.
func platformEnsureGoogleVoice(dataDir string) {}

// platformRestartGoogleVoice restarts the window process. There is none off
// Windows.
func platformRestartGoogleVoice(dataDir string) {}

func platformSignOutGoogleVoice(dataDir string) error {
	return errors.New("Google Voice calling is available only in the Windows FlipAi app")
}

func platformOpenCodexVoice(dataDir string) error {
	return errors.New("the Codex voice window is available only in the Windows FlipAi app")
}
