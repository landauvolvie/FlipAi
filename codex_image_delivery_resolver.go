package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// generatedImageForVoiceReplyResolved is the production resolver used by the
// Google Voice MMS path. The App Server event remains the best source because it
// belongs to the exact turn. Current Codex image-generation builds also persist
// the completed image under <turn cwd>/generated_images, not under CODEX_HOME.
// v0.30/v0.31 only searched CODEX_HOME, so a machine whose Codex build did not
// surface the v2 imageGeneration item could complete the turn and send its text
// while FlipAi never discovered that an image existed.
func generatedImageForVoiceReplyResolved(original GmailMessage, body string) (*outboundVoiceImage, string) {
	if isTransientVoiceReply(body) || strings.TrimSpace(original.ID) == "" {
		return nil, ""
	}
	key := original.ID
	if _, sent := deliveredVoiceImages.Load(key); sent {
		return nil, ""
	}

	// Exact-turn App Server capture is still first and consumes the media slot.
	if img := takeCapturedCodexImage(); img != nil {
		return img, key
	}

	since := original.InternalDate
	if since.IsZero() {
		since = time.Now().Add(-5 * time.Minute)
	}
	since = since.Add(-15 * time.Second)

	// Modern Codex saves generated images relative to the configured turn cwd.
	// Restrict this filesystem fallback to requests that actually ask for image
	// creation, so a queued text-only SMS can never inherit a recent picture from
	// the previous turn.
	if looksLikeImageGenerationRequest(original.Body) {
		if img, err := newestConfiguredCodexWorkingDirImageSince(since); err == nil && img != nil {
			return img, key
		}
	}

	// Keep the older CODEX_HOME/session scan for compatibility with Codex builds
	// that used those locations. It is also intent-gated for the same stale-image
	// protection as the current working-folder fallback.
	if looksLikeImageGenerationRequest(original.Body) {
		if img, err := newestCodexGeneratedImageSince(since); err == nil && img != nil {
			return img, key
		}
	}
	return nil, ""
}

func looksLikeImageGenerationRequest(body string) bool {
	s := strings.ToLower(extractGoogleVoiceCommand(body))
	if strings.TrimSpace(s) == "" {
		s = strings.ToLower(body)
	}
	imageWord := strings.Contains(s, "image") ||
		strings.Contains(s, "picture") ||
		strings.Contains(s, "photo") ||
		strings.Contains(s, "illustration") ||
		strings.Contains(s, "drawing") ||
		strings.Contains(s, "artwork") ||
		strings.Contains(s, "graphic")
	createWord := strings.Contains(s, "generate") ||
		strings.Contains(s, "create") ||
		strings.Contains(s, "make") ||
		strings.Contains(s, "draw") ||
		strings.Contains(s, "render") ||
		strings.Contains(s, "paint") ||
		strings.Contains(s, "design")
	return imageWord && createWord
}

func newestConfiguredCodexWorkingDirImageSince(since time.Time) (*outboundVoiceImage, error) {
	dataDir, configFile, _, _, err := appPaths()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(configFile, dataDir)
	if err != nil {
		return nil, err
	}
	cwd := strings.TrimSpace(cfg.codexWorkingDir())
	if cwd == "" {
		return nil, errors.New("Codex working directory is unavailable")
	}
	return newestVoiceImageInDirectory(filepath.Join(cwd, "generated_images"), since)
}

func newestVoiceImageInDirectory(root string, since time.Time) (*outboundVoiceImage, error) {
	imageExt := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}
	for _, f := range recentFiles(root, since, imageExt) {
		data, err := os.ReadFile(f.path)
		if err != nil || len(data) == 0 {
			continue
		}
		if img, err := normalizeVoiceImage(filepath.Base(f.path), data); err == nil {
			return img, nil
		}
	}
	return nil, errors.New("no recent generated image found in Codex working directory")
}
