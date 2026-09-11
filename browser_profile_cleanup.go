package main

import (
	"context"
	"os"
	"time"
)

// removeBrowserProfileWithRetry waits for WebView2 helper processes to release
// profile lock files after the owning window has been terminated. WebView2 can
// keep EBWebView/lockfile open briefly after the control endpoint has returned,
// so a single immediate os.RemoveAll produces a false disconnect failure.
func removeBrowserProfileWithRetry(path string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		last = os.RemoveAll(path)
		if last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// The tray also performs best-effort cleanup for every browser provider after a
// disconnect. This closes the race where a handler has already asked WebView2
// to terminate but one helper still owns EBWebView/lockfile for a few hundred
// milliseconds. It never touches a connected, starting, running, or login
// profile.
func runBrowserProfileCleanupSupervisor(ctx context.Context, dataDir string) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	cleanup := func(path string, idle bool) {
		if idle {
			_ = os.RemoveAll(path)
		}
	}
	for {
		g := loadChatGPTRuntime(dataDir)
		cleanup(chatGPTProfilePath(dataDir), !g.Connected && !g.Running && !g.Starting && !g.LoginActive)
		h := loadClaudeChatRuntime(dataDir)
		cleanup(claudeChatProfilePath(dataDir), !h.Connected && !h.Running && !h.Starting && !h.LoginActive)
		m := loadGeminiChatRuntime(dataDir)
		cleanup(geminiChatProfilePath(dataDir), !m.Connected && !m.Running && !m.Starting && !m.LoginActive)
		x := loadGrokChatRuntime(dataDir)
		cleanup(grokChatProfilePath(dataDir), !x.Connected && !x.Running && !x.Starting && !x.LoginActive)
		p := loadCopilotChatRuntime(dataDir)
		cleanup(copilotChatProfilePath(dataDir), !p.Connected && !p.Running && !p.Starting && !p.LoginActive)
		u := loadMuseChatRuntime(dataDir)
		cleanup(museChatProfilePath(dataDir), !u.Connected && !u.Running && !u.Starting && !u.LoginActive)

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
