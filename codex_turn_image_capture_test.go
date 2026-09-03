package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func completedTurnWithImage(t *testing.T, kind, result, savedPath string) json.RawMessage {
	t.Helper()
	item := map[string]any{
		"type":   kind,
		"result": result,
	}
	if savedPath != "" {
		item["savedPath"] = savedPath
	}
	raw, err := json.Marshal(map[string]any{
		"threadId": "thread-image-test",
		"turn": map[string]any{
			"id":     "turn-image-test",
			"status": "completed",
			"items": []any{
				map[string]any{"type": "agentMessage", "text": "done"},
				item,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCaptureCodexImageFromTurnCompletedRawBase64(t *testing.T) {
	clearCapturedCodexImage()
	want := testPNG(t)
	params := completedTurnWithImage(t, "imageGeneration", base64.StdEncoding.EncodeToString(want), "")
	if !captureCodexImageFromTurnCompleted(params) {
		t.Fatal("expected turn/completed image to be captured")
	}
	got := takeCapturedCodexImage()
	if got == nil {
		t.Fatal("captured image is nil")
	}
	if got.MediaType != "image/png" || !bytes.Equal(got.Data, want) {
		t.Fatal("captured turn image does not match generated PNG")
	}
}

func TestCaptureCodexImageFromTurnCompletedAcceptsLegacyTypeAndDataURL(t *testing.T) {
	clearCapturedCodexImage()
	want := testPNG(t)
	result := "data:image/png;base64," + base64.StdEncoding.EncodeToString(want)
	params := completedTurnWithImage(t, "image_generation", result, "")
	if !captureCodexImageFromTurnCompleted(params) {
		t.Fatal("expected legacy/data-URL image to be captured")
	}
	got := takeCapturedCodexImage()
	if got == nil || !bytes.Equal(got.Data, want) {
		t.Fatal("legacy/data-URL image bytes were not captured")
	}
}

func TestCaptureCodexImageFromItemCompletedFlexibleAcceptsLegacyDataURL(t *testing.T) {
	clearCapturedCodexImage()
	want := testPNG(t)
	result := "data:image/png;base64," + base64.StdEncoding.EncodeToString(want)
	params, err := json.Marshal(map[string]any{
		"turnId": "turn-image-test",
		"item": map[string]any{
			"type":   "image_generation",
			"result": result,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !captureCodexImageFromItemCompletedFlexible(params) {
		t.Fatal("expected tolerant item/completed parser to capture image")
	}
	got := takeCapturedCodexImage()
	if got == nil || !bytes.Equal(got.Data, want) {
		t.Fatal("tolerant item/completed image bytes were not captured")
	}
}

func TestCodexRouteCapturesImageFromTurnCompletedBeforeDelivery(t *testing.T) {
	clearCapturedCodexImage()
	want := testPNG(t)
	params := completedTurnWithImage(t, "imageGeneration", base64.StdEncoding.EncodeToString(want), "")
	client := NewCodexClient("", "")
	client.route(rpcEnvelope{Method: "turn/completed", Params: params})
	got := takeCapturedCodexImage()
	if got == nil || !bytes.Equal(got.Data, want) {
		t.Fatal("route did not retain the completed turn image for reply delivery")
	}
}

func TestCaptureCodexImageFromTurnCompletedIgnoresTextOnlyTurn(t *testing.T) {
	clearCapturedCodexImage()
	raw, err := json.Marshal(map[string]any{
		"threadId": "thread-test",
		"turn": map[string]any{
			"id":     "turn-test",
			"status": "completed",
			"items": []any{map[string]any{"type": "agentMessage", "text": "hello"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if captureCodexImageFromTurnCompleted(raw) {
		t.Fatal("text-only turn must not capture an image")
	}
	if got := takeCapturedCodexImage(); got != nil {
		t.Fatal("text-only turn left a captured image")
	}
}
