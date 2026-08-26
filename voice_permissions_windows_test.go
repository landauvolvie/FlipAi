//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestGoogleVoiceWebViewAllowsRequiredCallCapabilities(t *testing.T) {
	if !strings.Contains(voiceBrowserArguments, "--disable-popup-blocking") {
		t.Fatal("Google Voice WebView must allow call-related popups")
	}
	for _, protocol := range []string{"tel:", "callto:"} {
		if !strings.Contains(googleVoiceInitScript, protocol) {
			t.Fatalf("Google Voice script must preserve %s dialing links", protocol)
		}
	}
}

func TestWebView2PermissionKindPatchIsPresent(t *testing.T) {
	b, err := os.ReadFile("third_party/go-webview2/pkg/edge/chromium.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "uintptr(unsafe.Pointer(&kind))") {
		t.Fatal("WebView2 permission-kind callback must pass GetPermissionKind an output pointer")
	}
}
