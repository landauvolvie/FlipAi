package main

import (
	"os"
	"strings"
	"testing"
)

// This test runs on every platform even though the sender itself is Windows-only.
// It prevents a future UI refactor from quietly bringing back contact-name,
// ambiguous-recipient, or wrong-thread fallbacks that can send a reply to the
// wrong Google Voice conversation.
func TestGoogleVoiceSMSReplySourceFailsClosed(t *testing.T) {
	b, err := os.ReadFile("google_voice_sms_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`ExactThread bool`,
		`Google Voice reply blocked: exact conversation thread is required`,
		`const wanted=threadInfo('https://voice.google.com'+thread);`,
		`if(!wanted.thread||wanted.thread!==thread)return 'invalid-exact-thread';`,
		`if(wanted.phone&&wanted.phone!==phone)return 'thread-phone-mismatch';`,
		`if(!exact)return 'exact-thread-not-found';`,
		`if(after.phone&&after.phone!==phone)return 'thread-phone-mismatch';`,
		`const exact=choices.find(x=>identity(x).phone===phone||digits(label(x))===phone);`,
		`if(!exact)return 'exact-recipient-not-found';`,
		`a[href*="/messages"]`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Google Voice SMS reply safety check is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`choices.length===1`,
		`digits(label(x)).includes(phone)`,
		`press('Enter')`,
		`press("Enter")`,
		`a[href*="/messages/"]`,
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("unsafe or obsolete Google Voice SMS recipient fallback returned: %q", forbidden)
		}
	}
}
