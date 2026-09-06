package main

import (
	"os"
	"strings"
	"testing"
)

func TestGoogleVoiceSMSKeepsBackgroundBrowserWithPageControlledOutbound(t *testing.T) {
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
		"WindowOptions.X = -30000",
	} {
		if !strings.Contains(webview, want) {
			t.Fatalf("Google Voice SMS background browser is missing %q", want)
		}
	}
	for _, want := range []string{
		"googleVoiceSMSUIThreadPath",
		"googleVoiceSMSUIOpenConversation",
		"googleVoiceSMSUISendExpression",
		"InputEvent",
		"b.click()",
	} {
		if !strings.Contains(send, want) {
			t.Fatalf("Google Voice SMS page-controlled sender is missing %q", want)
		}
	}
	for _, forbidden := range []string{"googleVoiceSMSAPITarget", "googleVoiceSMSAPISend", "api2thread/sendsms"} {
		if strings.Contains(send, forbidden) {
			t.Fatalf("Google Voice SMS outbound still constructs the web-service send through %q", forbidden)
		}
	}
}
