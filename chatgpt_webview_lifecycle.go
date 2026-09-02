package main

import (
	"fmt"
	"net/http"
	"time"
)

// chatGPTBrowserStillOpen is used by the tray supervisor and update/uninstall
// shutdown waits. It deliberately does NOT use the authenticated /health call:
// that handler also checks the ChatGPT page and can wait on Runtime.evaluate.
// A busy renderer occasionally took longer than the supervisor's old 500 ms
// deadline, which made a perfectly live worker look dead. The supervisor then
// cleared its control state and spawned another hidden WebView; repeated false
// negatives could leave many Chromium/WebView2 process trees consuming most of
// the machine's RAM.
//
// An unauthenticated request is a cheap process-liveness probe. A real FlipAi
// worker rejects it with 403 before touching the renderer. Accept 200 as well so
// this remains compatible if /health is ever made public. Authentication and
// signed-in readiness are still checked separately by waitForChatGPTReady.
func chatGPTBrowserStillOpen(dataDir string) bool {
	s := loadChatGPTRuntime(dataDir)
	if !s.Running || s.ControlPort < 1 || s.ControlToken == "" {
		return false
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", s.ControlPort))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusOK
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
