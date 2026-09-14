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

// Copilot renders an action row under each reply -- "Edit in a page" -- and it
// was arriving on the end of the text message. It is furniture for the person
// looking at the browser, not part of what the model said.
func TestActionRowIsStrippedFromBrowserReplies(t *testing.T) {
	cases := map[string]string{
		"Short answer: I'm not a company. Edit in a page": "Short answer: I'm not a company.",
		"the answer\nCopy":                      "the answer",
		"the answer | Copy":                     "the answer",
		"the answer Copy":                       "the answer Copy", // an ordinary word, not furniture
		"the answer Good response Bad response": "the answer",
		"the answer · Edit in a page":           "the answer",
		"the answer\nRead aloud":                "the answer",

		// Not furniture: the answer itself ends this way.
		"you can copy":           "you can copy",
		"press Share to send it": "press Share to send it",
		"the answer":             "the answer",
	}
	for in, want := range cases {
		if got := stripBrowserChatReplyChrome(in); got != want {
			t.Errorf("stripBrowserChatReplyChrome(%q) = %q, want %q", in, got, want)
		}
	}
}

// An action row is never allowed to consume the whole reply.
func TestActionRowAloneIsKept(t *testing.T) {
	for _, in := range []string{"Copy", "Edit in a page", "Share"} {
		if got := stripBrowserChatReplyChrome(in); got != in {
			t.Errorf("stripBrowserChatReplyChrome(%q) = %q, want it kept", in, got)
		}
	}
}

// The two cleanups compose: label in front, action row behind.
func TestReplyCleanupRemovesBothLabelAndActionRow(t *testing.T) {
	got := cleanBrowserChatReply("Copilot said Short answer: I'm not a company. Edit in a page")
	want := "Short answer: I'm not a company."
	if got != want {
		t.Fatalf("cleanBrowserChatReply = %q, want %q", got, want)
	}
}
