package main

import (
    "strings"
    "testing"
)

func TestChatGPTSMSPromptIsPlainAndShort(t *testing.T) {
    cfg := defaultConfig(t.TempDir())
    b := &Bridge{cfg: cfg}
    got := b.composeChatGPTSMSPrompt("Generate me an image of a nice waterfall")
    want := "Generate me an image of a nice waterfall\n\n" + defaultReplyStyleHint
    if got != want {
        t.Fatalf("unexpected ChatGPT SMS prompt:\n%q\nwant:\n%q", got, want)
    }
    if strings.Contains(got, "<sms_command>") || strings.Contains(got, "</sms_command>") {
        t.Fatalf("internal SMS wrapper leaked into ChatGPT: %q", got)
    }
}
