package main

import (
	"fmt"
	"net/http"
	"time"
)

// Do not make renderer responsiveness a liveness test. An unauthenticated
// request is rejected before Runtime.evaluate and therefore tells us whether
// the one private loopback worker still exists without waiting on Grok's page.
// This is the same RAM-safety rule used by ChatGPT Chat.
func grokChatBrowserStillOpen(dataDir string) bool {
	s := loadGrokChatRuntime(dataDir)
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

func prepareGrokChatRuntimeForTray(dataDir string) {
	s := loadGrokChatRuntime(dataDir)
	if !s.Connected || grokChatBrowserStillOpen(dataDir) {
		return
	}
	mutateGrokChatRuntime(dataDir, func(v *GrokChatWebRuntime) {
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
}
