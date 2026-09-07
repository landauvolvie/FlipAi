package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBrowserChatPromptRequestsGeneratedImage(t *testing.T) {
	for _, prompt := range []string{
		"Generate me a picture of flying birds",
		"generate a picture of a green car",
		"Create an image of the skyline",
		"make me a photo of a red house",
		"draw me a picture of a cat",
	} {
		if !browserChatPromptRequestsGeneratedImage(prompt) {
			t.Fatalf("expected generated-image intent for %q", prompt)
		}
	}
	for _, prompt := range []string{
		"What do you see in this image?",
		"Summarize this photo",
		"Tell me about image compression",
	} {
		if browserChatPromptRequestsGeneratedImage(prompt) {
			t.Fatalf("did not expect generated-image intent for %q", prompt)
		}
	}
}

func TestBrowserChatReplySuggestsPendingImage(t *testing.T) {
	for _, reply := range []string{"Image", "Creating your image", "Generating the image..."} {
		if !browserChatReplySuggestsPendingImage(reply) {
			t.Fatalf("expected pending image reply for %q", reply)
		}
	}
	if browserChatReplySuggestsPendingImage("Sorry, I can't generate images in this chat.") {
		t.Fatal("a real provider refusal must not be treated as a still-rendering image")
	}
}

func TestFinishBrowserGeneratedImageTurnRecoversNinetySecondFailure(t *testing.T) {
	clearCapturedBrowserChatReturnedMedia()
	defer clearCapturedBrowserChatReturnedMedia()

	go func() {
		time.Sleep(25 * time.Millisecond)
		storeCapturedBrowserChatReturnedMedia(&browserChatReturnedMedia{Kind: "image", Data: []byte{1, 2, 3}})
	}()

	reply, err := finishBrowserGeneratedImageTurn(
		context.Background(),
		"Generate me a picture of flying birds",
		"",
		errors.New("ChatGPT did not produce an assistant response within 90 seconds"),
	)
	if err != nil {
		t.Fatalf("image turn should recover after media arrives: %v", err)
	}
	if reply != "Image created." {
		t.Fatalf("recovered reply = %q, want Image created.", reply)
	}
	if !hasCapturedBrowserChatReturnedMedia() {
		t.Fatal("wait helper consumed the media before delivery")
	}
}
