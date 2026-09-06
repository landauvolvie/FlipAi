//go:build windows

package main

import (
	"context"
	"os"
	"time"
)

var copilotChatWorkerInstanceRelease func()

func init() {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	if mode == "--copilot-chat-worker" || mode == "--copilot-chat-login" {
		release, owner, err := acquireNamedInstance(`Local\FlipAi-CopilotChat-WebView`, "Microsoft Copilot Chat WebView owner")
		if err == nil {
			if !owner {
				os.Exit(0)
			}
			copilotChatWorkerInstanceRelease = release
		}
	}
	if mode != "--copilot-chat-login" && mode != "--copilot-chat-worker" {
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
				_ = platformStopCopilotChatWorker(dataDir)
				return
			}
		}
	}()
	copilotChatWorkerMain(dataDir, mode == "--copilot-chat-login")
	os.Exit(0)
}

// A saved Copilot login is restored only from the signed-in tray session. The
// first Connect flow may show a normal window; every subsequent worker remains
// off-screen and never makes the service/session-0 host own a WebView2 process.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	prepareCopilotChatRuntimeForTray(dataDir)
	go runCopilotChatBackgroundSupervisor(context.Background(), dataDir)
}
