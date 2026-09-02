//go:build windows

package main

import "os"

// The dedicated ChatGPT WebView runs in its own FlipAi process so its Win32
// message loop and browser profile can stay alive independently of the desktop
// control window. Handling these two private modes in init keeps the main
// launcher/watchdog switch focused on user-facing process modes.
func init() {
	if len(os.Args) < 2 || (os.Args[1] != "--chatgpt-login" && os.Args[1] != "--chatgpt-worker") {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		os.Exit(2)
	}
	if err := ensureDataDir(dataDir); err != nil {
		os.Exit(2)
	}
	chatGPTWorkerMain(dataDir, os.Args[1] == "--chatgpt-login")
	os.Exit(0)
}
