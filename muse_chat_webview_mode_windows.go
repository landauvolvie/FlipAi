//go:build windows

package main

import (
	"context"
	"os"
	"time"
)

var museChatWorkerInstanceRelease func()

func init() {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	if mode == "--muse-chat-worker" || mode == "--muse-chat-login" {
		release, owner, err := acquireNamedInstance(`Local\FlipAi-MuseChat-WebView`, "Muse WebView owner")
		if err == nil {
			if !owner {
				os.Exit(0)
			}
			museChatWorkerInstanceRelease = release
		}
	}
	if mode != "--muse-chat-login" && mode != "--muse-chat-worker" {
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
				_ = platformStopMuseChatWorker(dataDir)
				return
			}
		}
	}()
	museChatWorkerMain(dataDir, mode == "--muse-chat-login")
	os.Exit(0)
}

func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	prepareMuseChatRuntimeForTray(dataDir)
	go runMuseChatBackgroundSupervisor(context.Background(), dataDir)
}
