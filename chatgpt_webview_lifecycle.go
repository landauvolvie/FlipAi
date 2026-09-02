package main

import (
	"context"
	"net/http"
	"time"
)

// chatGPTBrowserStillOpen is used by update/uninstall shutdown waits. The
// runtime file can be stale after a crash, so a live authenticated health reply
// is the authority rather than Running alone.
func chatGPTBrowserStillOpen(dataDir string) bool {
	s := loadChatGPTRuntime(dataDir)
	if !s.Running || s.ControlPort < 1 || s.ControlToken == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, code, err := chatGPTControlRequest(ctx, s, http.MethodGet, "/health", nil)
	return err == nil && code == http.StatusOK
}

// prepareChatGPTRuntimeForTray discards process-only flags left behind when
// Windows terminated FlipAi without letting the WebView write its shutdown
// state. Connected and the conversation id are durable; Running, SignedIn and
// the loopback control endpoint only describe a live process.
func prepareChatGPTRuntimeForTray(dataDir string) {
	s := loadChatGPTRuntime(dataDir)
	if !s.Connected || chatGPTBrowserStillOpen(dataDir) {
		return
	}
	mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) {
		v.Running = false
		v.Starting = false
		v.Visible = false
		v.LoginActive = false
		v.SignedIn = false
		v.ControlPort = 0
		v.ControlToken = ""
		v.LastEvent = "background-restart-pending"
		v.LastError = ""
	})
	chatGPTActivity(dataDir, "info", "chatgpt-session", "Windows/app restart detected; discarded stale ChatGPT browser process state and queued the saved session for invisible restore.", 0)
}
