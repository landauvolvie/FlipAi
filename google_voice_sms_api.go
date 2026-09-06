package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	googleVoiceSMSAPIBase        = "https://clients6.google.com/voice/v1/voiceclient"
	googleVoiceSMSFallbackAPIKey = "AIzaSyDTYc1N4xiODyrQYK0Kl6g_y279LjYkrBg"
	googleVoiceSMSAPISeenLimit   = 800
)

type googleVoiceSMSAPIMessage struct {
	CursorID string
	Sender   string
	Thread   string
	Body     string
	At       time.Time
	Outgoing bool
}

type googleVoiceSMSAPISeenState struct {
	Initialized bool     `json:"initialized"`
	Seen        []string `json:"seen,omitempty"`
}

type googleVoiceSMSAPIListResponse struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Thread []struct {
		ID   string `json:"id"`
		Item []struct {
			ID          string          `json:"id"`
			StartTime   json.RawMessage `json:"startTime"`
			DID         string          `json:"did"`
			Status      string          `json:"status"`
			MessageText string          `json:"messageText"`
			Type        string          `json:"type"`
			MessageID   string          `json:"messageId"`
		} `json:"item"`
	} `json:"thread"`
}

func googleVoiceSMSAPIListBody() []byte {
	return []byte(`[2,20,30,"",null,[null,true,true]]`)
}

func googleVoiceSMSAPISendBody(threadID, body string, nonce int64) ([]byte, error) {
	threadID = strings.TrimSpace(threadID)
	body = strings.TrimSpace(body)
	if googleVoiceSMSItemPhone(threadID) == "" {
		return nil, errors.New("Google Voice SMS needs an exact phone thread")
	}
	if body == "" {
		return nil, errors.New("Google Voice SMS needs text")
	}
	return json.Marshal([]any{nil, nil, nil, nil, body, threadID, []any{}, nil, []any{nonce}})
}

func googleVoiceSMSAPITarget(phone, thread string, exactThread bool) (string, error) {
	phone = normalizeUSPhone(phone)
	if phone == "" {
		return "", errors.New("Google Voice SMS needs a valid recipient")
	}
	if !exactThread {
		return "t.+1" + phone, nil
	}
	thread = normalizeGoogleVoiceSMSThread(thread)
	if thread == "" {
		return "", errors.New("Google Voice reply blocked: exact conversation thread is required")
	}
	u, err := url.Parse(thread)
	if err != nil {
		return "", errors.New("Google Voice reply blocked: exact conversation thread is invalid")
	}
	itemID := u.Query().Get("itemId")
	threadPhone := googleVoiceSMSItemPhone(itemID)
	if threadPhone == "" {
		return "", errors.New("Google Voice reply blocked: exact API thread identity is unavailable")
	}
	if threadPhone != phone {
		return "", errors.New("Google Voice reply blocked: conversation phone does not match recipient")
	}
	return itemID, nil
}

func parseGoogleVoiceSMSAPIStartTime(raw json.RawMessage) time.Time {
	v := strings.TrimSpace(string(raw))
	if v == "" || v == "null" {
		return time.Time{}
	}
	if strings.HasPrefix(v, `"`) {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return time.Time{}
		}
		v = strings.TrimSpace(s)
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	if n < 100000000000 {
		return time.Unix(n, 0)
	}
	return time.UnixMilli(n)
}

func googleVoiceSMSAPICursorID(threadID, itemID, messageID, kind, body string, at time.Time) string {
	stable := strings.TrimSpace(itemID)
	if stable == "" {
		stable = strings.TrimSpace(messageID)
	}
	if stable == "" {
		stable = threadID + "\x00" + kind + "\x00" + body + "\x00" + at.UTC().Format(time.RFC3339Nano)
	}
	sum := sha256.Sum256([]byte(stable))
	return "api-" + hex.EncodeToString(sum[:12])
}

func parseGoogleVoiceSMSAPIListResponse(raw []byte, accountSlot string) ([]googleVoiceSMSAPIMessage, error) {
	accountSlot = strings.TrimSpace(accountSlot)
	if accountSlot == "" || !asciiDigitsOnly(accountSlot) {
		accountSlot = "0"
	}
	raw = []byte(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), ")]}'")))
	var decoded googleVoiceSMSAPIListResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, errors.New("Google Voice web service returned invalid inbox data")
	}
	if decoded.Error != nil {
		msg := strings.TrimSpace(decoded.Error.Message)
		if msg == "" {
			msg = "request failed"
		}
		return nil, fmt.Errorf("Google Voice web service: %s", msg)
	}
	out := make([]googleVoiceSMSAPIMessage, 0, 64)
	for _, thread := range decoded.Thread {
		threadID := strings.TrimSpace(thread.ID)
		threadPhone := googleVoiceSMSItemPhone(threadID)
		threadLocator := ""
		if threadPhone != "" {
			threadLocator = "/u/" + accountSlot + "/messages?itemId=" + url.QueryEscape(threadID)
		}
		for _, item := range thread.Item {
			body := strings.TrimSpace(item.MessageText)
			if body == "" {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(item.Type))
			if kind != "smsin" && kind != "smsout" && kind != "sms" {
				continue
			}
			outgoing := kind == "smsout"
			sender := normalizeUSPhone(item.DID)
			at := parseGoogleVoiceSMSAPIStartTime(item.StartTime)
			out = append(out, googleVoiceSMSAPIMessage{
				CursorID: googleVoiceSMSAPICursorID(threadID, item.ID, item.MessageID, kind, body, at),
				Sender:   sender,
				Thread:   threadLocator,
				Body:     body,
				At:       at,
				Outgoing: outgoing,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].CursorID < out[j].CursorID
		}
		if out[i].At.IsZero() {
			return false
		}
		if out[j].At.IsZero() {
			return true
		}
		return out[i].At.Before(out[j].At)
	})
	return out, nil
}

func googleVoiceSMSAPIStatePath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-api-state.json")
}

func loadGoogleVoiceSMSAPISeenState(dataDir string) googleVoiceSMSAPISeenState {
	var state googleVoiceSMSAPISeenState
	if raw, err := os.ReadFile(googleVoiceSMSAPIStatePath(dataDir)); err == nil {
		_ = json.Unmarshal(raw, &state)
	}
	return state
}

func saveGoogleVoiceSMSAPISeenState(dataDir string, state googleVoiceSMSAPISeenState) error {
	if len(state.Seen) > googleVoiceSMSAPISeenLimit {
		state.Seen = append([]string(nil), state.Seen[len(state.Seen)-googleVoiceSMSAPISeenLimit:]...)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	path := googleVoiceSMSAPIStatePath(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func googleVoiceSMSAPISeenSet(state googleVoiceSMSAPISeenState) map[string]struct{} {
	out := make(map[string]struct{}, len(state.Seen))
	for _, id := range state.Seen {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func rememberGoogleVoiceSMSAPIIDs(state *googleVoiceSMSAPISeenState, ids []string) {
	if state == nil {
		return
	}
	seen := googleVoiceSMSAPISeenSet(*state)
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		state.Seen = append(state.Seen, id)
	}
	if len(state.Seen) > googleVoiceSMSAPISeenLimit {
		state.Seen = append([]string(nil), state.Seen[len(state.Seen)-googleVoiceSMSAPISeenLimit:]...)
	}
}
