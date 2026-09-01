package main

import (
	"os"
	"strings"
	"testing"
)

// Gmail is the transport. Once its credentials are usable, watching the inbox
// must not depend on phone routing or the retired global security-code fields.
// Those checks belong to each message after it arrives.
func TestStartBridgeDoesNotGateGmailMonitoringOnSMSSetup(t *testing.T) {
	raw, err := os.ReadFile("webui.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, "func (a *App) startBridge(ctx context.Context)")
	if start < 0 {
		t.Fatal("startBridge not found")
	}
	body := src[start:]
	if end := strings.Index(body, "\n}"); end >= 0 {
		body = body[:end+2]
	}

	for _, forbidden := range []string{
		"cfg.Security.CodeHash",
		"normalizeAllowedPhoneList(cfg.GoogleVoice.AllowedFrom)",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Gmail monitoring is still blocked by SMS setup: found %q in startBridge", forbidden)
		}
	}

	runAt := strings.Index(body, "go b.Run(ctx)")
	liveAt := strings.Index(body, "a.startClaudeLive(ctx, cfg, b)")
	if runAt < 0 {
		t.Fatal("startBridge does not start the Gmail bridge")
	}
	if liveAt >= 0 && runAt > liveAt {
		t.Fatal("Gmail monitoring starts after Claude live preflight; mailbox watching must start first")
	}
}
