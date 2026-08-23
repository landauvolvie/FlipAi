package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSplitReplyKeepsShortAnswerIntact(t *testing.T) {
	got := splitReply("all done", 300, 4)
	if len(got) != 1 || got[0] != "all done" {
		t.Fatalf("short answer was altered: %q", got)
	}
}

// The old behaviour cut the answer at ReplyMaxChars and appended an ellipsis,
// losing the end of any desktop-length reply.
func TestSplitReplyNumbersPartsAndLosesNothing(t *testing.T) {
	words := make([]string, 120)
	for i := range words {
		words[i] = fmt.Sprintf("word%03d", i)
	}
	full := strings.Join(words, " ")

	parts := splitReply(full, 200, 10)
	if len(parts) < 2 {
		t.Fatalf("long answer was not split: %d part(s)", len(parts))
	}

	var rebuilt []string
	for i, p := range parts {
		wantPrefix := fmt.Sprintf("%d/%d ", i+1, len(parts))
		if !strings.HasPrefix(p, wantPrefix) {
			t.Fatalf("part %d missing %q prefix: %q", i, wantPrefix, p)
		}
		if len([]rune(p)) > 200 {
			t.Fatalf("part %d exceeds the per-text limit: %d runes", i, len([]rune(p)))
		}
		rebuilt = append(rebuilt, strings.TrimPrefix(p, wantPrefix))
	}
	if strings.Join(rebuilt, " ") != full {
		t.Fatalf("splitting lost or reordered content:\n got %q\nwant %q", strings.Join(rebuilt, " "), full)
	}
}

// Beyond MaxReplyParts the tail is marked rather than silently vanishing.
func TestSplitReplyMarksTruncationAtThePartCap(t *testing.T) {
	full := strings.Repeat("alpha beta gamma delta ", 200)
	parts := splitReply(full, 200, 2)
	if len(parts) != 2 {
		t.Fatalf("part cap not honoured: %d parts", len(parts))
	}
	if !strings.HasSuffix(parts[1], "…") {
		t.Fatalf("dropped tail was not marked: %q", parts[1])
	}
}

func TestSplitReplyEmptyProducesNothing(t *testing.T) {
	if got := splitReply("   ", 300, 4); len(got) != 0 {
		t.Fatalf("expected no texts for an empty answer, got %q", got)
	}
}

// queueMailClient serves a different message on each Get so a second SMS can
// arrive while the first is still running.
type queueMailClient struct {
	mu    sync.Mutex
	msgs  map[string]GmailMessage
	ids   []string
	sent  []string
	onGet func(id string)
}

func (q *queueMailClient) Authorized() bool           { return true }
func (q *queueMailClient) Test(context.Context) error { return nil }
func (q *queueMailClient) List(context.Context) ([]string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.ids...), nil
}
func (q *queueMailClient) Get(_ context.Context, id string) (GmailMessage, error) {
	if q.onGet != nil {
		q.onGet(id)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.msgs[id], nil
}
func (q *queueMailClient) SendText(_ context.Context, _ string, body string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sent = append(q.sent, body)
	return nil
}
func (q *queueMailClient) texts() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.sent...)
}

func voiceMessage(id, body string) GmailMessage {
	return GmailMessage{
		ID:                    id,
		From:                  `Owner (SMS) <18455551234.2125557777.` + id + `@txt.voice.google.com>`,
		ReplyTo:               "18455551234.2125557777." + id + "@txt.voice.google.com",
		Subject:               "New text message from Owner",
		AuthenticationResults: "mx.google.com; dkim=pass header.d=google.com",
		Body:                  body,
		InternalDate:          time.Now(),
	}
}

// Two texts arriving in one poll must both run. Previously execute returned
// early when busy, which would discard the second now that jobs are queued.
func TestBothQueuedCommandsRunInOrder(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodAppPassword
	cfg.GoogleVoice.AllowedFrom = "2125557777"
	cfg.GoogleVoice.ProgressUpdates = false
	if err := setSecurityCode(&cfg, "482913"); err != nil {
		t.Fatal(err)
	}

	qm := &queueMailClient{
		msgs: map[string]GmailMessage{
			"m1": voiceMessage("m1", "482913 STATUS"),
			"m2": voiceMessage("m2", "482913 A NEW"),
		},
		// List returns newest first; poll walks it oldest-first.
		ids: []string{"m2", "m1"},
	}

	b := NewBridge(cfg, t.TempDir()+"/state.json",
		State{GmailBaselineUnix: time.Now().Add(-time.Minute).Unix()}, qm, nil, nil)

	ctx := context.Background()
	b.poll(ctx)
	b.drainQueue(ctx)

	joined := strings.Join(qm.texts(), "\n")
	if !strings.Contains(joined, "Bridge online") {
		t.Fatalf("STATUS never answered: %q", joined)
	}
	if !strings.Contains(joined, "New Claude conversation started") {
		t.Fatalf("queued second command never ran: %q", joined)
	}
}

// STATUS needs no agent, so it is answered inline and must not wait behind a
// running turn.
func TestStatusIsAnsweredWithoutQueueing(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodAppPassword
	cfg.GoogleVoice.AllowedFrom = "2125557777"
	if err := setSecurityCode(&cfg, "482913"); err != nil {
		t.Fatal(err)
	}
	qm := &queueMailClient{
		msgs: map[string]GmailMessage{"s1": voiceMessage("s1", "482913 STATUS")},
		ids:  []string{"s1"},
	}
	b := NewBridge(cfg, t.TempDir()+"/state.json",
		State{GmailBaselineUnix: time.Now().Add(-time.Minute).Unix()}, qm, nil, nil)

	b.poll(context.Background())

	// Answered by poll itself: nothing was enqueued and no drain was needed.
	b.mu.Lock()
	depth := len(b.queue)
	b.mu.Unlock()
	if depth != 0 {
		t.Fatalf("STATUS was queued instead of answered inline (depth %d)", depth)
	}
	if joined := strings.Join(qm.texts(), "\n"); !strings.Contains(joined, "Bridge online") {
		t.Fatalf("STATUS not answered by poll: %q", joined)
	}
}

// The map index must agree with the slice it replaced, including after the
// 2000-entry trim drops the oldest ids.
func TestProcessedIndexMatchesCheckpointSlice(t *testing.T) {
	b := NewBridge(defaultConfig(t.TempDir()), t.TempDir()+"/state.json", State{}, nil, nil, nil)

	for i := 0; i < 2500; i++ {
		b.markProcessed(fmt.Sprintf("id-%d", i))
	}
	if got := len(b.state.ProcessedMessageIDs); got != 2000 {
		t.Fatalf("checkpoint slice not trimmed to 2000: %d", got)
	}
	if got := len(b.processedSet); got != 2000 {
		t.Fatalf("index drifted from the slice: %d entries", got)
	}
	// Oldest entries were dropped by the trim; newest must still be present.
	if b.processed("id-0") {
		t.Fatal("trimmed id still reported as processed")
	}
	if !b.processed("id-2499") {
		t.Fatal("most recent id was not recorded")
	}
	for _, id := range b.state.ProcessedMessageIDs {
		if !b.processed(id) {
			t.Fatalf("slice entry %q missing from the index", id)
		}
	}
}

// Re-checkpointing the same id must not grow the slice.
func TestMarkProcessedIsIdempotent(t *testing.T) {
	b := NewBridge(defaultConfig(t.TempDir()), t.TempDir()+"/state.json", State{}, nil, nil, nil)
	b.markProcessed("dup")
	b.markProcessed("dup")
	if got := len(b.state.ProcessedMessageIDs); got != 1 {
		t.Fatalf("duplicate checkpoint recorded %d times", got)
	}
}
