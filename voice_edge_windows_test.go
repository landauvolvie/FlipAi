//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestEdgeGoogleVoiceUsesWebPermissionDescriptorNames(t *testing.T) {
	b, err := os.ReadFile("voice_edge_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"notifications"`, `"push"`, `"microphone"`, `"speaker-selection"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("Edge Google Voice receiver missing permission descriptor %s", want)
		}
	}
	for _, wrong := range []string{`{"notifications", "audioCapture"`, `"speakerSelection"`} {
		if strings.Contains(s, wrong) {
			t.Fatalf("Edge Google Voice receiver still contains old PermissionType name %s", wrong)
		}
	}
}

func TestEdgeReceiverDoesNotStartBrowserHidden(t *testing.T) {
	b, err := os.ReadFile("voice_edge_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "cmd := exec.Command(edgePath, args...)")
	if start < 0 {
		t.Fatal("Edge launch block missing")
	}
	end := strings.Index(s[start:], "if err := cmd.Start()")
	if end < 0 {
		t.Fatal("Edge cmd.Start block missing")
	}
	if strings.Contains(s[start:start+end], "hideWindow(cmd)") {
		t.Fatal("Edge Google Voice app-mode window must not be started with HideWindow")
	}
}

func TestEdgeReceiverLivenessUsesHWNDNotVisibility(t *testing.T) {
	b, err := os.ReadFile("voice_edge_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "func edgeWindowGone")
	if start < 0 {
		t.Fatal("edgeWindowGone missing")
	}
	end := strings.Index(s[start:], "\n}\n")
	if end < 0 {
		t.Fatal("edgeWindowGone end missing")
	}
	fn := s[start : start+end]
	if !strings.Contains(fn, "procEdgeIsWindow.Call(hwnd)") {
		t.Fatal("edgeWindowGone must use IsWindow so minimized/hidden receiver remains alive")
	}
	if strings.Contains(fn, "edgeWindowForPID") {
		t.Fatal("edgeWindowGone must not require a visible Edge window")
	}
}
