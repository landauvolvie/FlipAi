package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func imageGenerationCompletedParams(t *testing.T, data []byte, status string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"turnId": "turn-image-1",
		"item": map[string]any{
			"type":          "imageGeneration",
			"id":            "ig-1",
			"status":        status,
			"result":        base64.StdEncoding.EncodeToString(data),
			"savedPath":     nil,
			"revisedPrompt": "test",
			"failure":       nil,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCaptureCodexImageFromItemCompletedUsesResultEvenWhenStatusIsGenerating(t *testing.T) {
	clearCapturedCodexImage()
	want := testPNG(t)
	if ok := captureCodexImageFromItemCompleted(imageGenerationCompletedParams(t, want, "generating")); !ok {
		t.Fatal("imageGeneration item was not captured")
	}
	img := takeCapturedCodexImage()
	if img == nil {
		t.Fatal("captured image was missing")
	}
	if img.MediaType != "image/png" {
		t.Fatalf("media type = %q, want image/png", img.MediaType)
	}
	if !bytes.Equal(img.Data, want) {
		t.Fatal("captured image bytes differ from Codex result")
	}
}

func TestCodexRouteCapturesImageGenerationItem(t *testing.T) {
	clearCapturedCodexImage()
	want := testPNG(t)
	c := NewCodexClient("", "")
	c.route(rpcEnvelope{Method: "item/completed", Params: imageGenerationCompletedParams(t, want, "completed")})
	img := takeCapturedCodexImage()
	if img == nil || !bytes.Equal(img.Data, want) {
		t.Fatal("Codex route did not preserve the imageGeneration result")
	}
}

func TestGeneratedVoiceImagePrefersCapturedCodexEventWithoutDiskFile(t *testing.T) {
	clearCapturedCodexImage()
	t.Setenv("CODEX_HOME", t.TempDir())
	const messageID = "event-image-message"
	deliveredVoiceImages.Delete(messageID)
	t.Cleanup(func() {
		deliveredVoiceImages.Delete(messageID)
		clearCapturedCodexImage()
	})

	want := testPNG(t)
	if ok := captureCodexImageFromItemCompleted(imageGenerationCompletedParams(t, want, "generating")); !ok {
		t.Fatal("imageGeneration item was not captured")
	}
	img, key := generatedImageForVoiceReply(GmailMessage{
		ID:           messageID,
		InternalDate: time.Now(),
	}, "Done — image generated.")
	if img == nil {
		t.Fatal("generatedImageForVoiceReply ignored the captured app-server image")
	}
	if key != messageID {
		t.Fatalf("delivery key = %q, want %q", key, messageID)
	}
	if !bytes.Equal(img.Data, want) {
		t.Fatal("voice image bytes differ from the app-server event")
	}
}

func TestStartingNewCodexTurnClearsPreviousCapturedImage(t *testing.T) {
	clearCapturedCodexImage()
	want := testPNG(t)
	if ok := captureCodexImageFromItemCompleted(imageGenerationCompletedParams(t, want, "completed")); !ok {
		t.Fatal("imageGeneration item was not captured")
	}

	// Request performs the clear before it attempts to send turn/start. Leaving
	// stdin nil intentionally makes the request fail immediately after that
	// invariant, without needing to launch a real Codex process in this unit test.
	c := NewCodexClient("", "")
	_, _ = c.Request(context.Background(), "turn/start", map[string]any{"threadId": "thread-1"})
	if img := takeCapturedCodexImage(); img != nil {
		t.Fatal("a previous turn's image survived the next turn/start")
	}
}
