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
