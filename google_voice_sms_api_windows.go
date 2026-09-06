//go:build windows

package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	errGoogleVoiceSMSNotAuthenticated = errors.New("Google Voice SMS session is not authenticated")
	googleVoiceSMSAPIMu               sync.Mutex
	googleVoiceSMSHTTPClient          = &http.Client{Timeout: 12 * time.Second}
)

type googleVoiceSMSCDPCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
	Secure bool   `json:"secure"`
}

type googleVoiceSMSAPISession struct {
	CookieHeader string
	SAPISID      string
	AuthUser     string
	APIKey       string
	UserAgent    string
}

func googleVoiceSMSCookieAppliesToAPI(domain string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return false
	}
	const host = "clients6.google.com"
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func googleVoiceSMSCDPCookies(d voiceDevTools) ([]googleVoiceSMSCDPCookie, error) {
	if d == nil {
		return nil, errNoVoiceControlChannel
	}
	var got struct {
		Cookies []googleVoiceSMSCDPCookie `json:"cookies"`
	}
	if err := d.Call("Network.getAllCookies", map[string]any{}, &got); err == nil && len(got.Cookies) > 0 {
		return got.Cookies, nil
	}
	got.Cookies = nil
	if err := d.Call("Storage.getCookies", map[string]any{}, &got); err != nil {
		return nil, err
	}
	return got.Cookies, nil
}

func googleVoiceSMSAPIAccountSlot(d voiceDevTools) string {
	var href string
	if err := voiceEval(d, `String(location.href||'')`, false, &href); err == nil {
		if u, parseErr := url.Parse(href); parseErr == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 && parts[0] == "u" && asciiDigitsOnly(parts[1]) {
				return parts[1]
			}
		}
	}
	return "0"
}

func googleVoiceSMSAPIKeyFromPage(d voiceDevTools) string {
	const expression = `(()=>{try{for(const e of performance.getEntriesByType('resource')){const u=new URL(String(e.name||''));if(u.hostname==='clients6.google.com'&&u.pathname.includes('/voice/v1/voiceclient/')){const k=u.searchParams.get('key');if(k&&/^AIza[0-9A-Za-z_-]{20,}$/.test(k))return k}}}catch(_){}return ''})()`
	var key string
	if err := voiceEval(d, expression, false, &key); err == nil && strings.HasPrefix(key, "AIza") {
		return key
	}
	return googleVoiceSMSFallbackAPIKey
}

func googleVoiceSMSAPIUserAgent(d voiceDevTools) string {
	var got struct {
		UserAgent string `json:"userAgent"`
	}
	if err := d.Call("Browser.getVersion", nil, &got); err == nil {
		return strings.TrimSpace(got.UserAgent)
	}
	return ""
}

func googleVoiceSMSAPISessionFromBrowser(d voiceDevTools) (googleVoiceSMSAPISession, error) {
	cookies, err := googleVoiceSMSCDPCookies(d)
	if err != nil {
		return googleVoiceSMSAPISession{}, fmt.Errorf("read Google Voice browser session: %w", err)
	}
	type chosenCookie struct {
		value string
		score int
	}
	chosen := make(map[string]chosenCookie)
	for _, c := range cookies {
		if !googleVoiceSMSCookieAppliesToAPI(c.Domain) || strings.TrimSpace(c.Name) == "" || c.Value == "" {
			continue
		}
		path := strings.TrimSpace(c.Path)
		if path != "" && !strings.HasPrefix("/voice/v1/voiceclient/", path) && path != "/" {
			continue
		}
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(c.Domain)), ".")
		score := len(domain)*10 + len(path)
		if prev, ok := chosen[c.Name]; !ok || score > prev.score {
			chosen[c.Name] = chosenCookie{value: c.Value, score: score}
		}
	}
	var sapisid string
	for _, name := range []string{"SAPISID", "__Secure-3PAPISID", "__Secure-1PAPISID", "APISID"} {
		if c, ok := chosen[name]; ok && c.value != "" {
			sapisid = c.value
			break
		}
	}
	if sapisid == "" {
		return googleVoiceSMSAPISession{}, errGoogleVoiceSMSNotAuthenticated
	}
	names := make([]string, 0, len(chosen))
	for name := range chosen {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+chosen[name].value)
	}
	return googleVoiceSMSAPISession{
		CookieHeader: strings.Join(parts, "; "),
		SAPISID:      sapisid,
		AuthUser:     googleVoiceSMSAPIAccountSlot(d),
		APIKey:       googleVoiceSMSAPIKeyFromPage(d),
		UserAgent:    googleVoiceSMSAPIUserAgent(d),
	}, nil
}

func googleVoiceSMSSAPISIDHash(sapisid string, now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	sum := sha1.Sum([]byte(ts + " " + sapisid + " https://voice.google.com"))
	return "SAPISIDHASH " + ts + "_" + hex.EncodeToString(sum[:])
}

func googleVoiceSMSAPIRequest(d voiceDevTools, method string, body []byte) ([]byte, error) {
	googleVoiceSMSAPIMu.Lock()
	defer googleVoiceSMSAPIMu.Unlock()

	session, err := googleVoiceSMSAPISessionFromBrowser(d)
	if err != nil {
		return nil, err
	}
	endpoint := googleVoiceSMSAPIBase + "/" + strings.TrimPrefix(method, "/") + "?alt=json&key=" + url.QueryEscape(session.APIKey)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json+protobuf")
	req.Header.Set("Authorization", googleVoiceSMSSAPISIDHash(session.SAPISID, time.Now()))
	req.Header.Set("Cookie", session.CookieHeader)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-JavaScript-User-Agent", "google-api-javascript-client/1.1.0")
	req.Header.Set("X-Origin", "https://voice.google.com")
	req.Header.Set("X-Referer", "https://voice.google.com")
	req.Header.Set("X-Goog-AuthUser", session.AuthUser)
	req.Header.Set("X-Goog-Encode-Response-If-Executable", "base64")
	req.Header.Set("X-Client-Version", "512793257")
	req.Header.Set("Origin", "https://clients6.google.com")
	req.Header.Set("Referer", "https://clients6.google.com/static/proxy.html?usegapi=1")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if session.UserAgent != "" {
		req.Header.Set("User-Agent", session.UserAgent)
	}

	resp, err := googleVoiceSMSHTTPClient.Do(req)
	if err != nil {
		return nil, errors.New("Google Voice web service is unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errGoogleVoiceSMSNotAuthenticated
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Google Voice web service returned HTTP %d", resp.StatusCode)
	}
	const maxResponse = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, errors.New("Google Voice web service response could not be read")
	}
	if len(raw) > maxResponse {
		return nil, errors.New("Google Voice web service response was unexpectedly large")
	}
	return raw, nil
}

func googleVoiceSMSAPIFetchInbox(d voiceDevTools) ([]googleVoiceSMSAPIMessage, error) {
	raw, err := googleVoiceSMSAPIRequest(d, "api2thread/list", googleVoiceSMSAPIListBody())
	if err != nil {
		return nil, err
	}
	return parseGoogleVoiceSMSAPIListResponse(raw, googleVoiceSMSAPIAccountSlot(d))
}

func googleVoiceSMSAPISend(d voiceDevTools, threadID, body string) error {
	payload, err := googleVoiceSMSAPISendBody(threadID, body, time.Now().UnixNano()&0x7fffffffffffffff)
	if err != nil {
		return err
	}
	raw, err := googleVoiceSMSAPIRequest(d, "api2thread/sendsms", payload)
	if err != nil {
		return err
	}
	clean := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), ")]}'"))
	var result struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
		ThreadItemID string          `json:"threadItemId,omitempty"`
		TimestampMs  json.RawMessage `json:"timestampMs,omitempty"`
	}
	if err := json.Unmarshal([]byte(clean), &result); err != nil {
		return errors.New("Google Voice returned an invalid SMS result")
	}
	if result.Error != nil {
		if msg := strings.TrimSpace(result.Error.Message); msg != "" {
			return fmt.Errorf("Google Voice refused the SMS: %s", msg)
		}
		return errors.New("Google Voice refused the SMS")
	}
	if strings.TrimSpace(result.ThreadItemID) == "" && len(result.TimestampMs) == 0 {
		return errors.New("Google Voice did not confirm the SMS send")
	}
	return nil
}

func processGoogleVoiceSMSAPIPoll(dataDir string, messages []googleVoiceSMSAPIMessage) error {
	state := loadGoogleVoiceSMSAPISeenState(dataDir)
	allIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.CursorID != "" {
			allIDs = append(allIDs, message.CursorID)
		}
	}
	if !state.Initialized {
		state.Initialized = true
		rememberGoogleVoiceSMSAPIIDs(&state, allIDs)
		return saveGoogleVoiceSMSAPISeenState(dataDir, state)
	}

	seen := googleVoiceSMSAPISeenSet(state)
	for _, message := range messages {
		if message.CursorID == "" {
			continue
		}
		if _, ok := seen[message.CursorID]; ok {
			continue
		}
		if !message.Outgoing {
			payload, err := json.Marshal(directGoogleVoiceSMS{
				ID:     message.CursorID,
				Sender: message.Sender,
				Thread: message.Thread,
				Body:   message.Body,
				At:     message.At,
			})
			if err != nil {
				return err
			}
			if err := appendDirectGoogleVoiceSMS(dataDir, string(payload)); err != nil {
				// The direct security gate logs blocked/unauthorized identities. Mark
				// this API item seen so a rejected text cannot spam Activity forever.
				rememberGoogleVoiceSMSAPIIDs(&state, []string{message.CursorID})
				_ = saveGoogleVoiceSMSAPISeenState(dataDir, state)
				return err
			}
			mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
				s.LastInboundAt = time.Now()
			})
		}
		rememberGoogleVoiceSMSAPIIDs(&state, []string{message.CursorID})
		seen[message.CursorID] = struct{}{}
	}
	return saveGoogleVoiceSMSAPISeenState(dataDir, state)
}

func runGoogleVoiceSMSAPIInboxLoop(dataDir string, d voiceDevTools, stop <-chan struct{}) {
	if googleVoiceSMSCallProcess() || d == nil {
		return
	}
	poll := func() {
		messages, err := googleVoiceSMSAPIFetchInbox(d)
		if err == nil {
			err = processGoogleVoiceSMSAPIPoll(dataDir, messages)
		}
		now := time.Now()
		if err != nil {
			mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
				s.ListenerRunning = false
				s.Ready = false
				s.LastProbeAt = now
				s.LastEvent = "background-api-error"
				s.LastError = err.Error()
			})
			return
		}
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.Running = true
			s.Starting = false
			s.Connected = true
			s.SignedIn = true
			s.ListenerRunning = true
			s.Ready = true
			s.LastProbeAt = now
			s.LastEvent = "background-api-ready"
			s.LastError = ""
		})
	}

	poll()
	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			poll()
		}
	}
}
