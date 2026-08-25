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
