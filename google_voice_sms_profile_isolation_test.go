package main

import (
	"os"
	"strings"
	"testing"
)

func TestGoogleVoiceSMSUsesIndependentBrowserProfile(t *testing.T) {
	b, err := os.ReadFile("google_voice_sms_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`--google-voice-sms-login`,
		`--google-voice-sms-worker`,
		`DataPath:  googleVoiceSMSProfilePath(dataDir)`,
		`flipGoogleVoiceSMSStatus`,
		`googleVoiceSMSPageMonitorJS`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("dedicated SMS browser is missing %q", want)
		}
	}
	if strings.Contains(s, `DataPath:  voiceProfilePath(dataDir)`) {
		t.Fatal("direct SMS must never reuse the Google Voice calling WebView profile")
	}
}

func TestGoogleVoiceSMSConnectDoesNotRestartCalling(t *testing.T) {
	b, err := os.ReadFile("google_voice_sms_control.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, forbidden := range []string{"loadVoiceRuntime", "platformOpenGoogleVoice", "platformRestartGoogleVoice"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("SMS connection is coupled to calling again through %q", forbidden)
		}
	}
	for _, want := range []string{"platformStartGoogleVoiceSMSLogin", "platformDisconnectGoogleVoiceSMS"} {
		if !strings.Contains(s, want) {
			t.Fatalf("SMS connection is missing %q", want)
		}
	}
}

func TestGoogleVoiceSMSReplyDoesNotOpenCallBrowser(t *testing.T) {
	b, err := os.ReadFile("google_voice_sms_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "platformOpenGoogleVoice") || strings.Contains(s, "loadVoiceRuntime") {
		t.Fatal("direct SMS reply path still opens or depends on the Google Voice calling browser")
	}
	if !strings.Contains(s, "platformEnsureGoogleVoiceSMSWorker") {
		t.Fatal("direct SMS reply path does not ensure its dedicated SMS browser")
	}
}

func TestRetiredSharedProfileObserverIsGone(t *testing.T) {
	if _, err := os.Stat("google_voice_sms_observer_windows.go"); err == nil {
		t.Fatal("the retired shared-profile SMS observer was added back")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
