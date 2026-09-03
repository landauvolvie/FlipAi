//go:build windows

package main

import (
	"context"
	"os"
	"time"
)

var grokChatWorkerInstanceRelease func()

func init() {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	if mode == "--grok-chat-worker" || mode == "--grok-chat-login" {
		release, owner, err := acquireNamedInstance(`Local\FlipAi-GrokChat-WebView`, "Grok Chat WebView owner")
		if err == nil {
			if !owner {
				os.Exit(0)
			}
			grokChatWorkerInstanceRelease = release
		}
	}
	if mode != "--grok-chat-login" && mode != "--grok-chat-worker" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		os.Exit(2)
	}
	if err := ensureDataDir(dataDir); err != nil {
		os.Exit(2)
	}
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			if quitRequested(dataDir) {
				_ = platformStopGrokChatWorker(dataDir)
				return
			}
		}
	}()
	grokChatWorkerMain(dataDir, mode == "--grok-chat-login")
	os.Exit(0)
}

// The tray is guaranteed to live in the signed-in desktop session. Keeping the
// supervisor here means a saved Grok login is restored after app/Windows
// restart without making the service/session-0 host own a WebView2 process.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	prepareGrokChatRuntimeForTray(dataDir)
	go runGrokChatBackgroundSupervisor(context.Background(), dataDir)
}
