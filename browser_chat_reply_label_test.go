package main

import "testing"

// Copilot's page puts "Copilot said" in the same block as the answer, and it
// was going out to the phone in front of every reply. A text message has one
// speaker -- the agent the sender addressed -- so the label is noise.
func TestSpeakerLabelIsStrippedFromBrowserReplies(t *testing.T) {
	cases := map[string]string{
		"Copilot said I'm Copilot, an AI companion created by Microsoft": "I'm Copilot, an AI companion created by Microsoft",
		"Copilot said: hello there":                                      "hello there",
		"Microsoft Copilot said — hello there":                           "hello there",
		"ChatGPT said hello":                                             "hello",
		"Gemini: hello":                                                  "hello",
		"  Muse said   hello  ":                                          "hello",

		// Not labels: the answer itself begins this way.
		"Copilot is a Microsoft product":    "Copilot is a Microsoft product",
		"Claude Code runs in your terminal": "Claude Code runs in your terminal",
		"Gemini models are made by Google":  "Gemini models are made by Google",
		"hello there":                       "hello there",
		"":                                  "",
	}
	for in, want := range cases {
		if got := stripBrowserChatReplyLabel(in); got != want {
			t.Errorf("stripBrowserChatReplyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// A label with nothing after it is not a label, and must not erase the reply.
func TestSpeakerLabelAloneIsKept(t *testing.T) {
	for _, in := range []string{"Copilot said", "Gemini:", "Muse"} {
		if got := stripBrowserChatReplyLabel(in); got != in {
			t.Errorf("stripBrowserChatReplyLabel(%q) = %q, want it kept", in, got)
		}
	}
}
