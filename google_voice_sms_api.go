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
		ID   string                      `json:"id"`
		Item []googleVoiceSMSAPIListItem `json:"item"`
	} `json:"thread"`
}

type googleVoiceSMSAPIListItem struct {
	ID          string          `json:"id"`
	StartTime   json.RawMessage `json:"startTime"`
	DID         string          `json:"did"`
	Status      string          `json:"status"`
	MessageText string          `json:"messageText"`
	Type        json.RawMessage `json:"type"`
	MessageID   string          `json:"messageId"`

	// Google has shipped more than one encoding for an item's direction over
	// the life of this endpoint. These are the alternatives that have been
	// seen alongside "type"; whichever one is present is used.
	Outgoing   *bool  `json:"outgoing,omitempty"`
	IsOutgoing *bool  `json:"isOutgoing,omitempty"`
	Direction  string `json:"direction,omitempty"`
}

// googleVoiceSMSAPIStats is what one inbox response looked like, so a listener
// that is authenticated but returning nothing usable can say which of the two
// it is instead of silently doing nothing.
type googleVoiceSMSAPIStats struct {
	Threads      int
	Items        int
	Undirected   int
	UnknownTypes []string
}

// googleVoiceSMSAPIDirectionWord normalizes the many spellings Google has used
// for a direction ("smsIn", "SMS_IN", "sms-in", "INCOMING") to one token.
func googleVoiceSMSAPIDirectionWord(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// googleVoiceSMSAPIItemDirection decides whether an item is a text FlipAi
// received. It answers "unknown" rather than guessing: an outgoing message
// mistaken for an incoming one would make FlipAi answer its own reply forever,
// so an item whose direction cannot be established is skipped and reported.
func googleVoiceSMSAPIItemDirection(item googleVoiceSMSAPIListItem) (inbound bool, outgoing bool, known bool) {
	switch {
	case item.Outgoing != nil:
		return !*item.Outgoing, *item.Outgoing, true
	case item.IsOutgoing != nil:
		return !*item.IsOutgoing, *item.IsOutgoing, true
	}
	for _, candidate := range []string{googleVoiceSMSAPITypeWord(item.Type), item.Direction} {
		switch googleVoiceSMSAPIDirectionWord(candidate) {
		case "smsin", "in", "incoming", "inbound", "smsinbound", "received", "receivedsms":
			return true, false, true
		case "smsout", "out", "outgoing", "outbound", "smsoutbound", "sent", "sentsms":
			return false, true, true
		}
	}
	return false, false, false
}

// googleVoiceSMSAPITypeWord reads the "type" field whether Google encoded it as
// a JSON string or a bare number. A number carries no direction FlipAi can
// safely interpret, so it is returned verbatim for the diagnostic instead.
func googleVoiceSMSAPITypeWord(raw json.RawMessage) string {
	v := strings.TrimSpace(string(raw))
	if v == "" || v == "null" {
		return ""
	}
	if strings.HasPrefix(v, `"`) {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	return v
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
	msgs, _, err := parseGoogleVoiceSMSAPIListResponseDetailed(raw, accountSlot)
	return msgs, err
}

func parseGoogleVoiceSMSAPIListResponseDetailed(raw []byte, accountSlot string) ([]googleVoiceSMSAPIMessage, googleVoiceSMSAPIStats, error) {
	var stats googleVoiceSMSAPIStats
	accountSlot = strings.TrimSpace(accountSlot)
	if accountSlot == "" || !asciiDigitsOnly(accountSlot) {
		accountSlot = "0"
	}
	raw = []byte(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), ")]}'")))
	var decoded googleVoiceSMSAPIListResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, stats, errors.New("Google Voice web service returned invalid inbox data")
	}
	if decoded.Error != nil {
		msg := strings.TrimSpace(decoded.Error.Message)
		if msg == "" {
			msg = "request failed"
		}
		return nil, stats, fmt.Errorf("Google Voice web service: %s", msg)
	}
	stats.Threads = len(decoded.Thread)
	unknown := map[string]struct{}{}
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
			stats.Items++
			kind := googleVoiceSMSAPITypeWord(item.Type)
			_, outgoing, known := googleVoiceSMSAPIItemDirection(item)
			if !known {
				// Fail closed: an item FlipAi cannot place is never treated as
				// something to answer. Record the encoding so the listener can
				// say exactly what it did not recognize.
				stats.Undirected++
				if label := strings.TrimSpace(kind); label != "" {
					unknown[label] = struct{}{}
				}
				continue
			}
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
	for label := range unknown {
		stats.UnknownTypes = append(stats.UnknownTypes, label)
	}
	sort.Strings(stats.UnknownTypes)
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
	return out, stats, nil
}

// googleVoiceSMSListenerNote turns what one inbox check saw into something a
// person can act on. A listener that is authenticated but delivering nothing is
// the hardest state to debug from the outside, so it says which of the possible
// reasons applies rather than reporting a bare "ready".
func googleVoiceSMSListenerNote(stats googleVoiceSMSAPIStats, captured int) string {
	var parts []string
	if captured > 0 {
		parts = append(parts, fmt.Sprintf("%d inbox update(s) observed from the page", captured))
	}
	parts = append(parts, fmt.Sprintf("%d conversation(s), %d message(s)", stats.Threads, stats.Items))
	if stats.Undirected > 0 {
		label := strings.Join(stats.UnknownTypes, ", ")
		if label == "" {
			label = "unlabelled"
		}
		parts = append(parts, fmt.Sprintf("%d message(s) skipped: unrecognized conversation item type (%s)", stats.Undirected, label))
	}
	if stats.Threads == 0 {
		parts = append(parts, "this Google account's Voice inbox is empty or belongs to a different account")
	}
	return strings.Join(parts, "; ")
}

func googleVoiceSMSAPIStatePath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-api-state.json")
}

// A text FlipAi sent must never come back as a text FlipAi answers. Direction
// is read from the conversation item first, but an SMS loop is expensive and
// self-sustaining, so a second, independent guard remembers what was just sent
// and refuses to treat it as inbound no matter how it is labelled.
const (
	googleVoiceSMSSentMemory = 30 * time.Minute
	googleVoiceSMSSentLimit  = 200
)

type googleVoiceSMSSentEntry struct {
	Fingerprint string    `json:"fingerprint"`
	At          time.Time `json:"at"`
}

type googleVoiceSMSSentLedger struct {
	Sent []googleVoiceSMSSentEntry `json:"sent,omitempty"`
}

func googleVoiceSMSSentPath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-sent.json")
}

func googleVoiceSMSSentFingerprint(phone, body string) string {
	phone = normalizeUSPhone(phone)
	body = strings.Join(strings.Fields(body), " ")
	if phone == "" || body == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(phone + "\x00" + body))
	return hex.EncodeToString(sum[:16])
}

func loadGoogleVoiceSMSSentLedger(dataDir string) googleVoiceSMSSentLedger {
	var ledger googleVoiceSMSSentLedger
	if raw, err := os.ReadFile(googleVoiceSMSSentPath(dataDir)); err == nil {
		_ = json.Unmarshal(raw, &ledger)
	}
	cutoff := time.Now().Add(-googleVoiceSMSSentMemory)
	kept := ledger.Sent[:0]
	for _, entry := range ledger.Sent {
		if entry.Fingerprint != "" && entry.At.After(cutoff) {
			kept = append(kept, entry)
		}
	}
	ledger.Sent = kept
	return ledger
}

func rememberGoogleVoiceSMSSent(dataDir, phone, body string) {
	fingerprint := googleVoiceSMSSentFingerprint(phone, body)
	if fingerprint == "" {
		return
	}
	ledger := loadGoogleVoiceSMSSentLedger(dataDir)
	ledger.Sent = append(ledger.Sent, googleVoiceSMSSentEntry{Fingerprint: fingerprint, At: time.Now()})
	if len(ledger.Sent) > googleVoiceSMSSentLimit {
		ledger.Sent = append([]googleVoiceSMSSentEntry(nil), ledger.Sent[len(ledger.Sent)-googleVoiceSMSSentLimit:]...)
	}
	raw, err := json.Marshal(ledger)
	if err != nil || os.MkdirAll(dataDir, 0700) != nil {
		return
	}
	path := googleVoiceSMSSentPath(dataDir)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0600) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
	}
}

func googleVoiceSMSWasSentRecently(dataDir, phone, body string) bool {
	fingerprint := googleVoiceSMSSentFingerprint(phone, body)
	if fingerprint == "" {
		return false
	}
	for _, entry := range loadGoogleVoiceSMSSentLedger(dataDir).Sent {
		if entry.Fingerprint == fingerprint {
			return true
		}
	}
	return false
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
