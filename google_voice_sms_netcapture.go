package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FlipAi's Google Voice SMS browser is signed in, but Go still has to speak to
// the same Voice web service the page speaks to. Three parts of that request
// are not guessable and go stale: the API key the page was served, the client
// version it declares, and -- the one that silently costs a 401 -- which origin
// Google's own code puts inside the SAPISIDHASH authorization value.
//
// So the page tells FlipAi instead of FlipAi guessing. A small script installed
// before any page script wraps fetch and XMLHttpRequest, and records two things
// about the page's own calls to the Voice web service:
//
//   - the request template: the URL (which carries the key), the origin, and
//     the request headers Google's client actually sent, including one real
//     Authorization value. The origin baked into that value is recovered by
//     re-computing the hash for each candidate origin and seeing which one
//     matches, so FlipAi can then mint fresh authorizations that Google
//     accepts for as long as the session lasts.
//   - the inbox responses themselves. When Google Voice refreshes its own
//     conversation list, FlipAi already has the answer and does not need to
//     ask for it.
//
// Nothing is typed, clicked, or selected in the page, and no conversation is
// opened: the script only observes traffic the page was making anyway.

// googleVoiceSMSCaptureDrainBudget bounds how much captured inbox JSON is
// carried back through one DevTools evaluation.
const googleVoiceSMSCaptureDrainBudget = 512 * 1024

// googleVoiceSMSNetworkCaptureJS is installed on every navigation of the SMS
// browser, before the page's own scripts run, so it wraps the original fetch
// and XMLHttpRequest rather than a copy the app already took.
const googleVoiceSMSNetworkCaptureJS = `
(() => {
  if (globalThis.__flipAiGVNet) return;
  const MAX_BODIES = 6, MAX_BODY_BYTES = 400000;
  const API_HOST = 'clients6.google.com';
  const API_PATH = '/voice/v1/voiceclient/';
  const WANTED = ['authorization','x-goog-authuser','x-client-version','x-goog-api-key','content-type','x-javascript-user-agent','x-origin','x-referer','x-requested-with','x-goog-encode-response-if-executable'];
  const store = { template: null, bodies: [], seenRequests: 0, seenResponses: 0 };
  globalThis.__flipAiGVNet = store;

  const parse = (raw) => { try { return new URL(String(raw || ''), location.href); } catch (_) { return null; } };
  const isAPI = (raw) => { const u = parse(raw); return !!u && u.hostname === API_HOST && u.pathname.indexOf(API_PATH) === 0; };
  const methodOf = (raw) => { const u = parse(raw); return u ? u.pathname.slice(u.pathname.indexOf(API_PATH) + API_PATH.length) : ''; };
  const keyOf = (raw) => { const u = parse(raw); return u ? (u.searchParams.get('key') || '') : ''; };
  const isList = (raw) => methodOf(raw).indexOf('api2thread/list') === 0;

  const isSend = (raw) => methodOf(raw).indexOf('api2thread/sendsms') === 0;
  const noteRequest = (raw, headers, body) => {
    try {
      store.seenRequests++;
      // A real send is the one request FlipAi cannot derive from first
      // principles, so its exact body is kept when Google Voice makes one.
      if (isSend(raw) && typeof body === 'string' && body.length > 0 && body.length < 200000) {
        store.send = { at: Date.now(), url: String(raw), key: keyOf(raw), body: body, headers: headers || {} };
      }
      if (!headers || !headers['authorization']) return;
      store.template = { at: Date.now(), url: String(raw), key: keyOf(raw), origin: String(location.origin || ''), headers: headers };
    } catch (_) {}
  };
  const noteResponse = (raw, text) => {
    try {
      store.seenResponses++;
      if (!isList(raw) || typeof text !== 'string') return;
      if (text.length === 0 || text.length > MAX_BODY_BYTES) return;
      store.bodies.push({ at: Date.now(), text: text });
      while (store.bodies.length > MAX_BODIES) store.bodies.shift();
    } catch (_) {}
  };
  const collect = (source) => {
    const out = {};
    try {
      if (!source) return out;
      if (typeof source.forEach === 'function' && typeof source.get === 'function') {
        source.forEach((v, k) => { const n = String(k || '').toLowerCase(); if (WANTED.indexOf(n) >= 0) out[n] = String(v); });
        return out;
      }
      for (const k of Object.keys(source)) {
        const n = String(k || '').toLowerCase();
        if (WANTED.indexOf(n) >= 0) out[n] = String(source[k]);
      }
    } catch (_) {}
    return out;
  };

  try {
    const originalFetch = globalThis.fetch;
    if (typeof originalFetch === 'function') {
      globalThis.fetch = function (input, init) {
        let target = '';
        try { target = (input && typeof input === 'object' && 'url' in input) ? String(input.url) : String(input); } catch (_) {}
        const watched = isAPI(target);
        if (watched) {
          let headers = {};
          try {
            if (init && init.headers) headers = collect(init.headers.forEach ? init.headers : new Headers(init.headers));
            else if (input && typeof input === 'object' && input.headers) headers = collect(input.headers);
          } catch (_) {}
          let sent = '';
          try {
            const raw = (init && init.body !== undefined) ? init.body : ((input && typeof input === 'object') ? input.body : undefined);
            if (typeof raw === 'string') sent = raw;
          } catch (_) {}
          noteRequest(target, headers, sent);
        }
        const promise = originalFetch.apply(this, arguments);
        if (!watched) return promise;
        return promise.then((response) => {
          try { response.clone().text().then((text) => noteResponse(target, text), () => {}); } catch (_) {}
          return response;
        });
      };
    }
  } catch (_) {}

  try {
    const proto = globalThis.XMLHttpRequest && globalThis.XMLHttpRequest.prototype;
    if (proto && typeof proto.open === 'function') {
      const open = proto.open, setHeader = proto.setRequestHeader, send = proto.send;
      proto.open = function (method, url) { try { this.__flipAiGVUrl = String(url); this.__flipAiGVHeaders = {}; } catch (_) {} return open.apply(this, arguments); };
      proto.setRequestHeader = function (name, value) {
        try {
          const n = String(name || '').toLowerCase();
          if (this.__flipAiGVHeaders && WANTED.indexOf(n) >= 0) this.__flipAiGVHeaders[n] = String(value);
        } catch (_) {}
        return setHeader.apply(this, arguments);
      };
      proto.send = function (payload) {
        try {
          const target = this.__flipAiGVUrl || '';
          if (isAPI(target)) {
            noteRequest(target, this.__flipAiGVHeaders || {}, typeof payload === 'string' ? payload : '');
            this.addEventListener('loadend', () => {
              try { if (this.readyState === 4 && this.status >= 200 && this.status < 300) noteResponse(target, String(this.responseText || '')); } catch (_) {}
            });
          }
        } catch (_) {}
        return send.apply(this, arguments);
      };
    }
  } catch (_) {}
})();`

// googleVoiceSMSCaptureTemplateJS returns the most recent request template the
// page produced, or an empty object when the page has not called the service
// yet.
const googleVoiceSMSCaptureTemplateJS = `(()=>{try{return JSON.stringify(globalThis.__flipAiGVNet&&globalThis.__flipAiGVNet.template||{})}catch(_){return '{}'}})()`

// googleVoiceSMSCaptureDrainJS hands over the inbox responses the page received
// since the last drain and clears them, staying inside a byte budget so one
// evaluation never has to carry an unbounded reply.
var googleVoiceSMSCaptureDrainJS = `(()=>{try{
  const s=globalThis.__flipAiGVNet; if(!s||!s.bodies||!s.bodies.length) return '[]';
  const out=[]; let used=0;
  while(s.bodies.length){
    const next=s.bodies[0];
    if(out.length&&used+next.text.length>` + strconv.Itoa(googleVoiceSMSCaptureDrainBudget) + `) break;
    s.bodies.shift(); used+=next.text.length; out.push(next.text);
  }
  return JSON.stringify(out);
}catch(_){return '[]'}})()`

// The signed-in page is Google Voice's own client, and Google treats it as one.
// A Go HTTP client making the same call from the same machine is not the same
// caller: reading an inbox that way is tolerated, but sending a text -- the
// operation abuse protection actually cares about -- is refused outright, and
// refused persistently rather than for a while.
//
// So the request is issued from inside the page instead. The capture script
// proves this works: it wraps fetch on voice.google.com and sees the page's own
// calls to clients6.google.com, which means those calls are ordinary
// cross-origin requests from that page rather than something tunnelled through
// the gapi proxy frame. A fetch FlipAi runs there is the same request from the
// same origin with the same cookies, and Google answers it the same way.
//
// FlipAi still computes the authorization itself, because the value is
// time-bound and a captured one goes stale.

// googleVoiceSMSPageResponse is what the page reports back about the request it
// made on FlipAi's behalf.
type googleVoiceSMSPageResponse struct {
	Status int    `json:"status"`
	Text   string `json:"text"`
	Error  string `json:"error,omitempty"`

	// RetryAfter is Google's own requested wait. A cross-origin response only
	// exposes a header when the server says it may be read, so this can be
	// empty even when Google sent one; the backoff schedule covers that.
	RetryAfter string `json:"retryAfter,omitempty"`
}

// googleVoiceSMSPageRequestMarker lets the DevTools channel recognize this
// expression and give it a network-sized deadline instead of the short one a
// page probe gets. See webViewDevToolsCallTimeout.
const googleVoiceSMSPageRequestMarker = "__flipAiGVRequest"

// googleVoiceSMSPageRequestJS builds the expression that performs one request in
// the page. Every value is JSON-encoded into the source, so nothing in a URL,
// header or message body can end the expression and become code.
//
// The fetch carries its own deadline, comfortably inside the one the host
// waits. A fetch the host stopped waiting for keeps running in the page and can
// still deliver the text, so it is the page that gives up first: that way the
// outcome is reported rather than guessed at.
func googleVoiceSMSPageRequestJS(url string, headers map[string]string, body string) (string, error) {
	encodedURL, err := json.Marshal(url)
	if err != nil {
		return "", err
	}
	encodedHeaders, err := json.Marshal(headers)
	if err != nil {
		return "", err
	}
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	limit := strconv.Itoa(googleVoiceSMSPageResponseLimit)
	return `(async()=>{const ` + googleVoiceSMSPageRequestMarker + `=1;` +
		`const c=new AbortController();const k=setTimeout(()=>c.abort(),` + strconv.FormatInt(googleVoiceSMSPageRequestTimeout.Milliseconds(), 10) + `);try{` +
		`const r=await fetch(` + string(encodedURL) + `,{method:'POST',credentials:'include',signal:c.signal,headers:` + string(encodedHeaders) + `,body:` + string(encodedBody) + `});` +
		`let t='';try{t=await r.text()}catch(_){}` +
		`let ra='';try{ra=r.headers.get('Retry-After')||''}catch(_){}` +
		`return JSON.stringify({status:r.status,retryAfter:ra,text:t.length>` + limit + `?t.slice(0,` + limit + `):t})` +
		`}catch(e){return JSON.stringify({status:0,error:String((e&&e.message)||e)})}finally{clearTimeout(k)}})()`, nil
}

const googleVoiceSMSPageResponseLimit = 1 << 20

// googleVoiceSMSPageRequestTimeout is how long the page waits for Google. It
// matches the timeout the direct HTTP client used, so moving the request into
// the page did not quietly make slow connections fail.
const googleVoiceSMSPageRequestTimeout = 20 * time.Second

// googleVoiceSMSPageRequestDeadline is how long the host waits for the page. It
// must outlast the fetch's own deadline, so the page always gets to report the
// outcome instead of the host timing out on a request that is still running.
const googleVoiceSMSPageRequestDeadline = googleVoiceSMSPageRequestTimeout + 10*time.Second

func parseGoogleVoiceSMSPageResponse(raw string) (googleVoiceSMSPageResponse, error) {
	var out googleVoiceSMSPageResponse
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, errors.New("the Google Voice page returned nothing")
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, errors.New("the Google Voice page returned an unreadable result")
	}
	return out, nil
}

// googleVoiceSMSServiceMessage digs Google's own words out of an error body, so
// a failure says what Google said rather than only its status code. Falling
// back to a short snippet keeps an unrecognized shape from being silent.
func googleVoiceSMSServiceMessage(raw string) string {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), ")]}'"))
	if raw == "" {
		return ""
	}
	var decoded struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &decoded) == nil {
		if msg := strings.TrimSpace(decoded.Error.Message); msg != "" {
			return truncateGoogleVoiceSMSMessage(msg)
		}
		if status := strings.TrimSpace(decoded.Error.Status); status != "" {
			return truncateGoogleVoiceSMSMessage(status)
		}
	}
	return truncateGoogleVoiceSMSMessage(strings.Join(strings.Fields(raw), " "))
}

func truncateGoogleVoiceSMSMessage(v string) string {
	const limit = 240
	if len([]rune(v)) <= limit {
		return v
	}
	return string([]rune(v)[:limit]) + "…"
}

// googleVoiceSMSAPIStatusError turns one HTTP status into the same error both
// request paths agree on, carrying whatever Google said about it.
func googleVoiceSMSAPIStatusError(status int, retryAfter time.Duration, bodyText string) error {
	detail := googleVoiceSMSServiceMessage(bodyText)
	withDetail := func(base string) error {
		if detail == "" {
			return errors.New(base)
		}
		return fmt.Errorf("%s: %s", base, detail)
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return errGoogleVoiceSMSNotAuthenticated
	case status == http.StatusTooManyRequests:
		// Google declined to process the request, which is the one answer that
		// makes sending again safe.
		return googleVoiceSMSRefused(withDetail("Google Voice web service is asking FlipAi to slow down"), retryAfter)
	case status >= 500:
		// A server error can be raised on either side of the text going out.
		return googleVoiceSMSUnknownOutcome(withDetail(fmt.Sprintf("Google Voice web service returned HTTP %d", status)), retryAfter)
	case status < 200 || status >= 300:
		return withDetail(fmt.Sprintf("Google Voice web service returned HTTP %d", status))
	}
	return nil
}

// googleVoiceSMSCapturedSendJS hands over the last real send Google Voice made,
// if it has made one since the browser started.
const googleVoiceSMSCapturedSendJS = `(()=>{try{return JSON.stringify(globalThis.__flipAiGVNet&&globalThis.__flipAiGVNet.send||{})}catch(_){return '{}'}})()`

// googleVoiceSMSCapturedSend is one real send request, as Google Voice built it.
type googleVoiceSMSCapturedSend struct {
	AtMs int64  `json:"at"`
	URL  string `json:"url"`
	Key  string `json:"key"`
	Body string `json:"body"`
}

// The learned format is kept as a shape, not as a message. Before it is
// written anywhere the conversation and the text are replaced with placeholders
// that satisfy the same slot rules, so what persists is the structure Google
// Voice uses and never what anyone actually said.
const (
	googleVoiceSMSTemplateThread = "t.+15550000000"
	googleVoiceSMSTemplateText   = "flipai"
)

func googleVoiceSMSSendTemplatePath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-send-template.json")
}

type googleVoiceSMSSendTemplate struct {
	Body    string    `json:"body"`
	Learned time.Time `json:"learned"`
}

// googleVoiceSMSSendTemplateFrom turns one real send into a reusable shape,
// keeping nothing of the message it was carrying.
func googleVoiceSMSSendTemplateFrom(captured string) (string, bool) {
	stripped, ok := googleVoiceSMSSendBodyFromCapture(captured, googleVoiceSMSTemplateThread, googleVoiceSMSTemplateText, 1)
	if !ok {
		return "", false
	}
	// A shape that cannot be filled in again is not a usable template.
	if _, ok := googleVoiceSMSSendBodyFromCapture(string(stripped), googleVoiceSMSTemplateThread, googleVoiceSMSTemplateText, 2); !ok {
		return "", false
	}
	return string(stripped), true
}

func saveGoogleVoiceSMSSendTemplate(dataDir, captured string) bool {
	shape, ok := googleVoiceSMSSendTemplateFrom(captured)
	if !ok {
		return false
	}
	if existing, found := loadGoogleVoiceSMSSendTemplate(dataDir); found && existing == shape {
		return true
	}
	raw, err := json.Marshal(googleVoiceSMSSendTemplate{Body: shape, Learned: time.Now()})
	if err != nil || os.MkdirAll(dataDir, 0700) != nil {
		return false
	}
	path := googleVoiceSMSSendTemplatePath(dataDir)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0600) != nil {
		return false
	}
	if os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
		return false
	}
	return true
}

// loadGoogleVoiceSMSSendTemplate returns the shape learned in an earlier
// session. The page keeps its capture only for the life of one document, so
// without this the one-time teaching step would be a step before every reply.
func loadGoogleVoiceSMSSendTemplate(dataDir string) (string, bool) {
	raw, err := os.ReadFile(googleVoiceSMSSendTemplatePath(dataDir))
	if err != nil {
		return "", false
	}
	var stored googleVoiceSMSSendTemplate
	if json.Unmarshal(raw, &stored) != nil {
		return "", false
	}
	if _, ok := googleVoiceSMSSendBodyFromCapture(stored.Body, googleVoiceSMSTemplateThread, googleVoiceSMSTemplateText, 1); !ok {
		return "", false
	}
	return stored.Body, true
}

func parseGoogleVoiceSMSCapturedSend(raw string) googleVoiceSMSCapturedSend {
	var out googleVoiceSMSCapturedSend
	if raw = strings.TrimSpace(raw); raw == "" {
		return out
	}
	if json.Unmarshal([]byte(raw), &out) != nil {
		return googleVoiceSMSCapturedSend{}
	}
	return out
}

// googleVoiceSMSSendBodyFromCapture rebuilds a real send with FlipAi's own
// recipient and text.
//
// The payload for this endpoint was written from a guess at its shape, and a
// guess is what the service keeps refusing. Google Voice itself builds a
// correct one every time the user sends a text from the window FlipAi already
// runs, so when one has been observed FlipAi reuses that exact structure and
// changes only what has to change: the conversation, the message, and the
// tracking id.
//
// The slots are found by what they contain rather than by position, so a
// reordering on Google's side does not silently put the message where the
// conversation belongs. If any of them cannot be identified the caller keeps
// the built-in shape rather than sending something half-rewritten.
func googleVoiceSMSSendBodyFromCapture(captured, threadID, text string, nonce int64) ([]byte, bool) {
	threadID = strings.TrimSpace(threadID)
	text = strings.TrimSpace(text)
	if googleVoiceSMSItemPhone(threadID) == "" || text == "" {
		return nil, false
	}
	var fields []any
	if json.Unmarshal([]byte(strings.TrimSpace(captured)), &fields) != nil || len(fields) == 0 {
		return nil, false
	}
	threadAt, textAt, nonceAt := -1, -1, -1
	for i, field := range fields {
		switch value := field.(type) {
		case string:
			if googleVoiceSMSItemPhone(value) != "" {
				if threadAt < 0 {
					threadAt = i
				}
				continue
			}
			if value != "" && textAt < 0 {
				textAt = i
			}
		case []any:
			if len(value) == 1 && nonceAt < 0 {
				if _, ok := value[0].(float64); ok {
					nonceAt = i
				}
			}
		}
	}
	if threadAt < 0 || textAt < 0 {
		return nil, false
	}
	fields[threadAt] = threadID
	fields[textAt] = text
	if nonceAt >= 0 {
		fields[nonceAt] = []any{nonce}
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, false
	}
	return out, true
}

// googleVoiceSMSAuthTemplate is what the page observed about its own request.
type googleVoiceSMSAuthTemplate struct {
	AtMs    int64             `json:"at"`
	URL     string            `json:"url"`
	Key     string            `json:"key"`
	Origin  string            `json:"origin"`
	Headers map[string]string `json:"headers"`
}

func (t googleVoiceSMSAuthTemplate) header(name string) string {
	if t.Headers == nil {
		return ""
	}
	return strings.TrimSpace(t.Headers[strings.ToLower(name)])
}

// Fresh reports whether the template is recent enough to describe the session
// FlipAi is using now. A template older than this is still useful for the API
// key, but its origin evidence is re-derived on the next page request anyway.
func (t googleVoiceSMSAuthTemplate) Fresh(now time.Time) bool {
	if t.AtMs <= 0 {
		return false
	}
	age := now.Sub(time.UnixMilli(t.AtMs))
	return age >= -time.Minute && age < 12*time.Hour
}

func parseGoogleVoiceSMSAuthTemplate(raw string) googleVoiceSMSAuthTemplate {
	var t googleVoiceSMSAuthTemplate
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return t
	}
	if json.Unmarshal([]byte(raw), &t) != nil {
		return googleVoiceSMSAuthTemplate{}
	}
	if !strings.HasPrefix(t.Key, "AIza") {
		t.Key = ""
	}
	return t
}

func parseGoogleVoiceSMSCaptureDrain(raw string) []string {
	var out []string
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// googleVoiceSMSAuthSchemes pairs Google's authorization scheme names with the
// cookie each one is computed from. Sending a 3P cookie's hash under the 1P
// scheme name is rejected, so the name and the cookie must always travel
// together.
var googleVoiceSMSAuthSchemes = []struct{ Scheme, Cookie string }{
	{"SAPISIDHASH", "SAPISID"},
	{"SAPISID1PHASH", "__Secure-1PAPISID"},
	{"SAPISID3PHASH", "__Secure-3PAPISID"},
}

// googleVoiceSMSAuthOrigins are the two origins Google's own Voice client uses
// when it signs a request: the gapi proxy that actually issues the call, and
// the Voice page that hosts it.
var googleVoiceSMSAuthOrigins = []string{"https://clients6.google.com", "https://voice.google.com"}

func googleVoiceSMSHashValue(cookie, origin string, unix int64) string {
	ts := strconv.FormatInt(unix, 10)
	sum := sha1.Sum([]byte(ts + " " + cookie + " " + origin))
	return ts + "_" + hex.EncodeToString(sum[:])
}

// googleVoiceSMSAuthorization builds the authorization value for every cookie
// the browser session actually has, each under its own scheme name. Google
// accepts the space-separated list and picks the one it wants.
func googleVoiceSMSAuthorization(cookies map[string]string, origin string, now time.Time) string {
	if origin == "" {
		origin = googleVoiceSMSAuthOrigins[0]
	}
	unix := now.Unix()
	parts := make([]string, 0, len(googleVoiceSMSAuthSchemes)*2)
	for _, s := range googleVoiceSMSAuthSchemes {
		value := strings.TrimSpace(cookies[s.Cookie])
		if value == "" {
			continue
		}
		parts = append(parts, s.Scheme, googleVoiceSMSHashValue(value, origin, unix))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// googleVoiceSMSOriginFromAuthorization recovers the origin Google's own client
// signed with, by re-computing each candidate against the captured value. This
// is the difference between a request Google accepts and a 401 that looks like
// a signed-out browser.
func googleVoiceSMSOriginFromAuthorization(header string, cookies map[string]string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(header))
	if len(fields) < 2 {
		return "", false
	}
	byScheme := make(map[string]string, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		byScheme[strings.ToUpper(fields[i])] = fields[i+1]
	}
	for _, s := range googleVoiceSMSAuthSchemes {
		observed, ok := byScheme[s.Scheme]
		if !ok {
			continue
		}
		cookie := strings.TrimSpace(cookies[s.Cookie])
		if cookie == "" {
			continue
		}
		cut := strings.Index(observed, "_")
		if cut <= 0 {
			continue
		}
		unix, err := strconv.ParseInt(observed[:cut], 10, 64)
		if err != nil || unix <= 0 {
			continue
		}
		for _, origin := range googleVoiceSMSAuthOrigins {
			if googleVoiceSMSHashValue(cookie, origin, unix) == observed {
				return origin, true
			}
		}
	}
	return "", false
}

// googleVoiceSMSMergedHeaders combines FlipAi's own defaults with whatever the
// page's real request carried, letting the observed values win. Header names
// are normalized so one canonical set reaches the request.
func googleVoiceSMSMergedHeaders(defaults map[string]string, t googleVoiceSMSAuthTemplate) []string {
	merged := make(map[string]string, len(defaults)+len(t.Headers))
	for name, value := range defaults {
		merged[strings.ToLower(name)] = value
	}
	for name, value := range t.Headers {
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		// Authorization is time-bound, so a captured one is evidence about the
		// origin rather than a credential to replay.
		if name == "" || value == "" || name == "authorization" {
			continue
		}
		merged[name] = value
	}
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names)*2)
	for _, name := range names {
		out = append(out, name, merged[name])
	}
	return out
}
