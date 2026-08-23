package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Structurally faithful to a real Google Voice notification — the leading URL
// line, the SMS, then the YOUR ACCOUNT/legal footer — but every phone number,
// conversation token and DKIM signature is fictional. Numbers use the 555-01xx
// range reserved for examples.
const realVoiceBodyFixture = `<https://voice.google.com>
123456 C: FLIPAI_CODEX_OK
YOUR ACCOUNT <https://voice.google.com> HELP CENTER
<https://support.google.com/voice#topic=1707989> HELP FORUM
<https://productforums.google.com/forum/#!forum/voice>
This email was sent to you because you indicated that you'd like to receive
email notifications for text messages. If you don't want to receive such
emails in the future, please update your email notification settings
<https://voice.google.com/settings#messaging>.
Google LLC
1600 Amphitheatre Pkwy
Mountain View CA 94043 USA`

func realVoiceMessageFixture() GmailMessage {
	return GmailMessage{
		ID:      "real-voice-20260823",
		From:    `"Me (SMS)" <18455550199.18455550142.AbCdEfGhIj@txt.voice.google.com>`,
		ReplyTo: "", // The real notification has no Reply-To header.
		Subject: "New text message from Me (845) 555-0142",
		AuthenticationResults: `mx.google.com;
       dkim=pass header.i=@google.com header.s=20251104 header.b=AAAABBBBCCCC;
       spf=pass smtp.mailfrom=grandcentral.bounces.google.com;
       dmarc=pass header.from=google.com`,
		Body:         realVoiceBodyFixture,
		InternalDate: time.Now(),
	}
}

func TestRealGoogleVoiceNotificationExtractsSMSNotLeadingURL(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.GoogleVoice.AllowedFrom = "8455550142"
	if err := setSecurityCode(&cfg, "123456"); err != nil {
		t.Fatal(err)
	}
	m := realVoiceMessageFixture()
	raw, sender, ok, reason := parseGoogleVoiceBodyDetailed(m, cfg.GoogleVoice.AllowedFrom, "new text message from")
	if !ok {
		t.Fatalf("real Google Voice notification was rejected: %s", reason)
	}
	if sender != "8455550142" {
		t.Fatalf("wrong sender: %q", sender)
	}
	if raw != "123456 C: FLIPAI_CODEX_OK" {
		t.Fatalf("wrong extracted SMS: %q", raw)
	}
	rc, err := parseRemoteCommand(raw, cfg)
	if err != nil {
		t.Fatalf("security/routing parser rejected real SMS: %v", err)
	}
	if rc.Agent != "C" || rc.Text != "FLIPAI_CODEX_OK" {
		t.Fatalf("wrong route: %#v", rc)
	}
}

func TestRealGoogleVoiceNotificationRunsCodexAndRepliesWithoutReplyTo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodAppPassword
	cfg.GoogleVoice.AllowedFrom = "8455550142"
	cfg.GoogleVoice.RequiredSubjectPhrase = "new text message from"
	cfg.CodexPath = os.Args[0]
	if err := setSecurityCode(&cfg, "123456"); err != nil {
		t.Fatal(err)
	}

	c := NewCodexClient(os.Args[0], "")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	fm := &fakeMailClient{msg: realVoiceMessageFixture()}
	stateFile := t.TempDir() + "/state.json"
	b := NewBridge(cfg, stateFile, State{GmailBaselineUnix: time.Now().Add(-time.Minute).Unix()}, fm, c, nil)
	// poll authenticates and enqueues; drainQueue runs the agent turn. In
	// production these are a mailbox loop and a worker goroutine.
	b.poll(ctx)
	b.drainQueue(ctx)

	got, gotTo := fm.joined(), fm.sentTo
	if !strings.Contains(got, "FLIPAI_CODEX_OK") {
		t.Fatalf("real Voice message did not make it through Codex: %q", got)
	}
	// The ack lands first, before the agent has produced anything.
	if texts := fm.sentTexts(); len(texts) < 2 || !strings.Contains(texts[0], "working on it") {
		t.Fatalf("expected an ack text ahead of the result, got %q", texts)
	}
	wantTo := "18455550199.18455550142.AbCdEfGhIj@txt.voice.google.com"
	if gotTo != wantTo {
		t.Fatalf("reply fallback did not use the safe From conversation address: got %q want %q", gotTo, wantTo)
	}

	events := b.activity.Recent(100)
	blob, _ := json.Marshal(events)
	logText := string(blob)
	for _, want := range []string{"New Google Voice candidate detected", "sender verified", "routed to Codex", "Codex command started", "Agent completed successfully", "Reply sent through Google Voice"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("activity log missing %q: %s", want, logText)
		}
	}
	if strings.Contains(logText, "123456") || strings.Contains(logText, "FLIPAI_CODEX_OK") {
		t.Fatalf("activity log leaked SMS/security content: %s", logText)
	}
}
