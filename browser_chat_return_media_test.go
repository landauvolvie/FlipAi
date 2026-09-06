package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func testReturnedPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 180, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSafeBrowserConversationURL(t *testing.T) {
	good := []string{
		"https://chatgpt.com/c/abc",
		"https://claude.ai/chat/abc",
		"https://gemini.google.com/app/abc",
		"https://grok.com/c/abc",
		"https://copilot.microsoft.com/chats/abc",
	}
	for _, raw := range good {
		if got := safeBrowserConversationURL(raw); got == "" {
			t.Fatalf("expected provider URL to be accepted: %s", raw)
		}
	}
	for _, raw := range []string{"http://chatgpt.com/c/x", "https://example.com/x", "javascript:alert(1)"} {
		if got := safeBrowserConversationURL(raw); got != "" {
			t.Fatalf("unsafe fallback URL accepted: %s -> %s", raw, got)
		}
	}
}

func TestPrepareBrowserReturnedImageUsesActualImage(t *testing.T) {
	body := "Here is the image."
	media := &browserChatReturnedMedia{
		Kind:            "image",
		Filename:        "answer.png",
		Data:            testReturnedPNG(t),
		ConversationURL: "https://chatgpt.com/c/abc",
	}
	text, image := prepareBrowserChatReturnedMediaReply(body, media)
	if image == nil || len(image.Data) == 0 {
		t.Fatal("expected actual image bytes to be prepared for Google Voice MMS")
	}
	if text != body {
		t.Fatalf("image MMS should keep caption unchanged, got %q", text)
	}
	if image.MediaType != "image/png" {
		t.Fatalf("expected PNG, got %q", image.MediaType)
	}
}

func TestPrepareBrowserReturnedFileFallsBackToConversation(t *testing.T) {
	media := &browserChatReturnedMedia{
		Kind:            "video",
		ConversationURL: "https://copilot.microsoft.com/chats/abc",
	}
	text, image := prepareBrowserChatReturnedMediaReply("Video ready.", media)
	if image != nil {
		t.Fatal("video must not be treated as Google Voice image MMS")
	}
	if !strings.Contains(text, "https://copilot.microsoft.com/chats/abc") {
		t.Fatalf("expected conversation fallback link, got %q", text)
	}
}

func TestCapturedBrowserMediaIsCopiedAndConsumedOnce(t *testing.T) {
	clearCapturedBrowserChatReturnedMedia()
	data := []byte{1, 2, 3}
	storeCapturedBrowserChatReturnedMedia(&browserChatReturnedMedia{
		Kind:            "image",
		Data:            data,
		ConversationURL: "https://claude.ai/chat/abc",
	})
	data[0] = 9
	got := takeCapturedBrowserChatReturnedMedia()
	if got == nil || len(got.Data) != 3 || got.Data[0] != 1 {
		t.Fatalf("captured media was not deep-copied: %#v", got)
	}
	got.Data[1] = 9
	if again := takeCapturedBrowserChatReturnedMedia(); again != nil {
		t.Fatalf("media must be consumed once, got %#v", again)
	}
}
