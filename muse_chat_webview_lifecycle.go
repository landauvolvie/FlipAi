package main

import (
	"context"
	"net/http"
	"time"
)

func museChatBrowserStillOpen(dataDir string) bool {
	s := loadMuseChatRuntime(dataDir)
	if s.ControlPort < 1 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+itoa(s.ControlPort)+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 700 * time.Millisecond}).Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusForbidden
}

func prepareMuseChatRuntimeForTray(dataDir string) {
	s := loadMuseChatRuntime(dataDir)
	if !s.Running && !s.Starting {
		return
	}
	if museChatBrowserStillOpen(dataDir) {
		return
	}
	mutateMuseChatRuntime(dataDir, func(v *MuseChatWebRuntime) {
		v.Running = false
		v.Starting = false
		v.Visible = false
		v.LoginActive = false
		v.SignedIn = false
		v.ControlPort = 0
		v.ControlToken = ""
		if v.Connected {
			v.LastEvent = "background-restart-pending"
		}
	})
}
