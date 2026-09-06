package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// The agent has already spent its turn producing the answer and the person who
// texted has no other way to receive it, so Google declining to process the
// request must not be the end of the attempt.
func TestGoogleVoiceSMSRefusedRequestsAreSentAgain(t *testing.T) {
	busy := googleVoiceSMSRefused(errors.New("Google Voice web service is asking FlipAi to slow down"), 7*time.Second)
	wait, resendable := googleVoiceSMSResendable(busy)
	if !resendable {
		t.Fatal("a rate-limited reply was treated as final")
	}
	if wait != 7*time.Second {
		t.Fatalf("Google's own wait was lost: %v", wait)
	}
	if !strings.Contains(busy.Error(), "slow down") {
		t.Fatalf("the reason was lost: %q", busy.Error())
	}
	if _, retryable := googleVoiceSMSRetryable(busy); !retryable {
		t.Fatal("a refused request should also be safe to read again")
	}
}

// The dangerous case: a 5xx, or a connection dropping while the response was
// being read, may mean Google already sent the text. Sending again would text
// the person the same answer twice and charge them for it. Reading again is
// harmless, so only reading may repeat.
func TestGoogleVoiceSMSUnknownOutcomesAreNeverSentAgain(t *testing.T) {
	for _, ambiguous := range []error{
		googleVoiceSMSUnknownOutcome(errors.New("Google Voice web service is unreachable"), 0),
		googleVoiceSMSUnknownOutcome(errors.New("Google Voice web service returned HTTP 503"), 4*time.Second),
	} {
		if _, resendable := googleVoiceSMSResendable(ambiguous); resendable {
			t.Fatalf("a reply that may already have been delivered was queued to send again: %v", ambiguous)
		}
		if _, retryable := googleVoiceSMSRetryable(ambiguous); !retryable {
			t.Fatalf("reading the inbox again should stay safe: %v", ambiguous)
		}
	}
}

// A refusal on the merits is final for both.
func TestGoogleVoiceSMSFinalErrorsAreNeitherResentNorReread(t *testing.T) {
	for _, final := range []error{
		errors.New("Google Voice refused the SMS: invalid recipient"),
		errGoogleVoiceSMSNotAuthenticated,
		errors.New("Google Voice reply blocked: exact conversation thread is required"),
	} {
		if _, retryable := googleVoiceSMSRetryable(final); retryable {
			t.Fatalf("a final refusal was queued for retry: %v", final)
		}
		if _, resendable := googleVoiceSMSResendable(final); resendable {
			t.Fatalf("a final refusal was queued to send again: %v", final)
		}
	}
}

// One reply keeps one tracking id across its attempts, so a resend carries the
// identity of the message it is resending rather than looking like a second,
// unrelated text.
func TestGoogleVoiceSMSResendKeepsOneTrackingID(t *testing.T) {
	const nonce = 1788667200123456789
	first, err := googleVoiceSMSAPISendBody("t.+18453241813", "the answer", nonce)
	if err != nil {
		t.Fatal(err)
	}
	second, err := googleVoiceSMSAPISendBody("t.+18453241813", "the answer", nonce)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("the same reply produced two different payloads:\n%s\n%s", first, second)
	}
	var decoded []any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 9 {
		t.Fatalf("unexpected send payload shape: %s", first)
	}
}

func TestGoogleVoiceSMSRetryAfterIsUnderstoodInBothForms(t *testing.T) {
	now := time.Date(2026, 9, 6, 6, 0, 0, 0, time.UTC)
	if got := parseGoogleVoiceSMSRetryAfter("30", now); got != 30*time.Second {
		t.Fatalf("seconds form: %v", got)
	}
	if got := parseGoogleVoiceSMSRetryAfter(now.Add(45*time.Second).Format(http.TimeFormat), now); got < 44*time.Second || got > 45*time.Second {
		t.Fatalf("http-date form: %v", got)
	}
	for _, ignored := range []string{"", "not a number", "0", "-5", now.Add(-time.Hour).Format(http.TimeFormat)} {
		if got := parseGoogleVoiceSMSRetryAfter(ignored, now); got != 0 {
			t.Fatalf("%q produced a wait of %v", ignored, got)
		}
	}
}

// The backoff has to grow, respect a longer wait Google asked for, stay
// bounded, and fit inside the delivery budget the bridge allows.
func TestGoogleVoiceSMSSendBackoffFitsTheDeliveryBudget(t *testing.T) {
	var total time.Duration
	previous := time.Duration(0)
	for attempt := 0; attempt < googleVoiceSMSSendAttempts-1; attempt++ {
		wait := googleVoiceSMSSendBackoff(attempt, 0)
		if wait <= previous {
			t.Fatalf("attempt %d did not back off further: %v after %v", attempt, wait, previous)
		}
		previous = wait
		total += wait
	}
	if total > googleVoiceSMSOutboundBudget {
		t.Fatalf("the retry schedule (%v) does not fit the delivery budget (%v)", total, googleVoiceSMSOutboundBudget)
	}
	if got := googleVoiceSMSSendBackoff(0, time.Hour); got != googleVoiceSMSSendMaxBackoff {
		t.Fatalf("an absurd Retry-After was not capped: %v", got)
	}
	if got := googleVoiceSMSSendBackoff(0, 20*time.Second); got != 20*time.Second {
		t.Fatalf("Google's own longer wait was not honored: %v", got)
	}
}

// The send loop must ask the strict question. Using the read-side check there
// would resend a reply whose outcome is unknown, delivering it twice.
func TestGoogleVoiceSMSSendLoopUsesTheStrictResendCheck(t *testing.T) {
	raw, err := os.ReadFile("google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	start := strings.Index(body, "func googleVoiceSMSAPISend(")
	if start < 0 {
		t.Fatal("the Google Voice send loop is gone")
	}
	send := body[start:]
	if end := strings.Index(send, "\nfunc "); end > 0 {
		send = send[:end]
	}
	if !strings.Contains(send, "googleVoiceSMSResendable(") {
		t.Fatal("the send loop no longer asks whether Google refused the request")
	}
	if strings.Contains(send, "googleVoiceSMSRetryable(") {
		t.Fatal("the send loop uses the read-side check, which resends replies that may already have gone out")
	}
	// One tracking id for the whole reply, generated outside the attempt loop.
	if strings.Contains(send, "UnixNano()") && strings.Index(send, "UnixNano()") > strings.Index(send, "for attempt") {
		t.Fatal("the tracking id is generated inside the attempt loop, so a resend looks like an unrelated text")
	}
}

// Taking what the page already received costs Google nothing, so it must not be
// blocked by a reply holding the quota: an inbound text arriving during a retry
// sequence would otherwise wait up to the whole delivery budget.
func TestGoogleVoiceSMSPassiveDrainSurvivesAPendingReply(t *testing.T) {
	raw, err := os.ReadFile("google_voice_sms_api_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	start := strings.Index(body, "func runGoogleVoiceSMSAPIInboxLoop")
	if start < 0 {
		t.Fatal("the Google Voice inbox loop is gone")
	}
	loop := body[start:]
	pending := strings.Index(loop, "googleVoiceSMSOutboundPending.Load()")
	if pending < 0 {
		t.Fatal("the inbox loop no longer yields the quota to a reply in flight")
	}
	guarded := loop[pending:]
	if end := strings.Index(guarded, "if time.Now().Before(nextPoll)"); end > 0 {
		guarded = guarded[:end]
	}
	if !strings.Contains(guarded, "drain()") {
		t.Fatal("a reply in flight also suppresses the passive drain, which costs Google nothing")
	}
}

// The poll is what stamps the readiness probe, so a readiness window shorter
// than the gap between polls would declare a healthy listener dead between two
// of its own heartbeats -- flickering "Not connected" and blocking replies.
func TestGoogleVoiceSMSReadinessOutlastsThePollThatFeedsIt(t *testing.T) {
	if googleVoiceSMSFreshWindow <= googleVoiceSMSPollInterval {
		t.Fatalf("readiness (%v) expires within one poll interval (%v)", googleVoiceSMSFreshWindow, googleVoiceSMSPollInterval)
	}
	if googleVoiceSMSFreshWindow < 2*googleVoiceSMSPollInterval {
		t.Fatalf("readiness (%v) leaves no margin for one missed poll (%v)", googleVoiceSMSFreshWindow, googleVoiceSMSPollInterval)
	}
	// Test and the outbound send gate both wait 15 seconds for readiness, so a
	// cold poll has to be able to land inside that.
	if googleVoiceSMSPollInterval > 15*time.Second {
		t.Fatalf("a poll every %v cannot satisfy the 15-second readiness wait", googleVoiceSMSPollInterval)
	}
}
