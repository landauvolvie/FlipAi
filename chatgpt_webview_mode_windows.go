//go:build windows

package main

import (
	"os"
	"time"
)

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
	// Quit/update/uninstall writes the same quit flag every FlipAi process
	// watches. Once the WebView endpoint is up, asking it to stop lets WebView2
	// close its profile cleanly rather than leaving files locked for Setup.
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			if quitRequested(dataDir) {
				_ = platformStopChatGPTWorker(dataDir)
				return
			}
		}
	}()
	chatGPTWorkerMain(dataDir, os.Args[1] == "--chatgpt-login")
	os.Exit(0)
}
