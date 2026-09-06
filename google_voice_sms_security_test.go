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

	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550199", "/u/0/messages/blocked", "X: hi")); err != nil {
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
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessVoice}})
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550142", "/u/0/messages/calls-only", "X: hi")); err != nil {
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

func TestDirectGoogleVoiceSMSReplyBoundToOriginalPhoneAndThread(t *testing.T) {
	dir := t.TempDir()
	writeDirectSMSTestConfig(t, dir, []AgentPhone{{Number: "8455550142", Access: AccessSMS}})
	if err := appendDirectGoogleVoiceSMS(dir, directSMSPayload(t, "8455550142", "/u/2/messages/contact-123", "X: hi")); err != nil {
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
	phone, thread, err := client.directReplyIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	if phone != "8455550142" || thread != "/u/2/messages/contact-123" {
		t.Fatalf("reply identity drifted: phone=%q thread=%q", phone, thread)
	}

	forged := original
	forged.ReplyTo = "flipai.8455550199.direct@txt.voice.google.com"
	if _, _, err := client.directReplyIdentity(forged); err == nil {
		t.Fatal("mismatched reply phone was accepted")
	}
}

func TestNormalizeGoogleVoiceSMSThreadRejectsOtherSites(t *testing.T) {
	if got := normalizeGoogleVoiceSMSThread("https://voice.google.com/u/3/messages/abc?x=1"); got != "/u/3/messages/abc" {
		t.Fatalf("unexpected normalized thread %q", got)
	}
	for _, bad := range []string{
		"https://example.com/u/0/messages/abc",
		"javascript:alert(1)",
		"/u/0/calls/abc",
		"/u/0/messages/../calls/abc",
	} {
		if got := normalizeGoogleVoiceSMSThread(bad); got != "" {
			t.Fatalf("unsafe thread %q normalized to %q", bad, got)
		}
	}
}
