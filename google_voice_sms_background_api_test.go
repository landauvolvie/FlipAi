package main

import (
	"os"
	"strings"
	"testing"
)

func TestGoogleVoiceSMSIsBackgroundAPIOnlyAfterSignIn(t *testing.T) {
	webviewRaw, err := os.ReadFile("google_voice_sms_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	sendRaw, err := os.ReadFile("google_voice_sms_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	webview := string(webviewRaw)
	send := string(sendRaw)

	for _, want := range []string{
		"runGoogleVoiceSMSAPIInboxLoop",
		"runGoogleVoiceSMSOutboundLoop",
		"googleVoiceSMSPageMonitorJS",
		"DataPath:  googleVoiceSMSProfilePath(dataDir)",
	} {
		if !strings.Contains(webview, want) {
			t.Fatalf("Google Voice SMS background browser is missing %q", want)
		}
	}
	for _, want := range []string{"googleVoiceSMSAPITarget", "googleVoiceSMSAPISend"} {
		if !strings.Contains(send, want) {
			t.Fatalf("Google Voice SMS background sender is missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"googleVoiceSMSInitScript",
		`Bind("flipVoiceSMS"`,
		"voiceSendTextJS",
		".click()",
		"send new message",
		"recipient-input",
		"message-input",
		"send-button",
	} {
		if strings.Contains(webview, forbidden) || strings.Contains(strings.ToLower(send), strings.ToLower(forbidden)) {
			t.Fatalf("direct Google Voice SMS still contains UI conversation automation %q", forbidden)
		}
	}
}
