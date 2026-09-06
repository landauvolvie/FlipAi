package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The agent has already spent its turn producing the answer and the person who
// texted has no other way to receive it, so one busy response from Google must
// not be the end of the attempt.
func TestGoogleVoiceSMSBusyResponsesAreWorthAskingAgain(t *testing.T) {
	busy := googleVoiceSMSTransient(errors.New("Google Voice web service is asking FlipAi to slow down"), 7*time.Second)
	wait, retryable := googleVoiceSMSRetryable(busy)
	if !retryable {
		t.Fatal("a rate-limited reply was treated as final")
	}
	if wait != 7*time.Second {
		t.Fatalf("Google's own wait was lost: %v", wait)
	}
	if !strings.Contains(busy.Error(), "slow down") {
		t.Fatalf("the reason was lost: %q", busy.Error())
	}
}

// A refusal is not a busy signal. Retrying one wastes the delivery budget and
// still fails, so only transient errors are retried.
func TestGoogleVoiceSMSRefusalsAreNotRetried(t *testing.T) {
	for _, final := range []error{
		errors.New("Google Voice refused the SMS: invalid recipient"),
		errGoogleVoiceSMSNotAuthenticated,
		errors.New("Google Voice reply blocked: exact conversation thread is required"),
	} {
		if _, retryable := googleVoiceSMSRetryable(final); retryable {
			t.Fatalf("a final refusal was queued for retry: %v", final)
		}
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
