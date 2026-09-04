//go:build windows

package main

import "testing"

func TestGoogleVoiceMessagesURLFromPage(t *testing.T) {
	tests := []struct {
		page string
		want string
	}{
		{"https://voice.google.com/u/0/calls", "https://voice.google.com/u/0/messages"},
		{"https://voice.google.com/u/3/voicemail", "https://voice.google.com/u/3/messages"},
		{"https://voice.google.com/", "https://voice.google.com/u/0/messages"},
		{"https://accounts.google.com/signin", "https://voice.google.com/u/0/messages"},
	}
	for _, tc := range tests {
		if got := googleVoiceMessagesURLFromPage(tc.page); got != tc.want {
			t.Fatalf("googleVoiceMessagesURLFromPage(%q) = %q, want %q", tc.page, got, tc.want)
		}
	}
}
