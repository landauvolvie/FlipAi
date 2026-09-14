package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// failingGetMailClient lists one message that can never be read, which is what
// a Google Voice MMS marker whose media has vanished from the conversation
// looks like to the bridge.
type failingGetMailClient struct {
	gets int
}

func (f *failingGetMailClient) Authorized() bool           { return true }
func (f *failingGetMailClient) Test(context.Context) error { return nil }
func (f *failingGetMailClient) List(context.Context) ([]string, error) {
	return []string{"stuck-1"}, nil
}
func (f *failingGetMailClient) Get(context.Context, string) (GmailMessage, error) {
	f.gets++
	return GmailMessage{}, errors.New("read Google Voice MMS attachment: No MMS media element was found next to the received message.")
}
func (f *failingGetMailClient) SendText(context.Context, string, string) error { return nil }

// An unreadable message used to be re-fetched on every poll -- once a second,
// for as long as FlipAi ran -- writing the same error line thousands of times.
// One real export carried 5361 copies of a single message's failure, which is
// what buried every other event in the log.
func TestUnreadableMessageIsReportedOnceAndThenGivenUpOn(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodGoogleVoice
	statePath := t.TempDir() + "/state.json"
	fm := &failingGetMailClient{}
	b := NewBridge(cfg, statePath, State{}, fm, nil, nil)

	for i := 0; i < 20; i++ {
		b.poll(context.Background())
	}
	if fm.gets != 20 {
		t.Fatalf("the message was fetched %d times over 20 polls, want 20", fm.gets)
	}
	errorLines := 0
	for _, line := range activityMessages(t, statePath) {
		if strings.Contains(line, "Could not read matching Google Voice message") {
			errorLines++
		}
	}
	if errorLines != 1 {
		t.Fatalf("20 failed reads of one message wrote %d Activity lines, want 1", errorLines)
	}
	if len(b.state.ProcessedMessageIDs) != 0 {
		t.Fatal("a message that might still become readable was given up on immediately")
	}

	// Once it has been failing for longer than the give-up window, FlipAi stops
	// retrying it and says so, instead of looping on it forever.
	b.mu.Lock()
	b.unreadableSince["stuck-1"] = time.Now().Add(-unreadableMessageGiveUp - time.Second)
	b.mu.Unlock()
	b.poll(context.Background())

	if len(b.state.ProcessedMessageIDs) != 1 {
		t.Fatalf("a permanently unreadable message was not checkpointed: %v", b.state.ProcessedMessageIDs)
	}
	if !containsSubstring(activityMessages(t, statePath), "stopped retrying it") {
		t.Fatalf("giving up was never reported: %v", activityMessages(t, statePath))
	}

	before := fm.gets
	b.poll(context.Background())
	if fm.gets != before {
		t.Fatal("a given-up message was fetched again")
	}
}

// A message that reads cleanly after a hiccup must not carry its failure
// history forward, or a later unrelated failure could be given up on early.
func TestReadableMessageClearsItsRetryHistory(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	b := NewBridge(cfg, t.TempDir()+"/state.json", State{}, &failingGetMailClient{}, nil, nil)

	if b.noteUnreadable("blip-1", errors.New("temporary")) {
		t.Fatal("the first failed read gave up immediately")
	}
	b.noteReadable("blip-1")

	b.mu.Lock()
	_, tracked := b.unreadableSince["blip-1"]
	b.mu.Unlock()
	if tracked {
		t.Fatal("a message that read cleanly is still counted as unreadable")
	}
}
