package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGoogleVoiceSMSAuthorizationCoversEverySchemeTheSessionHas(t *testing.T) {
	now := time.Unix(1788667200, 0)
	got := googleVoiceSMSAuthorization(map[string]string{
		"SAPISID":             "one",
		"__Secure-3PAPISID":   "three",
		"__Secure-1PAPISID":   "onep",
		"UnrelatedCookieName": "ignored",
	}, "https://clients6.google.com", now)

	for _, scheme := range []string{"SAPISIDHASH", "SAPISID1PHASH", "SAPISID3PHASH"} {
		if !strings.Contains(got, scheme+" ") {
			t.Fatalf("authorization is missing %s: %q", scheme, got)
		}
	}
	if strings.Contains(got, "ignored") {
		t.Fatalf("an unrelated cookie was signed: %q", got)
	}

	// A session that only carries the partitioned cookie still authenticates,
	// and must be signed under its own scheme name rather than SAPISIDHASH.
	only := googleVoiceSMSAuthorization(map[string]string{"__Secure-3PAPISID": "three"}, "https://clients6.google.com", now)
	if !strings.HasPrefix(only, "SAPISID3PHASH ") {
		t.Fatalf("a 3P-only session was signed under the wrong scheme name: %q", only)
	}
	if googleVoiceSMSAuthorization(map[string]string{"NothingUseful": "x"}, "", now) != "" {
		t.Fatal("an unsigned session produced an authorization value")
	}
}

// The origin inside a SAPISIDHASH is not guessable and a wrong one is a 401
// that looks exactly like a signed-out browser. It is recovered from a real
// request the page made instead.
func TestGoogleVoiceSMSOriginIsRecoveredFromTheObservedRequest(t *testing.T) {
	cookies := map[string]string{"SAPISID": "cookie-value"}
	for _, want := range googleVoiceSMSAuthOrigins {
		header := "SAPISIDHASH " + googleVoiceSMSHashValue("cookie-value", want, 1788667200)
		got, ok := googleVoiceSMSOriginFromAuthorization(header, cookies)
		if !ok || got != want {
			t.Fatalf("origin %q was not recovered: got %q ok=%v", want, got, ok)
		}
	}
	if _, ok := googleVoiceSMSOriginFromAuthorization("SAPISIDHASH 1788667200_deadbeef", cookies); ok {
		t.Fatal("a value that matches no origin was accepted")
	}
	if _, ok := googleVoiceSMSOriginFromAuthorization("", cookies); ok {
		t.Fatal("an absent authorization header produced an origin")
	}
}

func TestGoogleVoiceSMSOriginRecoveryHandlesMultipleSchemes(t *testing.T) {
	cookies := map[string]string{"SAPISID": "one", "__Secure-3PAPISID": "three"}
	header := "SAPISIDHASH " + googleVoiceSMSHashValue("one", "https://clients6.google.com", 42) +
		" SAPISID3PHASH " + googleVoiceSMSHashValue("three", "https://clients6.google.com", 42)
	got, ok := googleVoiceSMSOriginFromAuthorization(header, cookies)
	if !ok || got != "https://clients6.google.com" {
		t.Fatalf("multi-scheme authorization was not understood: %q ok=%v", got, ok)
	}
}

func TestGoogleVoiceSMSTemplateSuppliesTheLiveKeyAndHeaders(t *testing.T) {
	raw := `{"at":1,"url":"https://clients6.google.com/voice/v1/voiceclient/api2thread/list?alt=json&key=AIzaLIVEKEYLIVEKEYLIVEKEY0","key":"AIzaLIVEKEYLIVEKEYLIVEKEY0","origin":"https://voice.google.com","headers":{"authorization":"SAPISIDHASH 1_2","x-client-version":"999","x-goog-authuser":"3"}}`
	template := parseGoogleVoiceSMSAuthTemplate(raw)
	if template.Key != "AIzaLIVEKEYLIVEKEYLIVEKEY0" {
		t.Fatalf("the live API key was not read: %q", template.Key)
	}
	if template.header("x-client-version") != "999" {
		t.Fatalf("the live client version was not read: %+v", template.Headers)
	}

	merged := googleVoiceSMSMergedHeaders(map[string]string{"x-client-version": "stale", "accept": "*/*"}, template)
	values := map[string]string{}
	for i := 0; i+1 < len(merged); i += 2 {
		values[merged[i]] = merged[i+1]
	}
	if values["x-client-version"] != "999" {
		t.Fatalf("a stale built-in header beat the observed one: %v", values)
	}
	if values["accept"] != "*/*" {
		t.Fatalf("a default header was dropped: %v", values)
	}
	// A captured authorization is time-bound evidence about the origin, never a
	// credential to replay.
	if _, ok := values["authorization"]; ok {
		t.Fatal("a captured authorization value was replayed")
	}
}

func TestGoogleVoiceSMSTemplateRejectsJunk(t *testing.T) {
	if got := parseGoogleVoiceSMSAuthTemplate("not json"); got.Key != "" || len(got.Headers) != 0 {
		t.Fatalf("junk produced a template: %+v", got)
	}
	if got := parseGoogleVoiceSMSAuthTemplate(`{"key":"not-a-google-key"}`); got.Key != "" {
		t.Fatalf("a key that is not a Google API key was accepted: %q", got.Key)
	}
	if got := parseGoogleVoiceSMSAuthTemplate("{}"); got.Fresh(time.Now()) {
		t.Fatal("an empty template reported itself as fresh")
	}
}

func TestGoogleVoiceSMSCaptureDrainParsesObservedInboxes(t *testing.T) {
	bodies := []string{`{"thread":[]}`, `{"thread":[{"id":"t.+18455550142"}]}`}
	raw, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	got := parseGoogleVoiceSMSCaptureDrain(string(raw))
	if len(got) != 2 || got[1] != bodies[1] {
		t.Fatalf("observed inbox updates were lost: %+v", got)
	}
	if parseGoogleVoiceSMSCaptureDrain("nonsense") != nil {
		t.Fatal("junk from the page was accepted as inbox data")
	}
}

// The capture script has to wrap the page's own network calls before the page
// takes its own reference to them, and it must never touch the conversation UI.
func TestGoogleVoiceSMSCaptureScriptOnlyObserves(t *testing.T) {
	for _, want := range []string{
		"globalThis.fetch",
		"XMLHttpRequest",
		"clients6.google.com",
		"/voice/v1/voiceclient/",
		"authorization",
	} {
		if !strings.Contains(googleVoiceSMSNetworkCaptureJS, want) {
			t.Fatalf("the network capture script is missing %q", want)
		}
	}
	for _, forbidden := range []string{".click(", "querySelector", "aria-label", "dispatchEvent", "location.replace"} {
		if strings.Contains(googleVoiceSMSNetworkCaptureJS, forbidden) {
			t.Fatalf("the network capture script drives the page through %q", forbidden)
		}
	}
}
