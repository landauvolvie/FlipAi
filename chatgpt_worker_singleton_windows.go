//go:build windows

package main

import (
	"os"
	"time"
)

// Keep one hard process-level owner for the hidden ChatGPT WebView. Runtime
// metadata is useful for reconnecting to the worker, but it is not a lock: a
// slow renderer used to make the supervisor clear that metadata and spawn a
// replacement while the original Chromium/WebView2 tree was still alive.
//
// The named Windows mutex is released by the kernel if the worker crashes, so a
// genuine failure can still be recovered automatically. A duplicate worker
// exits before it creates WebView2 and therefore cannot consume another browser
// process tree worth of RAM.
var chatGPTWorkerInstanceRelease func()

func init() {
	if len(os.Args) < 2 || os.Args[1] != "--chatgpt-worker" {
		return
	}
	release, owner, err := acquireNamedInstance(`Local\FlipAi-ChatGPT-WebView`, "ChatGPT WebView worker")
	if err == nil {
		if !owner {
			os.Exit(0)
		}
		// Hold the handle for the lifetime of this process. Windows releases the
		// mutex automatically on process termination; retaining the closure keeps
		// the handle reachable and also gives tests/debug builds an explicit owner.
		chatGPTWorkerInstanceRelease = release
	}

	// The WebView is a detached child so it can survive host restarts. It must
	// not survive an explicit FlipAi Quit/update/uninstall, though. Watching the
	// same quit flag as the host prevents old hidden browser trees from being
	// orphaned when the watchdog terminates the tray process.
	if dataDir, _, _, _, e := appPaths(); e == nil {
		go func() {
			t := time.NewTicker(250 * time.Millisecond)
			defer t.Stop()
			for range t.C {
				if quitRequested(dataDir) {
					os.Exit(0)
				}
			}
		}()
	}
}
