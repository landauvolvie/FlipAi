package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeDirectSMSTestConfig(t *testing.T, dir string, phones []AgentPhone) Config {
	t.Helper()
	cfg := defaultConfig(dir)
	cfg.Gmail.Method = GmailMethodGoogleVoice
	cfg.GrokChat.AgentSettings.Phones = phones
	cfg.GoogleVoice.AllowedFrom = smsAllowedFrom(cfg)
	if err := saveConfig(filepath.Join(dir, "bridge.json"), cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func directSMSPayload(t *testing.T, sender, thread, body string) string {
	t.Helper()
	b, err := json.Marshal(directGoogleVoiceSMS{Sender: sender, Thread: thread, Body: body, At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func activityHas(events []ActivityEvent, sender, contains string) bool {
	for _, e := range events {
		if (sender == "" || e.Sender == sender) && strings.Contains(e.Message, contains) {
			return true
		}
	}
	return false
}

func TestDirectGoogleVoiceSMSBlocksUnauthorizedNumberBeforeSpool(t *testing.T) {
	dir := t.TempDir()
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessSMS}})

	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550199", "/u/0/messages?itemId=t.%2B18455550199", "X: hi")); err != nil {
		t.Fatal(err)
	}
	client := NewGoogleVoiceSMSClient(dir)
	msgs, err := client.readAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("unauthorized SMS reached trusted spool: %+v", msgs)
	}
	events := directGoogleVoiceActivity(dir).Recent(20)
	if !activityHas(events, "8455550199", "Google Voice SMS received") {
		t.Fatalf("inbound blocked SMS was not logged as received: %+v", events)
	}
	if !activityHas(events, "8455550199", "Blocked Google Voice SMS: phone number is not allowed; no reply sent") {
		t.Fatalf("blocked number did not get an explicit no-reply Activity event: %+v", events)
	}
}

func TestDirectGoogleVoiceSMSBlocksCallsOnlyNumber(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig(dir)
	cfg.Gmail.Method = GmailMethodGoogleVoice
	cfg.Security.AgentsMigrated = true
	cfg.Codex.Phones = []AgentPhone{{Number: "8455550142", Access: AccessVoice}}
	cfg.GoogleVoice.AllowedFrom = smsAllowedFrom(cfg)
	if err := saveConfig(filepath.Join(dir, "bridge.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550142", "/u/0/messages?itemId=t.%2B18455550142", "C: hi")); err != nil {
		t.Fatal(err)
	}
	msgs, err := NewGoogleVoiceSMSClient(dir).readAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("calls-only number reached SMS spool: %+v", msgs)
	}
	if !activityHas(directGoogleVoiceActivity(dir).Recent(20), "8455550142", "allowed for calls only; no reply sent") {
		t.Fatal("calls-only inbound SMS did not receive an explicit blocked Activity event")
	}
}

func TestDirectGoogleVoiceSMSBlocksUnresolvedIdentity(t *testing.T) {
	dir := t.TempDir()
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessSMS}})
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "US Mobile", "/u/0/messages/contact", "X: hi")); err != nil {
		t.Fatal(err)
	}
	if !activityHas(directGoogleVoiceActivity(dir).Recent(20), "", "exact sender phone number could not be resolved; no reply sent") {
		t.Fatal("unresolved contact identity was not logged and blocked")
	}
	msgs, err := NewGoogleVoiceSMSClient(dir).readAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatal("contact name without an exact phone number reached the spool")
	}
}

func TestDirectGoogleVoiceSMSCanRecoverSenderOnlyFromTrustedItemID(t *testing.T) {
	dir := t.TempDir()
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessSMS}})
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "US Mobile", "/u/0/messages?itemId=t.%2B18455550142", "X: hi")); err != nil {
		t.Fatal(err)
	}
	msgs, err := NewGoogleVoiceSMSClient(dir).readAll()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("trusted itemId phone did not recover sender: msgs=%+v err=%v", msgs, err)
	}
	if msgs[0].Sender != "8455550142" {
		t.Fatalf("wrong sender recovered from itemId: %+v", msgs[0])
	}
}

// The conversation decides, so accompanying sender metadata cannot be used to
// borrow an allowed number and get an unrelated conversation answered. The
// reply would go to the conversation, so the conversation is what is authorized.
func TestDirectGoogleVoiceSMSIgnoresAClaimedSenderAndAuthorizesTheConversation(t *testing.T) {
	dir := t.TempDir()
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessSMS}})
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550142", "/u/0/messages?itemId=t.%2B18455550199", "X: hi")); err != nil {
		t.Fatal(err)
	}
	if msgs, err := NewGoogleVoiceSMSClient(dir).readAll(); err != nil || len(msgs) != 0 {
		t.Fatalf("an allowed number claimed alongside an unallowed conversation reached the spool: msgs=%+v err=%v", msgs, err)
	}
	if !activityHas(directGoogleVoiceActivity(dir).Recent(20), "8455550199", "phone number is not allowed; no reply sent") {
		t.Fatal("the unallowed conversation was not blocked on its own number")
	}
}

// A conversation with no phone-number identity of its own -- a group thread, or
// an older locator -- can neither be attributed nor replied to.
func TestDirectGoogleVoiceSMSBlocksAConversationWithNoPhoneIdentity(t *testing.T) {
	dir := t.TempDir()
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessSMS}})
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550142", "/u/0/messages/legacy-thread", "X: hi")); err != nil {
		t.Fatal(err)
	}
	if msgs, err := NewGoogleVoiceSMSClient(dir).readAll(); err != nil || len(msgs) != 0 {
		t.Fatalf("a conversation without an exact phone identity reached the spool: msgs=%+v err=%v", msgs, err)
	}
	if !activityHas(directGoogleVoiceActivity(dir).Recent(20), "8455550142", "no exact phone-number identity; no reply sent") {
		t.Fatal("a conversation without a phone identity was not explicitly blocked")
	}
}

// The number the user typed and the number Google reports arrive in different
// shapes. Every shape of the same number has to be the same number.
func TestDirectGoogleVoiceSMSAcceptsEveryShapeOfTheSameNumber(t *testing.T) {
	for _, typed := range []string{"+18453241813", "8453241813", "845-324-1813", "(845) 324-1813", "845 324 1813", "1-845-324-1813", "+1 (845) 324-1813"} {
		t.Run(typed, func(t *testing.T) {
			dir := t.TempDir()
			cfg := defaultConfig(dir)
			cfg.Gmail.Method = GmailMethodGoogleVoice
			cfg.Security.AgentsMigrated = true
			cfg.GeminiChat.AgentSettings.Phones = []AgentPhone{{Number: normalizeUSPhone(typed), Access: AccessSMS}}
			cfg.GoogleVoice.AllowedFrom = typed
			if err := saveConfig(filepath.Join(dir, "bridge.json"), cfg); err != nil {
				t.Fatal(err)
			}
			if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "", "/u/0/messages?itemId=t.%2B18453241813", "G: hi")); err != nil {
				t.Fatal(err)
			}
			msgs, err := NewGoogleVoiceSMSClient(dir).readAll()
			if err != nil || len(msgs) != 1 {
				t.Fatalf("%q did not authorize its own number: msgs=%+v err=%v events=%+v", typed, msgs, err, directGoogleVoiceActivity(dir).Recent(20))
			}
			if msgs[0].Sender != "8453241813" {
				t.Fatalf("%q resolved to the wrong sender: %+v", typed, msgs[0])
			}
		})
	}
}

func TestDirectGoogleVoiceSMSReplyBoundToOriginalPhoneAndThread(t *testing.T) {
	dir := t.TempDir()
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessSMS}})
	thread := "/u/2/messages?itemId=t.%2B18455550142"
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550142", thread, "X: hi")); err != nil {
		t.Fatal(err)
	}
	client := NewGoogleVoiceSMSClient(dir)
	msgs, err := client.readAll()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("expected one authorized message, got %+v err=%v", msgs, err)
	}
	original, err := client.Get(context.Background(), msgs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	phone, gotThread, err := client.directReplyIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	if phone != "8455550142" || gotThread != thread {
		t.Fatalf("reply identity drifted: phone=%q thread=%q", phone, gotThread)
	}

	forged := original
	forged.ReplyTo = "flipai.8455550199.direct@txt.voice.google.com"
	if _, _, err := client.directReplyIdentity(forged); err == nil {
		t.Fatal("mismatched reply phone was accepted")
	}
}

func TestNormalizeGoogleVoiceSMSThreadCurrentItemID(t *testing.T) {
	const want = "/u/3/messages?itemId=t.%2B18455550142"
	for _, raw := range []string{
		want,
		"https://voice.google.com/u/3/messages?itemId=t.%2B18455550142",
		"https://voice.google.com/u/3/messages/?itemId=t.%2B18455550142",
	} {
		if got := normalizeGoogleVoiceSMSThread(raw); got != want {
			t.Fatalf("normalizeGoogleVoiceSMSThread(%q)=%q, want %q", raw, got, want)
		}
	}
	if got := googleVoiceSMSThreadPhone(want); got != "8455550142" {
		t.Fatalf("itemId phone=%q, want 8455550142", got)
	}
}

func TestNormalizeGoogleVoiceSMSThreadRejectsUnsafeTargets(t *testing.T) {
	for _, bad := range []string{
		"https://example.com/u/0/messages?itemId=t.%2B18455550142",
		"http://voice.google.com/u/0/messages?itemId=t.%2B18455550142",
		"javascript:alert(1)",
		"/u/0/calls/abc",
		"/u/0/messages/../calls/abc",
		"/u/0/messages?itemId=t.%2B1845555014",
		"/u/0/messages?itemId=draft-123",
		"/u/0/messages?itemId=t.%2B18455550142&x=1",
		"/u/0/messages/abc?x=1",
	} {
		if got := normalizeGoogleVoiceSMSThread(bad); got != "" {
			t.Fatalf("unsafe thread %q normalized to %q", bad, got)
		}
	}
	if got := normalizeGoogleVoiceSMSThread("/u/2/messages/legacy-thread"); got != "/u/2/messages/legacy-thread" {
		t.Fatalf("legacy trusted thread no longer parses: %q", got)
	}
}
