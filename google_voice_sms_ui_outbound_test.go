package main

import (
	"os"
	"strings"
	"testing"
)

func googleVoiceSMSFunctionSource(t *testing.T, file, function string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func "+function+"(")
	if start < 0 {
		t.Fatalf("%s is missing from %s", function, file)
	}
	body := source[start:]
	if end := strings.Index(body[len("func "):], "\nfunc "); end >= 0 {
		body = body[:len("func ")+end]
	}
	return body
}

func TestGoogleVoiceSMSOutboundUsesTheRealPageComposer(t *testing.T) {
	body := googleVoiceSMSFunctionSource(t, "google_voice_sms_windows.go", "sendGoogleVoiceTextInPage")
	for _, want := range []string{
		"googleVoiceSMSUIThreadPath(",
		"googleVoiceSMSUIOpenConversation(",
		"googleVoiceSMSUISendExpression(",
		"voiceEval(",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Google Voice outbound no longer uses %q", want)
		}
	}
	if strings.Contains(body, "googleVoiceSMSAPISend(") || strings.Contains(body, "api2thread/sendsms") {
		t.Fatal("Google Voice outbound fell back to constructing the sendsms web-service request")
	}
}

func TestGoogleVoiceSMSPageSendUsesComposerEventsAndSendControl(t *testing.T) {
	raw, err := os.ReadFile("google_voice_sms_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, want := range []string{
		"type a message",
		"InputEvent",
		"Send button",
		"b.click()",
		"__FLIPAI_GV_UI_SEND__",
	} {
		if !strings.Contains(strings.ToLower(source), strings.ToLower(want)) {
			t.Fatalf("Google Voice page-control send is missing %q", want)
		}
	}
}

func TestGoogleVoiceSMSUISendGetsAConfirmationDeadline(t *testing.T) {
	raw, err := os.ReadFile("voice_cdp_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if !strings.Contains(source, "googleVoiceSMSUITurnDevToolsTimeout") ||
		!strings.Contains(source, "strings.Contains(expression, googleVoiceSMSUITurnMarker)") {
		t.Fatal("the UI send still has the ordinary short DevTools probe deadline")
	}
}
