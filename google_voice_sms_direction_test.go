package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Direction decides whether FlipAi answers a message. Getting it wrong in the
// inbound direction makes FlipAi answer its own reply, and that loop pays for
// itself in real text messages, so an item FlipAi cannot place is skipped.
func TestGoogleVoiceSMSDirectionUnderstandsGooglesSpellings(t *testing.T) {
	for _, spelling := range []string{"smsIn", "SMS_IN", "sms-in", "INCOMING", "inbound", "received"} {
		msgs, _, err := parseGoogleVoiceSMSAPIListResponseDetailed(
			[]byte(`{"thread":[{"id":"t.+18455550142","item":[{"id":"a","did":"+18455550142","messageText":"hi","type":"`+spelling+`"}]}]}`), "0")
		if err != nil || len(msgs) != 1 || msgs[0].Outgoing {
			t.Fatalf("%q was not understood as inbound: %+v err=%v", spelling, msgs, err)
		}
	}
	for _, spelling := range []string{"smsOut", "SMS_OUT", "sms-out", "OUTGOING", "outbound", "sent"} {
		msgs, _, err := parseGoogleVoiceSMSAPIListResponseDetailed(
			[]byte(`{"thread":[{"id":"t.+18455550142","item":[{"id":"a","did":"+18455550142","messageText":"hi","type":"`+spelling+`"}]}]}`), "0")
		if err != nil || len(msgs) != 1 || !msgs[0].Outgoing {
			t.Fatalf("%q was not understood as outgoing: %+v err=%v", spelling, msgs, err)
		}
	}
}

func TestGoogleVoiceSMSDirectionPrefersAnExplicitFlag(t *testing.T) {
	msgs, _, err := parseGoogleVoiceSMSAPIListResponseDetailed(
		[]byte(`{"thread":[{"id":"t.+18455550142","item":[{"id":"a","did":"+18455550142","messageText":"hi","type":41,"outgoing":true}]}]}`), "0")
	if err != nil || len(msgs) != 1 || !msgs[0].Outgoing {
		t.Fatalf("an explicit outgoing flag was ignored: %+v err=%v", msgs, err)
	}
}

// An unrecognized encoding must produce nothing and say what it saw, rather
// than being guessed at in either direction.
func TestGoogleVoiceSMSUnknownDirectionIsSkippedAndReported(t *testing.T) {
	msgs, stats, err := parseGoogleVoiceSMSAPIListResponseDetailed(
		[]byte(`{"thread":[{"id":"t.+18455550142","item":[{"id":"a","did":"+18455550142","messageText":"hi","type":41}]}]}`), "0")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("an item of unknown direction was delivered anyway: %+v", msgs)
	}
	if stats.Undirected != 1 || len(stats.UnknownTypes) != 1 || stats.UnknownTypes[0] != "41" {
		t.Fatalf("the unrecognized encoding was not reported: %+v", stats)
	}
	note := googleVoiceSMSListenerNote(stats, 0)
	if !strings.Contains(note, "41") || !strings.Contains(note, "skipped") {
		t.Fatalf("the listener note does not explain the skip: %q", note)
	}
}

func TestGoogleVoiceSMSListenerNoteExplainsAnEmptyInbox(t *testing.T) {
	note := googleVoiceSMSListenerNote(googleVoiceSMSAPIStats{}, 0)
	if !strings.Contains(note, "empty") {
		t.Fatalf("an empty inbox was not explained: %q", note)
	}
	note = googleVoiceSMSListenerNote(googleVoiceSMSAPIStats{Threads: 2, Items: 9}, 3)
	if !strings.Contains(note, "3 inbox update(s) observed") {
		t.Fatalf("passively observed updates were not reported: %q", note)
	}
}

// The second, independent loop guard: whatever an item is labelled, FlipAi does
// not answer text it just sent.
func TestGoogleVoiceSMSDoesNotAnswerItsOwnReply(t *testing.T) {
	dir := t.TempDir()
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessSMS}})
	const thread = "/u/0/messages?itemId=t.%2B18455550142"

	rememberGoogleVoiceSMSSent(dir, "+1 (845) 555-0142", "X: here is the answer")
	if !googleVoiceSMSWasSentRecently(dir, "8455550142", "X:   here is   the answer  ") {
		t.Fatal("a just-sent reply was not remembered across whitespace differences")
	}
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550142", thread, "X: here is the answer")); err != nil {
		t.Fatal(err)
	}
	if msgs, err := NewGoogleVoiceSMSClient(dir).readAll(); err != nil || len(msgs) != 0 {
		t.Fatalf("FlipAi spooled its own outgoing reply as a new inbound text: %+v err=%v", msgs, err)
	}

	// A genuinely new text from the same number still gets through.
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550142", thread, "X: a new question")); err != nil {
		t.Fatal(err)
	}
	msgs, err := NewGoogleVoiceSMSClient(dir).readAll()
	if err != nil || len(msgs) != 1 || msgs[0].Body != "X: a new question" {
		t.Fatalf("the loop guard swallowed a real inbound text: %+v err=%v", msgs, err)
	}
}

func TestGoogleVoiceSMSSentMemoryExpires(t *testing.T) {
	dir := t.TempDir()
	rememberGoogleVoiceSMSSent(dir, "8455550142", "old reply")
	ledger := loadGoogleVoiceSMSSentLedger(dir)
	if len(ledger.Sent) != 1 {
		t.Fatalf("the send was not recorded: %+v", ledger)
	}
	ledger.Sent[0].At = time.Now().Add(-2 * googleVoiceSMSSentMemory)
	raw, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(googleVoiceSMSSentPath(dir), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if googleVoiceSMSWasSentRecently(dir, "8455550142", "old reply") {
		t.Fatal("an expired send is still suppressing inbound text")
	}
}

// The ledger only defends the window it exists for. Google accepts a text
// before the send call returns, and both observation paths -- the inbox poll
// and the updates the signed-in page receives by itself -- can read it back in
// that gap. Recording after the send left FlipAi able to answer its own reply,
// which is a paid SMS loop, so the fingerprint is written first.
func TestGoogleVoiceSMSRecordsTheSendBeforeItCanBeObserved(t *testing.T) {
	raw, err := os.ReadFile("google_voice_sms_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	start := strings.Index(body, "func runGoogleVoiceSMSOutboundLoop")
	if start < 0 {
		t.Fatal("the Google Voice SMS outbound loop is gone")
	}
	loop := body[start:]
	if end := strings.Index(loop, "\nfunc "); end > 0 {
		loop = loop[:end]
	}
	recorded := strings.Index(loop, "rememberGoogleVoiceSMSSent(")
	sent := strings.Index(loop, "sendGoogleVoiceTextInPage(")
	if recorded < 0 {
		t.Fatal("outgoing text is no longer recorded, so FlipAi can answer its own reply")
	}
	if sent < 0 {
		t.Fatal("the outbound loop no longer sends")
	}
	if recorded > sent {
		t.Fatal("the outgoing text is recorded after the send: an inbox poll or an observed page update can read the reply back in that window and answer it")
	}
}

// Authorization is keyed on the exact phone number, never on a contact name, so
// a saved contact cannot change who is answered.
func TestGoogleVoiceSMSSentMemoryIsKeyedOnTheNumber(t *testing.T) {
	dir := t.TempDir()
	rememberGoogleVoiceSMSSent(dir, "8455550142", "same words")
	if googleVoiceSMSWasSentRecently(dir, "8455550199", "same words") {
		t.Fatal("the loop guard matched a different phone number")
	}
	if googleVoiceSMSSentFingerprint("Mom", "same words") != "" {
		t.Fatal("a contact name produced a usable fingerprint")
	}
	if _, err := filepath.Abs(googleVoiceSMSSentPath(dir)); err != nil {
		t.Fatal(err)
	}
}
