//go:build windows

package main

import (
	"context"
	"os"
)

// The tray is the FlipAi process that lives in the signed-in user's desktop
// session. Start every persistent browser-agent supervisor here so all saved
// WebView sessions are restored immediately after FlipAi/Windows restarts.
// The host can run before interactive sign-in and must not own WebView2.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}

	// Running/Starting are process facts persisted only so the UI can report
	// them. Clear stale values before each supervisor begins; Connected remains
	// the durable "restore this private profile" preference.
	prepareChatGPTRuntimeForTray(dataDir)
	prepareClaudeChatRuntimeForTray(dataDir)
	prepareGeminiChatRuntimeForTray(dataDir)
	prepareGrokChatRuntimeForTray(dataDir)
	prepareCopilotChatRuntimeForTray(dataDir)
	prepareMuseChatRuntimeForTray(dataDir)

	ctx := context.Background()
	go runBrowserProfileCleanupSupervisor(ctx, dataDir)
	go runChatGPTBackgroundSupervisor(ctx, dataDir)
	go runClaudeChatBackgroundSupervisor(ctx, dataDir)
	go runGeminiChatBackgroundSupervisor(ctx, dataDir)
	go runGrokChatBackgroundSupervisor(ctx, dataDir)
	go runCopilotChatBackgroundSupervisor(ctx, dataDir)
	go runMuseChatBackgroundSupervisor(ctx, dataDir)
}
