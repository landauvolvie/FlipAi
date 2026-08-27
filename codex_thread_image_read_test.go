package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestImageFromCodexThreadReadUsesNewestImageGenerationItem(t *testing.T) {
	want := testPNG(t)
	oldEncoded := base64.StdEncoding.EncodeToString([]byte("not-an-image"))
	newEncoded := base64.StdEncoding.EncodeToString(want)

	raw, err := json.Marshal(map[string]any{
		"thread": map[string]any{
			"turns": []any{
				map[string]any{"items": []any{
					map[string]any{"type": "imageGeneration", "result": oldEncoded},
				}},
				map[string]any{"items": []any{
					map[string]any{"type": "agentMessage", "text": "done"},
					map[string]any{"type": "imageGeneration", "result": newEncoded},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	img, err := imageFromCodexThreadRead(raw)
	if err != nil {
		t.Fatalf("imageFromCodexThreadRead: %v", err)
	}
	if img == nil {
		t.Fatal("expected generated image")
	}
	if img.MediaType != "image/png" {
		t.Fatalf("media type = %q, want image/png", img.MediaType)
	}
	if !bytes.Equal(img.Data, want) {
		t.Fatal("thread/read image bytes differ from newest imageGeneration item")
	}
}

func TestImageFromCodexThreadReadAcceptsDataURL(t *testing.T) {
	want := testPNG(t)
	encoded := "data:image/png;base64," + base64.StdEncoding.EncodeToString(want)
	raw, err := json.Marshal(map[string]any{
		"thread": map[string]any{
			"turns": []any{map[string]any{"items": []any{
				map[string]any{"type": "imageGeneration", "result": encoded},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := imageFromCodexThreadRead(raw)
	if err != nil {
		t.Fatalf("imageFromCodexThreadRead: %v", err)
	}
	if img == nil || !bytes.Equal(img.Data, want) {
		t.Fatal("data URL image was not decoded")
	}
}

func TestImageFromCodexThreadReadRejectsThreadWithoutImage(t *testing.T) {
	raw := json.RawMessage(`{"thread":{"turns":[{"items":[{"type":"agentMessage","text":"done"}]}]}}`)
	if img, err := imageFromCodexThreadRead(raw); err == nil || img != nil {
		t.Fatalf("got image=%v err=%v, want no image and an error", img, err)
	}
}
