package main

import (
	"os"
	"strings"
	"testing"
)

// This test runs on every platform even though the sender itself is Windows-only.
// It prevents a future UI refactor from quietly bringing back the dangerous
// contact-name/single-suggestion fallbacks that can send a reply to the wrong chat.
func TestGoogleVoiceSMSReplySourceFailsClosed(t *testing.T) {
	b, err := os.ReadFile("google_voice_sms_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`ExactThread bool`,
		`Google Voice reply blocked: exact conversation thread is required`,
		`const exactLink=links.find(x=>pathOf(x)===thread);`,
		`if(!exactLink)return 'exact-thread-not-found';`,
		`if(identityPhone(row)!==phone)return 'thread-phone-mismatch';`,
		`const exact=choices.find(x=>identityPhone(x)===phone);`,
		`if(!exact)return 'exact-recipient-not-found';`,
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
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("unsafe Google Voice SMS recipient fallback returned: %q", forbidden)
		}
	}
}
