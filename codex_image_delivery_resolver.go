package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// generatedImageForVoiceReplyResolved is the production resolver used by the
// Google Voice MMS path. The live App Server item event remains the cheapest
// source because it belongs to the exact turn. If that notification is not
// surfaced by the installed Codex build, FlipAi reads the already-completed
// imageGeneration item back from the persisted Codex thread before trying any
// filesystem heuristic. That mirrors how Codex Desktop reconstructs the same
// conversation and avoids depending on where a particular Codex build saved
// the image file.
func generatedImageForVoiceReplyResolved(original GmailMessage, body string) (*outboundVoiceImage, string) {
	if isTransientVoiceReply(body) || strings.TrimSpace(original.ID) == "" {
		return nil, ""
	}
	key := original.ID
	if _, sent := deliveredVoiceImages.Load(key); sent {
		return nil, ""
	}

	// Exact-turn live App Server capture is still first and consumes the media
	// slot without starting any second process or reading disk.
	if img := takeCapturedCodexImage(); img != nil {
		log.Printf("Codex generated image resolved from live item/completed event")
		return img, key
	}

	imageRequest := looksLikeImageGenerationRequest(original.Body)
	if imageRequest {
		// Codex persists imageGeneration items in thread history, including the
		// base64 result and optional savedPath. Read that canonical object back
		// through App Server instead of guessing the image's save directory.
		if img, err := newestPersistedCodexThreadImage(); err == nil && img != nil {
			log.Printf("Codex generated image resolved from persisted thread history")
			return img, key
		} else if err != nil {
			log.Printf("Codex generated image thread-history lookup failed: %v", err)
		}
	}

	since := original.InternalDate
	if since.IsZero() {
		since = time.Now().Add(-5 * time.Minute)
	}
	since = since.Add(-15 * time.Second)

	// Filesystem locations are compatibility fallbacks only. Codex can choose a
	// separate image save root, so neither location is authoritative.
	if imageRequest {
		if img, err := newestConfiguredCodexWorkingDirImageSince(since); err == nil && img != nil {
			log.Printf("Codex generated image resolved from working-directory fallback")
			return img, key
		} else if err != nil {
			log.Printf("Codex generated image working-directory fallback failed: %v", err)
		}
		if img, err := newestCodexGeneratedImageSince(since); err == nil && img != nil {
			log.Printf("Codex generated image resolved from legacy CODEX_HOME fallback")
			return img, key
		} else if err != nil {
			log.Printf("Codex generated image CODEX_HOME fallback failed: %v", err)
		}
		log.Printf("Codex image request completed but FlipAi could not resolve an image asset")
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

// newestPersistedCodexThreadImage reads the same durable thread FlipAi is
// already using. It starts a short-lived local App Server reader only after a
// live image event was missed. thread/read is a local history read: it does not
// send a prompt, invoke the model, or consume model/image-generation tokens.
func newestPersistedCodexThreadImage() (*outboundVoiceImage, error) {
	dataDir, configFile, stateFile, _, err := appPaths()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(configFile, dataDir)
	if err != nil {
		return nil, err
	}
	state := loadState(stateFile)
	threadID := strings.TrimSpace(state.CodexThreadID)
	if threadID == "" {
		return nil, errors.New("Codex thread id is unavailable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reader := NewCodexClient(cfg.CodexPath, cfg.codexWorkingDir())
	if err := reader.Start(ctx); err != nil {
		return nil, fmt.Errorf("start Codex thread reader: %w", err)
	}
	defer reader.Close()

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		raw, err := reader.Request(ctx, "thread/read", map[string]any{
			"threadId":     threadID,
			"includeTurns": true,
		})
		if err == nil {
			if img, parseErr := imageFromCodexThreadRead(raw); parseErr == nil {
				return img, nil
			} else {
				lastErr = parseErr
			}
		} else {
			lastErr = err
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("Codex thread contained no generated image")
	}
	return nil, lastErr
}

func imageFromCodexThreadRead(raw json.RawMessage) (*outboundVoiceImage, error) {
	var response struct {
		Thread struct {
			Turns []struct {
				Items []json.RawMessage `json:"items"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("parse Codex thread/read: %w", err)
	}

	// Walk newest-to-oldest so the image returned belongs to the just-finished
	// image request rather than an earlier image in the same long-lived thread.
	for ti := len(response.Thread.Turns) - 1; ti >= 0; ti-- {
		items := response.Thread.Turns[ti].Items
		for ii := len(items) - 1; ii >= 0; ii-- {
			var item struct {
				Type      string `json:"type"`
				Result    string `json:"result"`
				SavedPath string `json:"savedPath"`
			}
			if json.Unmarshal(items[ii], &item) != nil || item.Type != "imageGeneration" {
				continue
			}
			if encoded := strings.TrimSpace(item.Result); encoded != "" {
				// Be tolerant of a data URL even though current Codex v2 stores raw
				// base64 in result.
				if comma := strings.IndexByte(encoded, ','); strings.HasPrefix(strings.ToLower(encoded), "data:image/") && comma >= 0 {
					encoded = encoded[comma+1:]
				}
				if data, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(data) > 0 {
					name := "flipai-generated.png"
					if strings.TrimSpace(item.SavedPath) != "" {
						name = filepath.Base(item.SavedPath)
					}
					if img, err := normalizeVoiceImage(name, data); err == nil {
						return img, nil
					}
				}
			}
			if path := strings.TrimSpace(item.SavedPath); path != "" {
				if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
					if img, err := normalizeVoiceImage(filepath.Base(path), data); err == nil {
						return img, nil
					}
				}
			}
		}
	}
	return nil, errors.New("Codex thread/read contained no imageGeneration result")
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
