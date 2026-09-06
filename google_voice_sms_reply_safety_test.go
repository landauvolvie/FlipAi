package main

import (
	"os"
	"strings"
	"testing"
)

func TestGoogleVoiceSMSReplyRequiresExactBackgroundThread(t *testing.T) {
	b, err := os.ReadFile("google_voice_sms_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"ExactThread bool",
		"exact conversation thread is required",
		"googleVoiceSMSAPITarget",
		"googleVoiceSMSAPISend",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("direct Google Voice SMS exact-thread safety is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"voiceSendTextJS", ".click()", "querySelector", "aria-label",
		"Send new message", "recipient-input", "message-input", "send-button",
	} {
		if strings.Contains(strings.ToLower(s), strings.ToLower(forbidden)) {
			t.Fatalf("direct Google Voice SMS reply path still uses UI automation %q", forbidden)
		}
	}
}
