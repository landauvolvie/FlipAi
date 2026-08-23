package main

import (
	"context"
	"net"
	"sync"
	"testing"
)

// countingDial wraps a dialer so a test can assert how many TCP/TLS
// connections a sequence of operations actually cost.
func countingDial(inner func(context.Context) (net.Conn, error), n *int, mu *sync.Mutex) func(context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		mu.Lock()
		*n++
		mu.Unlock()
		return inner(ctx)
	}
}

// A poll used to dial, handshake, LOGIN and EXAMINE once for List and again for
// Get. Both now share one authenticated session.
func TestListAndGetShareOneIMAPConnection(t *testing.T) {
	raw := voiceFixture("482913 STATUS")
	var mu sync.Mutex
	dials := 0

	c := &IMAPMailClient{cfg: GmailConfig{SubjectPhrase: "new text message from"}, email: "u@gmail.com", password: "pw"}
	c.dialIMAP = countingDial(fakeIMAPDial(raw), &dials, &mu)
	defer c.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.List(ctx); err != nil {
			t.Fatalf("List %d: %v", i, err)
		}
		if _, err := c.Get(ctx, "42"); err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
	}

	mu.Lock()
	got := dials
	mu.Unlock()
	if got != 1 {
		t.Fatalf("three List+Get rounds cost %d connections, want 1", got)
	}
}

// A dropped connection must be transparently replaced, exactly once.
func TestBrokenIMAPSessionReconnectsOnce(t *testing.T) {
	raw := voiceFixture("482913 STATUS")
	var mu sync.Mutex
	dials := 0

	c := &IMAPMailClient{cfg: GmailConfig{SubjectPhrase: "new text message from"}, email: "u@gmail.com", password: "pw"}
	c.dialIMAP = countingDial(fakeIMAPDial(raw), &dials, &mu)
	defer c.Close()

	ctx := context.Background()
	if _, err := c.List(ctx); err != nil {
		t.Fatalf("first List: %v", err)
	}

	// Kill the pooled connection behind the client's back, the way an idle
	// timeout on Gmail's side would.
	c.sessMu.Lock()
	if c.sess == nil {
		c.sessMu.Unlock()
		t.Fatal("expected a pooled session after the first List")
	}
	_ = c.sess.conn.Close()
	c.sessMu.Unlock()

	if _, err := c.List(ctx); err != nil {
		t.Fatalf("List after a dropped connection should recover: %v", err)
	}

	mu.Lock()
	got := dials
	mu.Unlock()
	if got != 2 {
		t.Fatalf("recovery cost %d connections, want 2 (original + one reconnect)", got)
	}
}

// Ack, progress and result texts reuse one authenticated SMTP session.
func TestSendTextReusesTheSMTPConnection(t *testing.T) {
	var capMu sync.Mutex
	var captured string
	var mu sync.Mutex
	dials := 0

	c := &IMAPMailClient{email: "u@gmail.com", password: "pw", smtpServerName: "localhost"}
	c.dialSMTP = countingDial(fakeSMTPDial(&captured, &capMu), &dials, &mu)
	defer c.Close()

	to := "18455551234.2125557777.tok@txt.voice.google.com"
	for _, body := range []string{"✓ Codex working on it…", "Codex still working…", "done"} {
		if err := c.SendText(context.Background(), to, body); err != nil {
			t.Fatalf("SendText %q: %v", body, err)
		}
	}

	mu.Lock()
	got := dials
	mu.Unlock()
	if got != 1 {
		t.Fatalf("three texts cost %d SMTP connections, want 1", got)
	}

	capMu.Lock()
	last := captured
	capMu.Unlock()
	if last == "" {
		t.Fatal("fake SMTP server captured no message")
	}
}

// Reuse must never weaken the destination check.
func TestPooledSendTextStillRefusesNonVoiceAddresses(t *testing.T) {
	var capMu sync.Mutex
	var captured string
	c := &IMAPMailClient{email: "u@gmail.com", password: "pw", smtpServerName: "localhost"}
	c.dialSMTP = fakeSMTPDial(&captured, &capMu)
	defer c.Close()

	if err := c.SendText(context.Background(), "attacker@example.com", "hello"); err == nil {
		t.Fatal("reply to a non-Google-Voice address was allowed")
	}
	capMu.Lock()
	defer capMu.Unlock()
	if captured != "" {
		t.Fatalf("a message was transmitted despite the rejected address: %q", captured)
	}
}
