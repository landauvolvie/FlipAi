package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolvedGeneratedImageReadsConfiguredCodexWorkingDir(t *testing.T) {
	clearCapturedCodexImage()
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("CODEX_HOME", t.TempDir()) // prove the old CODEX_HOME fallback is not what passes

	dataDir, configFile, _, _, err := appPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDataDir(dataDir); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(base, "codex-work")
	generated := filepath.Join(cwd, "generated_images")
	if err := os.MkdirAll(generated, 0700); err != nil {
		t.Fatal(err)
	}
	want := testPNG(t)
	if err := os.WriteFile(filepath.Join(generated, "current-turn.png"), want, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig(dataDir)
	cfg.CodexCwd = cwd
	if err := saveConfig(configFile, cfg); err != nil {
		t.Fatal(err)
	}

	const messageID = "working-dir-image-message"
	deliveredVoiceImages.Delete(messageID)
	t.Cleanup(func() {
		deliveredVoiceImages.Delete(messageID)
		clearCapturedCodexImage()
	})

	img, key := generatedImageForVoiceReplyResolved(GmailMessage{
		ID:           messageID,
		Body:         "Generate me a picture of a person dancing on a ship",
		InternalDate: time.Now().Add(-time.Minute),
	}, "Here's your picture.")
	if img == nil {
		t.Fatal("configured Codex working-folder image was not found")
	}
	if key != messageID {
		t.Fatalf("delivery key = %q, want %q", key, messageID)
	}
	if !bytes.Equal(img.Data, want) {
		t.Fatal("resolved image bytes differ from the generated image")
	}
}

func TestResolvedGeneratedImageDoesNotReuseWorkingFolderForTextOnlyRequest(t *testing.T) {
	clearCapturedCodexImage()
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("CODEX_HOME", t.TempDir())
	dataDir, configFile, _, _, err := appPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDataDir(dataDir); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(base, "codex-work")
	generated := filepath.Join(cwd, "generated_images")
	if err := os.MkdirAll(generated, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generated, "old-picture.png"), testPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig(dataDir)
	cfg.CodexCwd = cwd
	if err := saveConfig(configFile, cfg); err != nil {
		t.Fatal(err)
	}

	const messageID = "text-only-after-image"
	deliveredVoiceImages.Delete(messageID)
	t.Cleanup(func() { deliveredVoiceImages.Delete(messageID) })
	img, _ := generatedImageForVoiceReplyResolved(GmailMessage{
		ID:           messageID,
		Body:         "What time is the meeting tomorrow?",
		InternalDate: time.Now().Add(-time.Minute),
	}, "The meeting is at 10 AM.")
	if img != nil {
		t.Fatal("text-only request incorrectly inherited a recent generated image")
	}
}
