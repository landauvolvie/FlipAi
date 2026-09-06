package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGoogleVoiceSMSAPIParsesExactPhoneWithoutContactName(t *testing.T) {
	raw := []byte(`{"thread":[{"id":"t.+18453241813","item":[{"id":"item-in","startTime":"1788667200000","did":"+1 (845) 324-1813","messageText":"G: hi","type":"smsIn"},{"id":"item-out","startTime":"1788667201000","did":"+18453241813","messageText":"reply","type":"smsOut"}]}]}`)
	msgs, err := parseGoogleVoiceSMSAPIListResponse(raw, "0")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages: %+v", len(msgs), msgs)
	}
	if msgs[0].Sender != "8453241813" || msgs[0].Thread != "/u/0/messages?itemId=t.%2B18453241813" || msgs[0].Body != "G: hi" || msgs[0].Outgoing {
		t.Fatalf("wrong inbound identity: %+v", msgs[0])
	}
	if !msgs[1].Outgoing {
		t.Fatalf("smsOut was not marked outgoing: %+v", msgs[1])
	}
}

// A real inbound text carries a "did" that is this account's own Google Voice
// number, not the person texting. Reading it as the sender made every genuine
// text disagree with its own conversation, and the identity cross-check then
// blocked all of them: "conversation phone does not match sender". The
// conversation's own t.+1 identity is the sender; the did is only recorded.
func TestGoogleVoiceSMSAPISenderIsTheConversationNotTheAccountNumber(t *testing.T) {
	raw := []byte(`{"thread":[{"id":"t.+18453241813","item":[{"id":"in","startTime":"1788667200000","did":"+18456043655","messageText":"X: hi","type":"smsIn"}]}]}`)
	msgs, err := parseGoogleVoiceSMSAPIListResponse(raw, "2")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("parse: %v %+v", err, msgs)
	}
	if msgs[0].Sender != "8453241813" {
		t.Fatalf("the sender was not taken from the conversation: %+v", msgs[0])
	}
	if msgs[0].AccountNumber != "8456043655" {
		t.Fatalf("this account's own Google Voice number was not recorded separately: %+v", msgs[0])
	}
	if googleVoiceSMSThreadPhone(msgs[0].Thread) != "8453241813" {
		t.Fatalf("the reply target drifted from the sender: %+v", msgs[0])
	}
}

func TestGoogleVoiceSMSAPITargetRequiresExactThreadPhone(t *testing.T) {
	got, err := googleVoiceSMSAPITarget("8453241813", "/u/0/messages?itemId=t.%2B18453241813", true)
	if err != nil || got != "t.+18453241813" {
		t.Fatalf("target=%q err=%v", got, err)
	}
	if _, err := googleVoiceSMSAPITarget("8456043655", "/u/0/messages?itemId=t.%2B18453241813", true); err == nil {
		t.Fatal("wrong-phone exact thread was accepted")
	}
	got, err = googleVoiceSMSAPITarget("8453241813", "", false)
	if err != nil || got != "t.+18453241813" {
		t.Fatalf("new-message target=%q err=%v", got, err)
	}
}

func TestGoogleVoiceSMSAPISendBodyEscapesMessage(t *testing.T) {
	body, err := googleVoiceSMSAPISendBody("t.+18453241813", "line 1\n\"quoted\"", 12345)
	if err != nil {
		t.Fatal(err)
	}
	var v []any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	if len(v) != 9 || v[4] != "line 1\n\"quoted\"" || v[5] != "t.+18453241813" {
		t.Fatalf("wrong send payload: %s", body)
	}
}

func TestGoogleVoiceSMSAPIRejectsServiceError(t *testing.T) {
	_, err := parseGoogleVoiceSMSAPIListResponse([]byte(`{"error":{"message":"denied"}}`), "0")
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("service error not returned: %v", err)
	}
}
