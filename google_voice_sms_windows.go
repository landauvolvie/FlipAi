//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type googleVoiceSMSOutboundRequest struct {
	ID          string    `json:"id"`
	Phone       string    `json:"phone"`
	Thread      string    `json:"thread,omitempty"`
	ExactThread bool      `json:"exactThread,omitempty"`
	Body        string    `json:"body"`
	Created     time.Time `json:"created"`
}

type googleVoiceSMSOutboundResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func googleVoiceSMSOutboxDir(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-outbox")
}

func requestGoogleVoiceText(ctx context.Context, dataDir, phone, body string) error {
	return requestGoogleVoiceTextTarget(ctx, dataDir, phone, "", body, false)
}

func requestGoogleVoiceTextThread(ctx context.Context, dataDir, phone, thread, body string) error {
	thread = normalizeGoogleVoiceSMSThread(thread)
	if thread == "" {
		return errors.New("Google Voice reply blocked: exact conversation thread is required")
	}
	return requestGoogleVoiceTextTarget(ctx, dataDir, phone, thread, body, true)
}

func requestGoogleVoiceTextTarget(ctx context.Context, dataDir, phone, thread, body string, exactThread bool) error {
	phone = normalizeUSPhone(phone)
	body = strings.TrimSpace(body)
	if exactThread {
		thread = normalizeGoogleVoiceSMSThread(thread)
		if thread == "" {
			return errors.New("Google Voice reply blocked: exact conversation thread is required")
		}
	} else {
		thread = ""
	}
	if phone == "" || body == "" {
		return errors.New("Google Voice SMS needs a recipient and text")
	}
	if err := platformEnsureGoogleVoiceSMSWorker(dataDir); err != nil {
		return err
	}
	readyDeadline := time.Now().Add(15 * time.Second)
	for {
		s := loadGoogleVoiceSMSRuntime(dataDir)
		if googleVoiceSMSConnected(s) {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(readyDeadline) {
			if s.LastError != "" {
				return errors.New(s.LastError)
			}
			return errors.New("Google Voice SMS background connection is not ready; reconnect it under Connections")
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err := os.MkdirAll(googleVoiceSMSOutboxDir(dataDir), 0700); err != nil {
		return err
	}
	id, err := secureRandomToken(12)
	if err != nil {
		return err
	}
	req := googleVoiceSMSOutboundRequest{ID: id, Phone: phone, Thread: thread, ExactThread: exactThread, Body: body, Created: time.Now()}
	raw, _ := json.Marshal(req)
	requestPath := filepath.Join(googleVoiceSMSOutboxDir(dataDir), id+".request.json")
	resultPath := filepath.Join(googleVoiceSMSOutboxDir(dataDir), id+".result.json")
	tmp := requestPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, requestPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	defer os.Remove(requestPath)
	defer os.Remove(resultPath)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if raw, err := os.ReadFile(resultPath); err == nil {
			var result googleVoiceSMSOutboundResult
			if json.Unmarshal(raw, &result) != nil {
				return errors.New("Google Voice returned an invalid SMS result")
			}
			if result.OK {
				return nil
			}
			if result.Error == "" {
				result.Error = "Google Voice refused the SMS"
			}
			return errors.New(result.Error)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}
}

func runGoogleVoiceSMSOutboundLoop(dataDir string, d voiceDevTools, stop <-chan struct{}) {
	if googleVoiceSMSCallProcess() || d == nil {
		return
	}
	_ = os.MkdirAll(googleVoiceSMSOutboxDir(dataDir), 0700)
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			entries, err := os.ReadDir(googleVoiceSMSOutboxDir(dataDir))
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".request.json") {
					continue
				}
				path := filepath.Join(googleVoiceSMSOutboxDir(dataDir), entry.Name())
				raw, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				var req googleVoiceSMSOutboundRequest
				if json.Unmarshal(raw, &req) != nil || req.ID == "" {
					_ = os.Remove(path)
					continue
				}
				result := googleVoiceSMSOutboundResult{OK: true}
				switch {
				case time.Since(req.Created) > 5*time.Minute:
					result.OK = false
					result.Error = "Google Voice SMS request expired before the background connection could send it"
				case req.ExactThread && normalizeGoogleVoiceSMSThread(req.Thread) == "":
					result.OK = false
					result.Error = "Google Voice reply blocked: exact conversation thread is invalid"
				default:
					// The loop guard has to exist before Google does. Both ways
					// FlipAi learns about a text -- its own inbox poll, and the
					// updates the signed-in page receives by itself -- can see
					// this one the instant Google accepts it, which is already
					// too late for bookkeeping that waits for the send to
					// return. Recording it first closes that window.
					//
					// A fingerprint left behind by a failed send only suppresses
					// an identical inbound text for half an hour. Removing it
					// would reopen the window for a text Google may in fact have
					// delivered, and an answered reply is a paid SMS loop, so
					// the harmless failure is the one to prefer.
					rememberGoogleVoiceSMSSent(dataDir, req.Phone, req.Body)
					if err := sendGoogleVoiceTextInPage(d, req.Phone, req.Thread, req.Body, req.ExactThread, req.Created.Add(googleVoiceSMSOutboundBudget)); err != nil {
						result.OK = false
						result.Error = err.Error()
					} else {
						mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) { s.LastOutboundAt = time.Now() })
					}
				}
				resultRaw, _ := json.Marshal(result)
				resultPath := filepath.Join(googleVoiceSMSOutboxDir(dataDir), req.ID+".result.json")
				tmp := resultPath + ".tmp"
				_ = os.WriteFile(tmp, resultRaw, 0600)
				_ = os.Rename(tmp, resultPath)
				_ = os.Remove(path)
			}
		}
	}
}

// Historical name retained for the outbox call site. This function no longer
// edits the Google Voice page: the browser contributes only its authenticated
// session, and the exact Voice web-service thread is used for delivery.
func sendGoogleVoiceTextInPage(d voiceDevTools, phone, thread, body string, exactThread bool, deadline time.Time) error {
	phone = normalizeUSPhone(phone)
	body = strings.TrimSpace(body)
	if phone == "" || body == "" {
		return errors.New("Google Voice SMS needs a recipient and text")
	}
	threadID, err := googleVoiceSMSAPITarget(phone, thread, exactThread)
	if err != nil {
		return err
	}
	return googleVoiceSMSAPISend(d, threadID, body, deadline)
}
