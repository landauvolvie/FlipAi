package main

import "testing"

func TestChatGPTSMSPromptHasNoDefaultInstruction(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	b := &Bridge{cfg: cfg}
	const command = "Generate me an image of a nice waterfall"
	if got := b.composeChatGPTSMSPrompt(command); got != command {
		t.Fatalf("default ChatGPT SMS prompt = %q, want exact user command %q", got, command)
	}
}

func TestChatGPTSMSPromptAppendsUserInstructionWhenConfigured(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.GoogleVoice.ReplyStyleHint = "Answer in Yiddish."
	b := &Bridge{cfg: cfg}
	const command = "Write a greeting"
	const want = "Write a greeting\n\nAnswer in Yiddish."
	if got := b.composeChatGPTSMSPrompt(command); got != want {
		t.Fatalf("custom ChatGPT SMS prompt = %q, want %q", got, want)
	}
}
