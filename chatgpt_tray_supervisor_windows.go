//go:build windows

package main

import (
	"context"
	"os"
)

// The tray is the FlipAi process that lives in the signed-in user's desktop
// session. Start the persistent ChatGPT supervisor there so a previously
// connected WebView is restored invisibly as soon as FlipAi starts, including
// after a Windows restart. This init hook is intentionally limited to --tray;
// the host may run before interactive sign-in and must not own WebView2.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	go runChatGPTBackgroundSupervisor(context.Background(), dataDir)
}
