package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Every value goes into the expression JSON-encoded, so nothing in a message
// body can end the expression and become code running in a signed-in Google
// session.
func TestGoogleVoiceSMSPageRequestCannotBeEscapedByMessageText(t *testing.T) {
	hostile := "');alert(1);globalThis.x=(('" + "\n\"\\`${}" + "</script>"
	expression, err := googleVoiceSMSPageRequestJS(
		"https://clients6.google.com/voice/v1/voiceclient/api2thread/sendsms?alt=json&key=AIza",
		map[string]string{"authorization": "SAPISIDHASH 1_2'\"</script>"},
		hostile,
	)
	if err != nil {
		t.Fatal(err)
	}
	// The body must appear only as one JSON string literal that decodes back to
	// exactly what went in. That is what keeps it data: a quote or a newline
	// that survived raw could close the literal and start executing.
	encoded, err := json.Marshal(hostile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expression, "body:"+string(encoded)) {
		t.Fatalf("the message body is not carried as an encoded literal: %s", expression)
	}
	var roundTripped string
	if err := json.Unmarshal(encoded, &roundTripped); err != nil || roundTripped != hostile {
		t.Fatalf("the encoded body does not decode back to the original: %q", roundTripped)
	}
	for _, raw := range []string{"</script>", "\n"} {
		if strings.Contains(expression, raw) {
			t.Fatalf("raw %q survived encoding into the expression", raw)
		}
	}
	if !strings.Contains(expression, "credentials:'include'") {
		t.Fatal("the page request does not carry the signed-in session")
	}
	if !strings.Contains(expression, "method:'POST'") {
		t.Fatal("the page request is not a POST")
	}
}

func TestGoogleVoiceSMSPageResponseIsUnderstood(t *testing.T) {
	raw, err := json.Marshal(googleVoiceSMSPageResponse{Status: 200, Text: `)]}'` + "\n" + `{"threadItemId":"x"}`})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseGoogleVoiceSMSPageResponse(string(raw))
	if err != nil || got.Status != 200 || !strings.Contains(got.Text, "threadItemId") {
		t.Fatalf("a good page response was not understood: %+v err=%v", got, err)
	}
	if _, err := parseGoogleVoiceSMSPageResponse("not json"); err == nil {
		t.Fatal("junk from the page was accepted")
	}
	if _, err := parseGoogleVoiceSMSPageResponse(""); err == nil {
		t.Fatal("an empty page result was accepted")
	}
}

// A request the page could not complete has an unknown outcome: Google may
// already have sent the text, so it must never be repeated.
func TestGoogleVoiceSMSPageFailureIsNeverResent(t *testing.T) {
	if _, resendable := googleVoiceSMSResendable(googleVoiceSMSAPIStatusError(503, 0, "")); resendable {
		t.Fatal("a 503 from the page path was marked safe to send again")
	}
	if _, resendable := googleVoiceSMSResendable(googleVoiceSMSAPIStatusError(429, 0, "")); !resendable {
		t.Fatal("a 429 refusal is safe to send again and was not")
	}
	if got := googleVoiceSMSAPIStatusError(200, 0, ""); got != nil {
		t.Fatalf("a successful status produced an error: %v", got)
	}
}

// Both request paths must agree about what a status means, or a reply would be
// resent by one and abandoned by the other.
func TestGoogleVoiceSMSStatusErrorsCarryGooglesOwnWords(t *testing.T) {
	body := `)]}'` + "\n" + `{"error":{"code":429,"message":"Quota exceeded for quota metric 'Requests'","status":"RESOURCE_EXHAUSTED"}}`
	err := googleVoiceSMSAPIStatusError(429, 5*time.Second, body)
	if err == nil || !strings.Contains(err.Error(), "Quota exceeded") {
		t.Fatalf("Google's own explanation was discarded: %v", err)
	}
	wait, resendable := googleVoiceSMSResendable(err)
	if !resendable || wait != 5*time.Second {
		t.Fatalf("the refusal lost its retry timing: wait=%v resendable=%v", wait, resendable)
	}

	if err := googleVoiceSMSAPIStatusError(401, 0, ""); err != errGoogleVoiceSMSNotAuthenticated {
		t.Fatalf("401 is not reported as a sign-in problem: %v", err)
	}
	if err := googleVoiceSMSAPIStatusError(400, 0, `{"error":{"message":"Invalid value at 'thread_id'"}}`); err == nil || !strings.Contains(err.Error(), "thread_id") {
		t.Fatalf("a refusal on the merits lost its reason: %v", err)
	}
}

func TestGoogleVoiceSMSServiceMessageFallsBackToASnippet(t *testing.T) {
	if got := googleVoiceSMSServiceMessage(""); got != "" {
		t.Fatalf("an empty body produced %q", got)
	}
	if got := googleVoiceSMSServiceMessage("upstream connect error   \n  reset"); got != "upstream connect error reset" {
		t.Fatalf("an unrecognized body was not reported readably: %q", got)
	}
	long := strings.Repeat("x", 900)
	if got := googleVoiceSMSServiceMessage(long); len([]rune(got)) > 241 {
		t.Fatalf("a long body was not truncated: %d runes", len([]rune(got)))
	}
}

// The reply goes through the page, because that is the caller Google accepts.
// The Go client may not quietly become the normal path again.
func TestGoogleVoiceSMSPrefersThePageForRequests(t *testing.T) {
	raw, err := readGoogleVoiceSMSSource(t, "google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(raw, "func googleVoiceSMSAPIRequest(")
	if start < 0 {
		t.Fatal("the Google Voice request entry point is gone")
	}
	body := raw[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	viaPage := strings.Index(body, "googleVoiceSMSAPIRequestViaPage(")
	viaGo := strings.Index(body, "googleVoiceSMSAPIRequestOnce(")
	if viaPage < 0 {
		t.Fatal("requests no longer go through the signed-in page")
	}
	if viaGo >= 0 && viaGo < viaPage {
		t.Fatal("the Go client is tried before the page again")
	}
	if !strings.Contains(body, "pageUsable") {
		t.Fatal("the Go fallback is no longer limited to a page that could not attempt the request")
	}
}

func readGoogleVoiceSMSSource(t *testing.T, name string) (string, error) {
	t.Helper()
	raw, err := os.ReadFile(name)
	return string(raw), err
}

// Google's own requested wait must survive the page path. Retrying a rate
// limit sooner than Google asked extends it, which is the failure the
// Retry-After handling exists to prevent.
func TestGoogleVoiceSMSPageResponseCarriesRetryAfter(t *testing.T) {
	raw, err := json.Marshal(googleVoiceSMSPageResponse{Status: 429, RetryAfter: "45", Text: `{"error":{"message":"slow down"}}`})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseGoogleVoiceSMSPageResponse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	wait := parseGoogleVoiceSMSRetryAfter(got.RetryAfter, time.Now())
	if wait != 45*time.Second {
		t.Fatalf("Google's requested wait was lost on the page path: %v", wait)
	}
	if backoff := googleVoiceSMSSendBackoff(0, wait); backoff != googleVoiceSMSSendMaxBackoff {
		t.Fatalf("the requested wait did not reach the backoff: %v", backoff)
	}
	// A response that does not expose the header still works, on the schedule.
	if wait := parseGoogleVoiceSMSRetryAfter("", time.Now()); wait != 0 {
		t.Fatalf("an unexposed header produced a wait: %v", wait)
	}

	expression, err := googleVoiceSMSPageRequestJS("https://example.invalid", nil, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expression, "Retry-After") {
		t.Fatal("the page request does not read Retry-After back")
	}
}

// The page request is a network call, so the host must wait longer than the
// page does. A fetch the host abandons is not cancelled and can still deliver
// the text while FlipAi reports the reply as failed.
func TestGoogleVoiceSMSPageRequestOutlivesTheProbeDeadline(t *testing.T) {
	if googleVoiceSMSPageRequestDeadline <= googleVoiceSMSPageRequestTimeout {
		t.Fatalf("the host (%v) does not outlast the page's own deadline (%v)",
			googleVoiceSMSPageRequestDeadline, googleVoiceSMSPageRequestTimeout)
	}
	expression, err := googleVoiceSMSPageRequestJS("https://example.invalid", nil, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expression, googleVoiceSMSPageRequestMarker) {
		t.Fatal("the page request is not marked, so the channel gives it the short probe deadline")
	}
	if !strings.Contains(expression, "AbortController") {
		t.Fatal("the page request has no deadline of its own")
	}
}

// The correction that cost a release: an init script runs in every frame, so
// seeing the page's calls to the web service was never proof they came from the
// main frame. They come from a frame on the service's own origin, and a request
// issued anywhere else is answered "Origin doesn't match Host".
func TestGoogleVoiceSMSPageRequestRunsInAMatchingOriginFrame(t *testing.T) {
	raw, err := readGoogleVoiceSMSSource(t, "google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(raw, "func googleVoiceSMSAPIRequestViaPage(")
	if start < 0 {
		t.Fatal("the page request path is gone")
	}
	body := raw[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "googleVoiceSMSAPIFrameContext(") {
		t.Fatal("the page request no longer looks for a frame on the service's own origin")
	}
	if !strings.Contains(body, "voiceEvalInContext(") {
		t.Fatal("the page request runs in the main frame again, which carries the wrong origin")
	}
	if strings.Contains(body, "voiceEval(d,") {
		t.Fatal("the page request uses the main-frame evaluation, which Google refuses")
	}

	finder := raw[strings.Index(raw, "func googleVoiceSMSAPIFrameContext("):]
	if end := strings.Index(finder, "\nfunc googleVoiceSMSAPIRequestViaPage"); end > 0 {
		finder = finder[:end]
	}
	for _, want := range []string{"Page.getFrameTree", "Page.createIsolatedWorld", "googleVoiceSMSAPIOrigin"} {
		if !strings.Contains(finder, want) {
			t.Fatalf("the frame search is missing %q", want)
		}
	}
}

// With no matching frame there is nothing to run in, and the request must fall
// back to the direct client rather than be issued from the wrong origin. That
// fallback is also what keeps reading the inbox working.
func TestGoogleVoiceSMSFallsBackWhenNoMatchingFrameExists(t *testing.T) {
	raw, err := readGoogleVoiceSMSSource(t, "google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(raw, "func googleVoiceSMSAPIRequestViaPage(")
	body := raw[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	missing := strings.Index(body, "if !ok {")
	if missing < 0 {
		t.Fatal("a missing frame is no longer handled")
	}
	tail := body[missing:]
	if end := strings.Index(tail, "\n\t}"); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "errNoVoiceControlChannel") {
		t.Fatal("a missing frame does not report the one error the caller falls back on")
	}
}

// The payload for sending was written from a guess at its shape, and the
// service kept refusing it. Google Voice builds a correct one every time the
// user sends a text from the same window, so FlipAi reuses that structure and
// changes only the conversation, the message and the tracking id.
func TestGoogleVoiceSMSSendBodyIsLearnedFromARealSend(t *testing.T) {
	real := `[null,null,null,null,"an earlier message","t.+18455550142",[],null,[123456789]]`
	got, ok := googleVoiceSMSSendBodyFromCapture(real, "t.+18453241813", "the answer", 987654321)
	if !ok {
		t.Fatal("a real send was not usable as a template")
	}
	var fields []any
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 9 {
		t.Fatalf("the real shape was not preserved: %s", got)
	}
	if fields[4] != "the answer" {
		t.Fatalf("the message did not land in the message slot: %s", got)
	}
	if fields[5] != "t.+18453241813" {
		t.Fatalf("the conversation did not land in the conversation slot: %s", got)
	}
	nonce, isArray := fields[8].([]any)
	if !isArray || len(nonce) != 1 || nonce[0].(float64) != 987654321 {
		t.Fatalf("the tracking id was not replaced: %s", got)
	}
}

// A half-rewritten payload is worse than the built-in one, so anything the
// slots cannot be identified in is refused outright.
func TestGoogleVoiceSMSSendTemplateRefusesWhatItCannotPlace(t *testing.T) {
	for _, unusable := range []string{
		``,
		`not json`,
		`[]`,
		`[null,null,"only text here"]`,
		`[null,"t.+18455550142"]`,
	} {
		if _, ok := googleVoiceSMSSendBodyFromCapture(unusable, "t.+18453241813", "hi", 1); ok {
			t.Fatalf("an unusable capture was accepted as a template: %q", unusable)
		}
	}
	real := `[null,null,null,null,"x","t.+18455550142",[],null,[1]]`
	if _, ok := googleVoiceSMSSendBodyFromCapture(real, "not-a-thread", "hi", 1); ok {
		t.Fatal("a bad conversation was accepted")
	}
	if _, ok := googleVoiceSMSSendBodyFromCapture(real, "t.+18453241813", "   ", 1); ok {
		t.Fatal("an empty message was accepted")
	}
}

// The capture has to record the request body, not just its headers, or there
// is nothing to learn the shape from.
func TestGoogleVoiceSMSCaptureRecordsRealSendBodies(t *testing.T) {
	for _, want := range []string{"api2thread/sendsms", "store.send", "noteRequest(target, headers, sent)"} {
		if !strings.Contains(googleVoiceSMSNetworkCaptureJS, want) {
			t.Fatalf("the capture script no longer records a real send: missing %q", want)
		}
	}
	got := parseGoogleVoiceSMSCapturedSend(`{"at":1,"url":"https://clients6.google.com/x","body":"[1,2]"}`)
	if got.Body != "[1,2]" {
		t.Fatalf("a captured send was not read back: %+v", got)
	}
	if parseGoogleVoiceSMSCapturedSend("nonsense").Body != "" {
		t.Fatal("junk was accepted as a captured send")
	}
}

// The capture lives on the globals of whichever frame made the request, and
// the send is made by the proxy frame. Reading it from the main frame finds
// nothing, so the format could never be learned no matter how many texts were
// sent -- the same frame separation that made a main-frame request get refused.
func TestGoogleVoiceSMSCapturedSendIsReadFromTheSendingFrame(t *testing.T) {
	raw, err := readGoogleVoiceSMSSource(t, "google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(raw, "func googleVoiceSMSCapturedSendRequest(")
	if start < 0 {
		t.Fatal("the captured-send reader is gone")
	}
	body := raw[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "googleVoiceSMSAPIFrameContext(") {
		t.Fatal("the captured send is no longer read from the frame that made it")
	}
	if !strings.Contains(body, "voiceEvalInContext(") {
		t.Fatal("the captured send is read only from the main frame, where it never exists")
	}
}

// The page forgets its capture with the document, so the one-time teaching step
// has to outlive the page. What persists is the shape, never the message.
func TestGoogleVoiceSMSSendTemplateSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	if _, ok := loadGoogleVoiceSMSSendTemplate(dir); ok {
		t.Fatal("an unlearned install reported a template")
	}
	real := `[null,null,null,null,"something private the user typed","t.+18455550142",[],null,[123]]`
	if !saveGoogleVoiceSMSSendTemplate(dir, real) {
		t.Fatal("a real send was not learned")
	}

	stored, ok := loadGoogleVoiceSMSSendTemplate(dir)
	if !ok {
		t.Fatal("the learned shape did not survive")
	}
	if strings.Contains(stored, "something private the user typed") {
		t.Fatalf("the user's message was persisted, not just the shape: %s", stored)
	}
	if strings.Contains(stored, "8455550142") {
		t.Fatalf("the conversation was persisted, not just the shape: %s", stored)
	}

	// The stored shape still fills in for a real reply.
	payload, ok := googleVoiceSMSSendBodyFromCapture(stored, "t.+18453241813", "the answer", 99)
	if !ok {
		t.Fatal("the stored shape could not be filled in again")
	}
	var fields []any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if fields[4] != "the answer" || fields[5] != "t.+18453241813" {
		t.Fatalf("the stored shape filled the wrong slots: %s", payload)
	}
}

func TestGoogleVoiceSMSSendTemplateRefusesAnUnusableCapture(t *testing.T) {
	dir := t.TempDir()
	for _, unusable := range []string{"", "not json", "[]", `[null,"only text"]`} {
		if saveGoogleVoiceSMSSendTemplate(dir, unusable) {
			t.Fatalf("an unusable capture was learned: %q", unusable)
		}
	}
	if _, ok := loadGoogleVoiceSMSSendTemplate(dir); ok {
		t.Fatal("a template was stored from an unusable capture")
	}
}

// An isolated world shares its frame's storage but not its globals, and an
// isolated world is the only place FlipAi can run code in the sending frame.
// Reading the capture from globals alone therefore always found nothing, so it
// has to cross between worlds through storage.
func TestGoogleVoiceSMSCaptureCrossesWorldsThroughStorage(t *testing.T) {
	if !strings.Contains(googleVoiceSMSNetworkCaptureJS, "setItem('__flipAiGVSend'") {
		t.Fatal("a real send is not left anywhere an isolated world can reach it")
	}
	if !strings.Contains(googleVoiceSMSCapturedSendJS, "getItem('__flipAiGVSend')") {
		t.Fatal("the reader looks only at globals, which an isolated world cannot see")
	}
	if !strings.Contains(googleVoiceSMSCapturedSendJS, "__flipAiGVNet") {
		t.Fatal("the reader no longer checks the same-world globals first")
	}
	if !strings.Contains(googleVoiceSMSForgetCapturedSendJS, "removeItem('__flipAiGVSend')") {
		t.Fatal("the handed-over message is never cleared after the shape is learned")
	}
	for _, js := range []string{googleVoiceSMSCapturedSendJS, googleVoiceSMSForgetCapturedSendJS} {
		for _, forbidden := range []string{".click(", "querySelector", "aria-label"} {
			if strings.Contains(js, forbidden) {
				t.Fatalf("a capture helper drives the page through %q", forbidden)
			}
		}
	}
}

// The learned shape is cleared from the browser once it is safely on disk, so
// the message it was learned from does not linger in storage.
func TestGoogleVoiceSMSForgetsTheCaptureOnceLearned(t *testing.T) {
	raw, err := readGoogleVoiceSMSSource(t, "google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(raw, "func googleVoiceSMSLearnSendTemplate(")
	if start < 0 {
		t.Fatal("the template learner is gone")
	}
	body := raw[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "saveGoogleVoiceSMSSendTemplate(") || !strings.Contains(body, "googleVoiceSMSForgetCapturedSend(") {
		t.Fatal("the capture is not cleared after the shape is stored")
	}
}

// The API key, the headers and the authorization that reveals the signing
// origin are only visible from the frame that called the service. Read from the
// main page they are always empty, and FlipAi then signs every request with the
// built-in key -- which the service answers RESOURCE_EXHAUSTED. That is one
// mistake made in three places; none of them may quietly return.
func TestGoogleVoiceSMSSessionFactsComeFromTheCallingFrame(t *testing.T) {
	raw, err := readGoogleVoiceSMSSource(t, "google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"func googleVoiceSMSCaptureTemplate(", "func googleVoiceSMSAPIKeyFromPage("} {
		start := strings.Index(raw, fn)
		if start < 0 {
			t.Fatalf("%s is gone", fn)
		}
		body := raw[start:]
		if end := strings.Index(body, "\nfunc "); end > 0 {
			body = body[:end]
		}
		if !strings.Contains(body, "googleVoiceSMSAPIFrameContext(") && !strings.Contains(body, "googleVoiceSMSReadFromAPIFrame(") {
			t.Fatalf("%s reads the main page again, where the service is never called", fn)
		}
	}
}

// An isolated world shares its frame's storage and resource timeline but not
// its globals, so the key has to be reachable through both of those.
func TestGoogleVoiceSMSKeyIsReachableFromAnIsolatedWorld(t *testing.T) {
	if !strings.Contains(googleVoiceSMSNetworkCaptureJS, "setItem('__flipAiGVKey'") {
		t.Fatal("the live Google key is not left anywhere an isolated world can reach it")
	}
	raw, err := readGoogleVoiceSMSSource(t, "google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(raw, "func googleVoiceSMSAPIKeyFromPage(")
	body := raw[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	for _, want := range []string{"getItem('__flipAiGVKey')", "getEntriesByType('resource')"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the key lookup lost a source an isolated world can use: %q", want)
		}
	}
}

// An evaluation that succeeds and returns nothing is not an answer. Stopping on
// a bare nil error would skip the main frame whenever the proxy frame exists but
// the call was made elsewhere -- and Google has moved that before.
func TestGoogleVoiceSMSFrameLookupFallsBackOnAnEmptyAnswer(t *testing.T) {
	raw, err := readGoogleVoiceSMSSource(t, "google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(raw, "func googleVoiceSMSReadFromAPIFrame(")
	if start < 0 {
		t.Fatal("the frame-aware lookup is gone")
	}
	body := raw[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "usable(got)") {
		t.Fatal("the lookup accepts an empty answer from the frame and never tries the main page")
	}
	if strings.Count(body, "usable(got)") < 2 {
		t.Fatal("only one of the two lookups checks whether the answer is usable")
	}
	if !strings.Contains(body, "voiceEvalInContext(") || !strings.Contains(body, "voiceEval(d,") {
		t.Fatal("the lookup no longer tries both the calling frame and the main page")
	}
}

// The client version and the authorization the signing origin is recovered from
// are only useful if the isolated world can read them, so they cross the same
// way the key and the send body do.
func TestGoogleVoiceSMSTemplateCrossesWorldsThroughStorage(t *testing.T) {
	if !strings.Contains(googleVoiceSMSNetworkCaptureJS, "setItem('__flipAiGVTemplate'") {
		t.Fatal("the request template is not left anywhere an isolated world can reach it")
	}
	if !strings.Contains(googleVoiceSMSCaptureTemplateJS, "getItem('__flipAiGVTemplate')") {
		t.Fatal("the template reader looks only at globals, which an isolated world cannot see")
	}
	if !strings.Contains(googleVoiceSMSCaptureTemplateJS, "__flipAiGVNet") {
		t.Fatal("the template reader no longer checks the same-world globals first")
	}
	// It still has to parse back into something usable.
	got := parseGoogleVoiceSMSAuthTemplate(`{"at":1,"key":"AIzaLIVEKEYLIVEKEYLIVEKEY0","headers":{"x-client-version":"7"}}`)
	if got.Key == "" || got.header("x-client-version") != "7" {
		t.Fatalf("a stored template did not read back: %+v", got)
	}
}
