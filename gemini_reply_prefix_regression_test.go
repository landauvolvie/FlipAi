package main

import "testing"

func TestCleanGeminiChatReplyStripsAccessibilitySpeakerLabel(t *testing.T) {
	cases := map[string]string{
		"Gemini said Hey there! How can I help you today?": "Hey there! How can I help you today?",
		"Gemini said: Top news today: U.S. markets rose.": "Top news today: U.S. markets rose.",
		"Gemini said\nHello": "Hello",
		"Top news today: U.S. markets rose.": "Top news today: U.S. markets rose.",
		"Gemini saidsomething else": "Gemini saidsomething else",
	}
	for in, want := range cases {
		if got := cleanGeminiChatReply(in); got != want {
			t.Fatalf("cleanGeminiChatReply(%q) = %q, want %q", in, got, want)
		}
	}
}
