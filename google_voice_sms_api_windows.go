//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	googleVoiceSMSAPIMu      sync.Mutex
	googleVoiceSMSHTTPClient = &http.Client{Timeout: 20 * time.Second}

	// googleVoiceSMSOriginMu guards the origin FlipAi has learned this session.
	// Once the page's own traffic proves which origin Google signs with, every
	// later request uses it instead of rediscovering it.
	googleVoiceSMSOriginMu    sync.Mutex
	googleVoiceSMSKnownOrigin string

	// googleVoiceSMSOutboundPending counts replies in flight. The inbox poll and
	// the reply share one quota with Google, so the poll stands aside while a
	// reply is being delivered.
	googleVoiceSMSOutboundPending atomic.Int64
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
	Cookies      map[string]string
	AuthUser     string
	APIKey       string
	UserAgent    string
	Template     googleVoiceSMSAuthTemplate
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

// googleVoiceSMSCaptureTemplate asks the page for the last request it made to
// the Voice web service. This is the authoritative source for the API key, the
// client version, and -- through its authorization value -- the origin Google
// signs with.
func googleVoiceSMSCaptureTemplate(d voiceDevTools) googleVoiceSMSAuthTemplate {
	var raw string
	if err := voiceEval(d, googleVoiceSMSCaptureTemplateJS, false, &raw); err != nil {
		return googleVoiceSMSAuthTemplate{}
	}
	return parseGoogleVoiceSMSAuthTemplate(raw)
}

// googleVoiceSMSCaptureDrain takes the inbox responses the page received on its
// own since the last drain. These need no authentication work at all: they are
// Google Voice's own answers, observed as they arrived.
func googleVoiceSMSCaptureDrain(d voiceDevTools) []string {
	var raw string
	if err := voiceEval(d, googleVoiceSMSCaptureDrainJS, false, &raw); err != nil {
		return nil
	}
	return parseGoogleVoiceSMSCaptureDrain(raw)
}

func googleVoiceSMSAPIKeyFromPage(d voiceDevTools) string {
	const expression = `(()=>{try{for(const e of performance.getEntriesByType('resource')){const u=new URL(String(e.name||''));if(u.hostname==='clients6.google.com'&&u.pathname.includes('/voice/v1/voiceclient/')){const k=u.searchParams.get('key');if(k&&/^AIza[0-9A-Za-z_-]{20,}$/.test(k))return k}}}catch(_){}return ''})()`
	var key string
	if err := voiceEval(d, expression, false, &key); err == nil && strings.HasPrefix(key, "AIza") {
		return key
	}
	return ""
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
	values := make(map[string]string, len(chosen))
	names := make([]string, 0, len(chosen))
	for name, c := range chosen {
		values[name] = c.value
		names = append(names, name)
	}
	// Every scheme Google offers is signed, so a session that carries only the
	// partitioned cookies still authenticates. Requiring plain SAPISID alone
	// turned such a session into a permanent "not signed in".
	hasSigningCookie := false
	for _, scheme := range googleVoiceSMSAuthSchemes {
		if strings.TrimSpace(values[scheme.Cookie]) != "" {
			hasSigningCookie = true
			break
		}
	}
	if !hasSigningCookie {
		return googleVoiceSMSAPISession{}, errGoogleVoiceSMSNotAuthenticated
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+values[name])
	}

	template := googleVoiceSMSCaptureTemplate(d)
	key := template.Key
	if key == "" {
		key = googleVoiceSMSAPIKeyFromPage(d)
	}
	if key == "" {
		key = googleVoiceSMSFallbackAPIKey
	}
	if origin, ok := googleVoiceSMSOriginFromAuthorization(template.header("authorization"), values); ok {
		googleVoiceSMSRememberOrigin(origin)
	}
	return googleVoiceSMSAPISession{
		CookieHeader: strings.Join(parts, "; "),
		Cookies:      values,
		AuthUser:     googleVoiceSMSAPIAccountSlot(d),
		APIKey:       key,
		UserAgent:    googleVoiceSMSAPIUserAgent(d),
		Template:     template,
	}, nil
}

func googleVoiceSMSRememberOrigin(origin string) {
	googleVoiceSMSOriginMu.Lock()
	defer googleVoiceSMSOriginMu.Unlock()
	googleVoiceSMSKnownOrigin = origin
}

// googleVoiceSMSOriginOrder is which origins to sign with, best first. A proven
// origin is tried alone; without proof both are tried, because sending the
// wrong one is indistinguishable from being signed out.
func googleVoiceSMSOriginOrder() []string {
	googleVoiceSMSOriginMu.Lock()
	known := googleVoiceSMSKnownOrigin
	googleVoiceSMSOriginMu.Unlock()
	if known != "" {
		return []string{known}
	}
	return append([]string(nil), googleVoiceSMSAuthOrigins...)
}

func googleVoiceSMSAPIDefaultHeaders() map[string]string {
	return map[string]string{
		"accept":                               "*/*",
		"content-type":                         "application/json+protobuf",
		"x-requested-with":                     "XMLHttpRequest",
		"x-javascript-user-agent":              "google-api-javascript-client/1.1.0",
		"x-origin":                             "https://voice.google.com",
		"x-referer":                            "https://voice.google.com",
		"x-goog-encode-response-if-executable": "base64",
		"sec-fetch-dest":                       "empty",
		"sec-fetch-mode":                       "cors",
		"sec-fetch-site":                       "same-origin",
	}
}

func googleVoiceSMSAPIRequestOnce(session googleVoiceSMSAPISession, method string, body []byte, origin string) ([]byte, error) {
	endpoint := googleVoiceSMSAPIBase + "/" + strings.TrimPrefix(method, "/") + "?alt=json&key=" + url.QueryEscape(session.APIKey)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	merged := googleVoiceSMSMergedHeaders(googleVoiceSMSAPIDefaultHeaders(), session.Template)
	for i := 0; i+1 < len(merged); i += 2 {
		req.Header.Set(merged[i], merged[i+1])
	}
	authorization := googleVoiceSMSAuthorization(session.Cookies, origin, time.Now())
	if authorization == "" {
		return nil, errGoogleVoiceSMSNotAuthenticated
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Cookie", session.CookieHeader)
	req.Header.Set("X-Goog-AuthUser", session.AuthUser)
	req.Header.Set("Origin", "https://clients6.google.com")
	req.Header.Set("Referer", "https://clients6.google.com/static/proxy.html?usegapi=1")
	if session.UserAgent != "" {
		req.Header.Set("User-Agent", session.UserAgent)
	}

	resp, err := googleVoiceSMSHTTPClient.Do(req)
	if err != nil {
		// The connection may have dropped while the response was being read, in
		// which case Google could already have sent the text. Reading may repeat
		// this; a reply may not.
		return nil, googleVoiceSMSUnknownOutcome(errors.New("Google Voice web service is unreachable"), 0)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errGoogleVoiceSMSNotAuthenticated
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		// Google declined to process this request at all, which is the one
		// answer that makes sending again safe.
		return nil, googleVoiceSMSRefused(
			errors.New("Google Voice web service is asking FlipAi to slow down"),
			parseGoogleVoiceSMSRetryAfter(resp.Header.Get("Retry-After"), time.Now()))
	}
	if resp.StatusCode >= 500 {
		// A server error can be raised on either side of the text actually
		// going out, so its outcome is unknown.
		return nil, googleVoiceSMSUnknownOutcome(
			fmt.Errorf("Google Voice web service returned HTTP %d", resp.StatusCode),
			parseGoogleVoiceSMSRetryAfter(resp.Header.Get("Retry-After"), time.Now()))
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

func googleVoiceSMSAPIRequest(d voiceDevTools, method string, body []byte) ([]byte, error) {
	googleVoiceSMSAPIMu.Lock()
	defer googleVoiceSMSAPIMu.Unlock()

	session, err := googleVoiceSMSAPISessionFromBrowser(d)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, origin := range googleVoiceSMSOriginOrder() {
		raw, err := googleVoiceSMSAPIRequestOnce(session, method, body, origin)
		if err == nil {
			googleVoiceSMSRememberOrigin(origin)
			return raw, nil
		}
		lastErr = err
		if !errors.Is(err, errGoogleVoiceSMSNotAuthenticated) {
			return nil, err
		}
	}
	// A proven origin that has started failing is no longer proven: forget it
	// so the next request rediscovers the right one instead of repeating a
	// request Google now rejects.
	googleVoiceSMSRememberOrigin("")
	return nil, lastErr
}

func googleVoiceSMSAPIFetchInbox(d voiceDevTools) ([]googleVoiceSMSAPIMessage, googleVoiceSMSAPIStats, error) {
	raw, err := googleVoiceSMSAPIRequest(d, "api2thread/list", googleVoiceSMSAPIListBody())
	if err != nil {
		return nil, googleVoiceSMSAPIStats{}, err
	}
	return parseGoogleVoiceSMSAPIListResponseDetailed(raw, googleVoiceSMSAPIAccountSlot(d))
}

// googleVoiceSMSAPISend delivers one reply, asking again when Google answers
// that it is busy rather than that it refuses.
//
// The agent has already spent its turn producing this answer, and the person
// waiting on it has no other way to receive it, so a single 429 must not be the
// end of the attempt. Sending also takes priority over the inbox poll for as
// long as it runs: they share one quota, and a reply that cannot get out is
// worse than an inbox that is read a few seconds later.
func googleVoiceSMSAPISend(d voiceDevTools, threadID, body string, deadline time.Time) error {
	googleVoiceSMSOutboundPending.Add(1)
	defer googleVoiceSMSOutboundPending.Add(-1)

	// One tracking id for the whole reply, not one per attempt. It is the
	// nearest thing this endpoint offers to an idempotency key, so a resend
	// carries the identity of the message it is resending rather than looking
	// like a second, unrelated text.
	nonce := time.Now().UnixNano() & 0x7fffffffffffffff

	var lastErr error
	for attempt := 0; attempt < googleVoiceSMSSendAttempts; attempt++ {
		if attempt > 0 {
			wait := googleVoiceSMSSendBackoff(attempt-1, googleVoiceSMSResendAfterOf(lastErr))
			if !deadline.IsZero() && time.Now().Add(wait).After(deadline) {
				break
			}
			time.Sleep(wait)
		}
		err := googleVoiceSMSAPISendOnce(d, threadID, body, nonce)
		if err == nil {
			return nil
		}
		lastErr = err
		// Only an answer that means "I did nothing with this" may be sent
		// again. Anything else could already be on its way to the phone.
		if _, resendable := googleVoiceSMSResendable(err); !resendable {
			return err
		}
	}
	if lastErr == nil {
		return errors.New("Google Voice did not confirm the SMS send")
	}
	return fmt.Errorf("Google Voice would not accept the reply: %w", lastErr)
}

func googleVoiceSMSResendAfterOf(err error) time.Duration {
	wait, _ := googleVoiceSMSResendable(err)
	return wait
}

func googleVoiceSMSAPISendOnce(d voiceDevTools, threadID, body string, nonce int64) error {
	payload, err := googleVoiceSMSAPISendBody(threadID, body, nonce)
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

// The inbox is read two ways at once, and either one alone is enough.
//
//   - Passively: the page's own calls to the Voice web service are observed,
//     so a text that arrives while Google Voice is refreshing itself is
//     already in hand before FlipAi asks for anything.
//   - Actively: FlipAi asks the same service directly, so a quiet page can
//     never stall delivery.
//
// The poll cadence lives in google_voice_sms_api.go, next to the readiness
// window it has to stay inside.

func googleVoiceSMSDrainCaptured(dataDir string, d voiceDevTools) (int, googleVoiceSMSAPIStats) {
	var total googleVoiceSMSAPIStats
	captured := 0
	slot := ""
	for _, raw := range googleVoiceSMSCaptureDrain(d) {
		if slot == "" {
			slot = googleVoiceSMSAPIAccountSlot(d)
		}
		messages, stats, err := parseGoogleVoiceSMSAPIListResponseDetailed([]byte(raw), slot)
		if err != nil {
			continue
		}
		captured++
		total.Threads += stats.Threads
		total.Items += stats.Items
		total.Undirected += stats.Undirected
		total.UnknownTypes = append(total.UnknownTypes, stats.UnknownTypes...)
		if err := processGoogleVoiceSMSAPIPoll(dataDir, messages); err != nil {
			mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
				s.LastNote = "Observed inbox update was not delivered: " + err.Error()
			})
		}
	}
	return captured, total
}

func runGoogleVoiceSMSAPIInboxLoop(dataDir string, d voiceDevTools, stop <-chan struct{}) {
	if googleVoiceSMSCallProcess() || d == nil {
		return
	}
	interval := googleVoiceSMSPollInterval
	var nextPoll, nextDrain time.Time

	// drain takes whatever the page has already received. It asks Google for
	// nothing, so it keeps running even while a reply holds the quota -- an
	// inbound text that arrives during a retry sequence is still delivered
	// immediately. An update it processed also proves the listener is alive.
	drain := func() (int, googleVoiceSMSAPIStats) {
		captured, stats := googleVoiceSMSDrainCaptured(dataDir, d)
		if captured > 0 {
			now := time.Now()
			note := googleVoiceSMSListenerNote(stats, captured)
			mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
				s.Running, s.Starting = true, false
				s.Connected, s.SignedIn = true, true
				s.ListenerRunning, s.Ready = true, true
				s.LastProbeAt = now
				s.LastEvent = "background-observed"
				s.LastError = ""
				s.LastNote = note
				s.ObservedRows = stats.Threads
				s.ObserverCandidates = stats.Items
			})
		}
		return captured, stats
	}

	poll := func() {
		captured, capturedStats := drain()
		messages, stats, err := googleVoiceSMSAPIFetchInbox(d)
		if err == nil {
			err = processGoogleVoiceSMSAPIPoll(dataDir, messages)
		}
		now := time.Now()
		if err != nil {
			// An observed update still proves the listener is alive and the
			// session is signed in, so a failed active ask does not tear down a
			// connection that is demonstrably working.
			if captured > 0 {
				interval = googleVoiceSMSPollInterval
				note := googleVoiceSMSListenerNote(capturedStats, captured)
				mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
					s.Running, s.Starting = true, false
					s.Connected, s.SignedIn = true, true
					s.ListenerRunning, s.Ready = true, true
					s.LastProbeAt = now
					s.LastEvent = "background-observed-only"
					s.LastError = ""
					s.LastNote = note + "; direct request failed: " + err.Error()
					s.ObservedRows = capturedStats.Threads
					s.ObserverCandidates = capturedStats.Items
				})
				return
			}
			message := err.Error()
			if errors.Is(err, errGoogleVoiceSMSNotAuthenticated) {
				message = "Google Voice did not accept this browser session; open Connections and press Connect under Google Voice SMS to sign in again"
			}
			if interval < googleVoiceSMSPollMaxInterval {
				interval *= 2
				if interval > googleVoiceSMSPollMaxInterval {
					interval = googleVoiceSMSPollMaxInterval
				}
			}
			mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
				s.ListenerRunning = false
				s.Ready = false
				s.LastProbeAt = now
				s.LastEvent = "background-api-error"
				s.LastError = message
			})
			return
		}
		interval = googleVoiceSMSPollInterval
		note := googleVoiceSMSListenerNote(stats, captured)
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
			s.LastNote = note
			s.ObservedRows = stats.Threads
			s.ObserverCandidates = stats.Items
		})
	}

	ticker := time.NewTicker(googleVoiceSMSPollTick)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		// The page owns the sign-in question. Polling before it has loaded and
		// reported would only record errors about a browser that is still
		// starting up.
		if !loadGoogleVoiceSMSRuntime(dataDir).SignedIn {
			nextPoll = time.Time{}
			continue
		}
		// A reply in flight has the quota, so the request to Google waits. What
		// the page has already received does not, because taking it costs
		// Google nothing -- a text arriving mid-retry is still delivered now
		// rather than up to a hundred seconds later.
		if googleVoiceSMSOutboundPending.Load() > 0 {
			if !time.Now().Before(nextDrain) {
				drain()
				nextDrain = time.Now().Add(googleVoiceSMSDrainInterval)
			}
			continue
		}
		if time.Now().Before(nextPoll) {
			continue
		}
		poll()
		now := time.Now()
		nextPoll = now.Add(interval)
		nextDrain = now.Add(googleVoiceSMSDrainInterval)
	}
}
