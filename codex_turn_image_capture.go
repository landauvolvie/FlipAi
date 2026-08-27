package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// captureCodexImageFromTurnCompleted is the strongest no-token fallback for
// generated images. Codex App Server's turn/completed notification contains the
// final turn items. If an imageGeneration item/completed notification was missed
// or not emitted by a particular Codex build, the completed turn still carries
// the durable imageGeneration item with its exact base64 result and optional
// savedPath.
func captureCodexImageFromTurnCompleted(params json.RawMessage) bool {
	var p struct {
		Turn struct {
			Items []json.RawMessage `json:"items"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &p) != nil {
		return false
	}

	for i := len(p.Turn.Items) - 1; i >= 0; i-- {
		var item struct {
			Type      string `json:"type"`
			Result    string `json:"result"`
			SavedPath string `json:"savedPath"`
		}
		if json.Unmarshal(p.Turn.Items[i], &item) != nil || item.Type != "imageGeneration" {
			continue
		}

		var img *outboundVoiceImage
		if encoded := strings.TrimSpace(item.Result); encoded != "" {
			if comma := strings.IndexByte(encoded, ','); strings.HasPrefix(strings.ToLower(encoded), "data:image/") && comma >= 0 {
				encoded = encoded[comma+1:]
			}
			if data, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(data) > 0 {
				name := "flipai-generated.png"
				if strings.TrimSpace(item.SavedPath) != "" {
					name = filepath.Base(item.SavedPath)
				}
				if normalized, err := normalizeVoiceImage(name, data); err == nil {
					img = normalized
				}
			}
		}
		if img == nil && strings.TrimSpace(item.SavedPath) != "" {
			if data, err := os.ReadFile(item.SavedPath); err == nil && len(data) > 0 {
				if normalized, err := normalizeVoiceImage(filepath.Base(item.SavedPath), data); err == nil {
					img = normalized
				}
			}
		}
		if img == nil {
			continue
		}

		copyImage := &outboundVoiceImage{
			Filename:  img.Filename,
			MediaType: img.MediaType,
			Data:      append([]byte(nil), img.Data...),
		}
		capturedCodexImageMu.Lock()
		capturedCodexImage = copyImage
		capturedCodexImageMu.Unlock()
		return true
	}
	return false
}
