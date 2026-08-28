//go:build !windows

package main

// startVoiceDesktopWorker is the interactive-session worker for Google Voice.
// There is no such window off Windows, so nothing runs here.
func startVoiceDesktopWorker(dataDir, mainListen string) {}
