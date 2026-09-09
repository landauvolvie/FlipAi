//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type googleVoiceMediaCaptureRequest struct {
	ID        string    `json:"id"`
	MessageID string    `json:"messageId"`
	Phone     string    `json:"phone"`
	Thread    string    `json:"thread"`
	Marker    string    `json:"marker,omitempty"`
	Created   time.Time `json:"created"`
}

type googleVoiceMediaWireAttachment struct {
	Filename  string `json:"filename,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Data      string `json:"data,omitempty"`
}

type googleVoiceMediaCaptureResult struct {
	OK          bool                             `json:"ok"`
	Error       string                           `json:"error,omitempty"`
	Attachments []googleVoiceMediaWireAttachment `json:"attachments,omitempty"`
}

func googleVoiceMediaQueueDir(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-media-requests")
}

func requestGoogleVoiceInboundMedia(ctx context.Context, dataDir, messageID, phone, thread, marker string) ([]MailAttachment, error) {
	phone = normalizeUSPhone(phone)
	thread = normalizeGoogleVoiceSMSThread(thread)
	if phone == "" || thread == "" {
		return nil, errors.New("Google Voice MMS needs the exact inbound conversation")
	}
	if threadPhone := googleVoiceSMSThreadPhone(thread); threadPhone != "" && threadPhone != phone {
		return nil, errors.New("Google Voice MMS conversation does not match the inbound sender")
	}
	if err := platformEnsureGoogleVoiceSMSWorker(dataDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(googleVoiceMediaQueueDir(dataDir), 0700); err != nil {
		return nil, err
	}
	token, err := secureRandomToken(12)
	if err != nil {
		return nil, err
	}
	req := googleVoiceMediaCaptureRequest{
		ID: token, MessageID: messageID, Phone: phone, Thread: thread,
		Marker: strings.TrimSpace(marker), Created: time.Now(),
	}
	raw, _ := json.Marshal(req)
	requestPath := filepath.Join(googleVoiceMediaQueueDir(dataDir), token+".request.json")
	resultPath := filepath.Join(googleVoiceMediaQueueDir(dataDir), token+".result.json")
	tmp := requestPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, requestPath); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	defer os.Remove(requestPath)
	defer os.Remove(resultPath)

	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(resultPath)
		if err == nil {
			var result googleVoiceMediaCaptureResult
			if json.Unmarshal(raw, &result) != nil {
				return nil, errors.New("Google Voice returned an invalid media result")
			}
			if !result.OK {
				if strings.TrimSpace(result.Error) == "" {
					result.Error = "Google Voice media capture failed"
				}
				return nil, errors.New(result.Error)
			}
			out := make([]MailAttachment, 0, len(result.Attachments))
			for i, item := range result.Attachments {
				mediaType := inboundMediaType(item.MediaType, item.Filename)
				if !supportedInboundMediaType(mediaType) || item.Data == "" {
					continue
				}
				data, err := base64.StdEncoding.DecodeString(item.Data)
				if err != nil || len(data) == 0 {
					continue
				}
				if len(data) > maxInboundAttachmentBytes {
					return nil, fmt.Errorf("Google Voice attachment exceeds FlipAi's %d MB inbound limit", maxInboundAttachmentBytes>>20)
				}
				out = append(out, MailAttachment{
					Filename:  safeAttachmentFilename(item.Filename, i, mediaType),
					MediaType: mediaType,
					Data:      data,
				})
				if len(out) >= maxInboundAttachmentCount {
					break
				}
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}
	return nil, errors.New("Google Voice media capture timed out")
}

func captureGoogleVoiceMediaInPage(dataDir string, d voiceDevTools, req googleVoiceMediaCaptureRequest) googleVoiceMediaCaptureResult {
	if d == nil {
		return googleVoiceMediaCaptureResult{Error: "Google Voice media browser is unavailable"}
	}
	target, err := googleVoiceSMSUIThreadPath(req.Phone, req.Thread, true, googleVoiceSMSAPIAccountSlot(d))
	if err != nil {
		return googleVoiceMediaCaptureResult{Error: err.Error()}
	}
	googleVoiceSMSOutboundPending.Add(1)
	defer googleVoiceSMSOutboundPending.Add(-1)
	deadline := time.Now().Add(20 * time.Second)
	if err := googleVoiceSMSUIOpenConversation(d, req.Phone, target, deadline); err != nil {
		return googleVoiceMediaCaptureResult{Error: err.Error()}
	}
	var result googleVoiceMediaCaptureResult
	if err := voiceEval(d, googleVoiceMediaCaptureExpression(req.Marker), true, &result); err != nil {
		return googleVoiceMediaCaptureResult{Error: err.Error()}
	}
	if !result.OK && strings.TrimSpace(result.Error) == "" {
		result.Error = "Google Voice did not expose the MMS attachment"
	}
	if result.OK {
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.LastEvent = "media-received"
			s.LastNote = fmt.Sprintf("Captured %d Google Voice media attachment(s)", len(result.Attachments))
		})
	}
	return result
}

func runGoogleVoiceSMSMediaCaptureLoop(dataDir string, d voiceDevTools, stop <-chan struct{}) {
	if googleVoiceSMSCallProcess() || d == nil {
		return
	}
	_ = os.MkdirAll(googleVoiceMediaQueueDir(dataDir), 0700)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		entries, err := os.ReadDir(googleVoiceMediaQueueDir(dataDir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".request.json") {
				continue
			}
			path := filepath.Join(googleVoiceMediaQueueDir(dataDir), entry.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var req googleVoiceMediaCaptureRequest
			if json.Unmarshal(raw, &req) != nil || req.ID == "" {
				_ = os.Remove(path)
				continue
			}
			result := googleVoiceMediaCaptureResult{}
			if time.Since(req.Created) > 2*time.Minute {
				result.Error = "Google Voice media request expired"
			} else {
				result = captureGoogleVoiceMediaInPage(dataDir, d, req)
			}
			resultRaw, _ := json.Marshal(result)
			resultPath := filepath.Join(googleVoiceMediaQueueDir(dataDir), req.ID+".result.json")
			tmp := resultPath + ".tmp"
			_ = os.WriteFile(tmp, resultRaw, 0600)
			_ = os.Rename(tmp, resultPath)
			_ = os.Remove(path)
		}
	}
}
