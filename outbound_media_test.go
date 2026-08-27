package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{G: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFindImageGenerationResult(t *testing.T) {
	want := base64.StdEncoding.EncodeToString(testPNG(t))
	v := map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type":   "image_generation_call",
			"status": "completed",
			"result": want,
		},
	}
	if got := findImageGenerationResult(v); got != want {
		t.Fatalf("result mismatch: got %q", got)
	}
}

func TestNewestCodexGeneratedImageUsesGeneratedImages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "generated_images", "thread")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data := testPNG(t)
	path := filepath.Join(dir, "ig_test.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	img, err := newestCodexGeneratedImageSince(time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if img.MediaType != "image/png" || !bytes.Equal(img.Data, data) {
		t.Fatalf("unexpected image: type=%q size=%d", img.MediaType, len(img.Data))
	}
}

func TestBuildThreadedReplyMessageWithImage(t *testing.T) {
	img, err := normalizeVoiceImage("generated.png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := replyThreadMeta{
		Subject:   "New text message",
		MessageID: "<original@example.com>",
	}
	raw, err := buildThreadedReplyMessageWithImage("", "15551234567.15557654321.token@txt.voice.google.com", meta, "Done", img)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(msg.Header.Get("Content-Type"), "multipart/mixed") {
		t.Fatalf("expected multipart message, got %q", msg.Header.Get("Content-Type"))
	}
	if !strings.Contains(raw, "Content-Type: image/png") || !strings.Contains(raw, "generated.png") {
		t.Fatalf("image MIME part missing: %s", raw)
	}
}

func TestTransientVoiceReplyNeverCarriesImage(t *testing.T) {
	for _, s := range []string{"✓ Codex working on it…", "Codex still working…", "Queued for Codex (1 ahead)…"} {
		if !isTransientVoiceReply(s) {
			t.Fatalf("expected transient: %q", s)
		}
	}
	if isTransientVoiceReply("Done — image generated.") {
		t.Fatal("final answer classified as transient")
	}
}
