package main

import "testing"

// Muse texted the whole conversation: weeks of older messages, the prompt, and
// then the answer, all as one message. Everything up to and including the
// quoted prompt is history.
func TestStripBrowserChatConversationKeepsOnlyTheAnswer(t *testing.T) {
	prompt := "what is the weather in new york today"
	for _, tc := range []struct {
		name  string
		reply string
		want  string
	}{
		{
			name:  "the whole thread",
			reply: "An older answer from last week about train times. " + prompt + " It is 72 and sunny in New York today.",
			want:  "It is 72 and sunny in New York today.",
		},
		{
			name:  "the answer repeats the question",
			reply: prompt + " — you asked " + prompt + ", and the answer is 72 and sunny.",
			want:  "and the answer is 72 and sunny.",
		},
		{
			name:  "no quoted prompt at all",
			reply: "It is 72 and sunny in New York today.",
			want:  "It is 72 and sunny in New York today.",
		},
		{
			name:  "nothing after the prompt",
			reply: "Some earlier context. " + prompt,
			want:  "Some earlier context. " + prompt,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripBrowserChatConversation(tc.reply, prompt); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// A short prompt occurs inside ordinary prose, so it is never a boundary.
func TestStripBrowserChatConversationIgnoresAShortPrompt(t *testing.T) {
	reply := "Yes, that is ok with me and here is the rest of the answer."
	if got := stripBrowserChatConversation(reply, "ok"); got != reply {
		t.Fatalf("a two-letter prompt was treated as a conversation boundary: %q", got)
	}
}

func TestCleanBrowserChatReplyForPromptStillStripsLabelAndChrome(t *testing.T) {
	prompt := "summarize the article for me please"
	reply := "Copilot said: Older thread text. " + prompt + " The article argues three things.\nCopy"
	want := "The article argues three things."
	if got := cleanBrowserChatReplyForPrompt(reply, prompt); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
