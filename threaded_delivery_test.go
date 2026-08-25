package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The reported failure was an answer that reached Gmail's Sent folder but never
// reached the phone, because the reply was a standalone email rather than a
// reply to the Google Voice notification. buildThreadedReplyMessage is already
// covered; what was not covered is everything around it — that the bridge
// actually routes ack, progress and every answer part through the threaded
// path, and that each backend puts those headers on the wire.

const voiceNotificationRaw = "Message-ID: <notify-42@mail.gmail.com>\r\n" +
	"References: <earlier-1@mail.gmail.com>\r\n" +
	"From: \"(845) 555-1212\" <18455551212.2125557777.tok@txt.voice.google.com>\r\n" +
	"Reply-To: 18455551212.2125557777.tok@txt.voice.google.com\r\n" +
	"Subject: New text message from (845) 555-1212\r\n" +
	"Authentication-Results: mx.google.com; dkim=pass header.d=google.com\r\n" +
	"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
	"https://voice.google.com\r\n\r\nC: check the build\r\n\r\nYOUR ACCOUNT\r\n"

// threadingMailClient is a MailClient that answers on the threaded path, the
// way the real Gmail and IMAP clients do, and records what it was asked to
// reply to.
type threadingMailClient struct {
	mu         sync.Mutex
	msg        GmailMessage
	replies    []string
	repliedTo  []GmailMessage
	standalone int
}

func (t *threadingMailClient) Authorized() bool                                  { return true }
func (t *threadingMailClient) Test(context.Context) error                        { return nil }
func (t *threadingMailClient) List(context.Context) ([]string, error)            { return []string{t.msg.ID}, nil }
func (t *threadingMailClient) Get(context.Context, string) (GmailMessage, error) { return t.msg, nil }

// SendText is the compatibility fallback. Production clients never take it, so
// a call here means the bridge sent a standalone message — the actual bug.
func (t *threadingMailClient) SendText(_ context.Context, _ string, body string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.standalone++
	t.replies = append(t.replies, body)
	return nil
}

func (t *threadingMailClient) SendReply(_ context.Context, original GmailMessage, body string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.replies = append(t.replies, body)
	t.repliedTo = append(t.repliedTo, original)
	return nil
}

func (t *threadingMailClient) sent() ([]string, []GmailMessage, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.replies...), append([]GmailMessage(nil), t.repliedTo...), t.standalone
}

func notificationFixture(t *testing.T) GmailMessage {
	t.Helper()
	m, err := parseRawGmailMessage("42", []byte(voiceNotificationRaw), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Ack and answer must both be replies to the incoming notification. The user
// received the ack and never the answer, so one threaded message is not enough:
// every message in the turn is checked.
func TestBridgeAnswersOnTheThreadedPath(t *testing.T) {
	m := notificationFixture(t)
	cfg := defaultConfig(t.TempDir())
	cfg.Security.RequireCode = false
	allowTestNumber(&cfg, "C", "2125557777")
	cfg.GoogleVoice.ReplyAck = true

	mc := &threadingMailClient{msg: m}
	b := NewBridge(cfg, t.TempDir()+"/state.json", State{GmailBaselineUnix: 1}, mc, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b.poll(ctx)
	b.drainQueue(ctx)

	bodies, originals, standalone := mc.sent()
	if len(bodies) < 2 {
		t.Fatalf("expected an acknowledgement and a result, got %d: %q", len(bodies), bodies)
	}
	if standalone > 0 {
		t.Fatalf("%d message(s) were sent standalone instead of as a reply: %q", standalone, bodies)
	}
	if len(originals) != len(bodies) {
		t.Fatalf("only %d of %d messages carried the original notification", len(originals), len(bodies))
	}
	for i, original := range originals {
		if original.ID != m.ID || original.ReplyTo != m.ReplyTo {
			t.Errorf("message %d replied to the wrong notification: %#v", i, original)
		}
	}
}

// A long answer is split into numbered texts. Each part is its own email, so
// each one has to be threaded too.
func TestSplitAnswerPartsAreAllThreaded(t *testing.T) {
	m := notificationFixture(t)
	cfg := defaultConfig(t.TempDir())
	cfg.GoogleVoice.ReplyMaxChars = 100
	cfg.GoogleVoice.MaxReplyParts = 4

	mc := &threadingMailClient{msg: m}
	b := NewBridge(cfg, t.TempDir()+"/state.json", State{}, mc, nil, nil)
	b.deliver(context.Background(), m, remoteCommand{Agent: "C", Sender: "2125557777"}, strings.Repeat("long answer text ", 20))

	bodies, originals, standalone := mc.sent()
	if len(bodies) < 2 {
		t.Fatalf("expected a split answer, got %d part(s)", len(bodies))
	}
	if standalone > 0 {
		t.Fatalf("%d part(s) were sent standalone: %q", standalone, bodies)
	}
	if len(originals) != len(bodies) {
		t.Fatalf("only %d of %d parts were threaded", len(originals), len(bodies))
	}
}

// The Gmail API backend must send the reply headers and keep the Gmail thread.
func TestGmailAPIReplyCarriesHeadersAndThreadID(t *testing.T) {
	var mu sync.Mutex
	var raw, threadID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/users/me/messages/42"):
			_, _ = w.Write([]byte(`{"threadId":"thread-9","payload":{"headers":[
				{"name":"Subject","value":"New text message from (845) 555-1212"},
				{"name":"Message-ID","value":"<notify-42@mail.gmail.com>"},
				{"name":"References","value":"<earlier-1@mail.gmail.com>"}]}}`))
		case r.URL.Path == "/users/me/messages/send":
			var payload struct {
				Raw      string `json:"raw"`
				ThreadID string `json:"threadId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			decoded, _ := base64.RawURLEncoding.DecodeString(payload.Raw)
			mu.Lock()
			raw, threadID = string(decoded), payload.ThreadID
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"sent"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := &GmailClient{apiBase: srv.URL, http: srv.Client(), token: oauthToken{AccessToken: "tok", Expiry: time.Now().Add(time.Hour)}}
	if err := g.SendReply(context.Background(), notificationFixture(t), "final answer"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if threadID != "thread-9" {
		t.Errorf("threadId was not sent with the reply: %q", threadID)
	}
	for _, want := range []string{
		"Subject: New text message from (845) 555-1212",
		"In-Reply-To: <notify-42@mail.gmail.com>",
		"References: <earlier-1@mail.gmail.com> <notify-42@mail.gmail.com>",
		"final answer",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("Gmail API payload is missing %q\n---\n%s", want, raw)
		}
	}
}

// Subject and Message-ID come from an incoming email, so a newline in one of
// them must not be able to add a header of its own.
func TestThreadedReplyFlattensUntrustedHeaders(t *testing.T) {
	raw, err := buildThreadedReplyMessage("me@gmail.com", "abc@txt.voice.google.com", replyThreadMeta{
		Subject:   "hi\r\nBcc: attacker@example.com",
		MessageID: "<id@x>\r\nX-Evil: 1",
	}, "text")
	if err != nil {
		t.Fatal(err)
	}
	headers, _, _ := strings.Cut(raw, "\r\n\r\n")
	for _, line := range strings.Split(headers, "\r\n") {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "bcc:") || strings.HasPrefix(lower, "x-evil:") {
			t.Fatalf("an injected header survived:\n%s", headers)
		}
	}
}

// The shipped words have to keep behaving exactly as they always did, in every
// form people actually type.
func TestDefaultCommandWordsStillRoute(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Security.RequireCode = false
	cfg.DefaultAgent = "C"
	cases := []struct {
		raw    string
		agent  string
		text   string
		isNew  bool
		status bool
	}{
		{raw: "C: hello", agent: "C", text: "hello"},
		{raw: "C:hello", agent: "C", text: "hello"},
		// A colon is what marks an agent word on a normal command. Without it
		// the text goes to the default agent unchanged — see
		// TestSpaceSeparatedAgentWordGoesToTheDefaultAgent.
		{raw: "c: hello", agent: "C", text: "hello"},
		{raw: "A: hello", agent: "A", text: "hello"},
		{raw: "plain request", agent: "C", text: "plain request"},
		{raw: "STATUS", status: true},
		{raw: "C NEW", agent: "C", isNew: true},
		{raw: "A: NEW", agent: "A", isNew: true},
	}
	for _, c := range cases {
		// The sending number decides the agent; a case naming an agent is parsed
		// as though it arrived from a number allowed on that agent.
		from := c.agent
		if from == "" {
			from = "C"
		}
		rc, err := parseRemoteCommand(c.raw, cfg, from)
		if err != nil {
			t.Fatalf("%q: %v", c.raw, err)
		}
		if rc.Agent != c.agent || rc.Text != c.text || rc.New != c.isNew || rc.Status != c.status {
			t.Errorf("%q parsed as %#v", c.raw, rc)
		}
	}
}

// Custom words have to survive the security code in front of them.
func TestCustomCommandWordsWorkWithTheSecurityCode(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	for _, agent := range []string{"C", "A"} {
		if err := setAgentCode(&cfg, agent, "482913"); err != nil {
			t.Fatal(err)
		}
		s := agentSettings(cfg, agent)
		s.RequireCode = true
		if agent == "A" {
			cfg.Claude.AgentSettings = s
		} else {
			cfg.Codex.AgentSettings = s
		}
	}
	cfg.DefaultAgent = "A"
	cfg.CodexPrefix, cfg.ClaudePrefix, cfg.NewSessionCommand = "go", "ask", "reset"

	rc, err := parseRemoteCommand("482913 go: check the build", cfg, "C")
	if err != nil || rc.Agent != "C" || rc.Text != "check the build" {
		t.Fatalf("coded Codex command parsed as %#v (%v)", rc, err)
	}
	rc, err = parseRemoteCommand("482913 ASK: summarize", cfg, "A")
	if err != nil || rc.Agent != "A" || rc.Text != "summarize" {
		t.Fatalf("coded Claude command parsed as %#v (%v)", rc, err)
	}
	rc, err = parseRemoteCommand("482913 go reset", cfg, "C")
	if err != nil || rc.Agent != "C" || !rc.New {
		t.Fatalf("coded new-conversation command parsed as %#v (%v)", rc, err)
	}
	if _, err := parseRemoteCommand("go: check the build", cfg, "C"); err == nil {
		t.Fatal("a command without the security code was accepted")
	}
}

// Routing today accepts an agent word without a colon only in front of the
// new-conversation word: "go reset" starts a fresh Codex conversation, while
// "go check the build" is text for the default agent. This pins that split so
// a change to it is a deliberate one — it is the difference between "c u later"
// being a message and being a Codex command.
func TestSpaceSeparatedAgentWordGoesToTheDefaultAgent(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.DefaultAgent = "A"
	cfg.CodexPrefix, cfg.ClaudePrefix, cfg.NewSessionCommand = "go", "ask", "reset"

	rc, err := parseRemoteCommand("go check the build", cfg, "A")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Agent != "A" || rc.Text != "go check the build" {
		t.Fatalf("space form routed as %#v; update this test deliberately if that changes", rc)
	}
	rc, err = parseRemoteCommand("go reset", cfg, "C")
	if err != nil || rc.Agent != "C" || !rc.New {
		t.Fatalf("space form before the new-conversation word parsed as %#v (%v)", rc, err)
	}
}
