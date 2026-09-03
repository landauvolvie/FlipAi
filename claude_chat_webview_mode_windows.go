//go:build windows

package main

import (
	"context"
	"os"
	"time"
)

var claudeChatWorkerInstanceRelease func()

func init() {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	if mode == "--claude-chat-worker" || mode == "--claude-chat-login" {
		release, owner, err := acquireNamedInstance(`Local\FlipAi-ClaudeChat-WebView`, "Claude Chat WebView owner")
		if err == nil {
			if !owner {
				os.Exit(0)
			}
			claudeChatWorkerInstanceRelease = release
		}
	}
	if mode != "--claude-chat-login" && mode != "--claude-chat-worker" {
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
				_ = platformStopClaudeChatWorker(dataDir)
				return
			}
		}
	}()
	claudeChatWorkerMain(dataDir, mode == "--claude-chat-login")
	os.Exit(0)
}

// The tray is guaranteed to live in the signed-in desktop session. Keeping the
// supervisor here means a saved Claude login is restored after app/Windows
// restart without making the service/session-0 host own a WebView2 process.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	prepareClaudeChatRuntimeForTray(dataDir)
	go runClaudeChatBackgroundSupervisor(context.Background(), dataDir)
}
