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
		"googleVoiceSMSUIThreadPath",
		"googleVoiceSMSThreadPhone",
		"conversation phone does not match recipient",
		"normalizeGoogleVoiceSMSThread",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("direct Google Voice SMS exact-thread safety is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"googleVoiceSMSAPITarget",
		"googleVoiceSMSAPISend",
		"api2thread/sendsms",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("Google Voice SMS exact reply still bypasses the page through %q", forbidden)
		}
	}
}
