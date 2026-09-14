package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// activityMessages returns every Activity line recorded under a state path,
// oldest first, so a test can assert on what the user would actually read.
func activityMessages(t *testing.T, statePath string) []string {
	t.Helper()
	events := activityLogForStatePath(statePath).Recent(200)
	out := make([]string, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		out = append(out, events[i].Message)
	}
	return out
}

func containsSubstring(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// waitingVoiceClient is the real-world startup shape: the Google Voice browser
// is still restoring its saved session, so the transport test fails for the
// first few tries and then succeeds.
type waitingVoiceClient struct {
	failures int
	tests    int
}

func (w *waitingVoiceClient) Authorized() bool { return true }
func (w *waitingVoiceClient) Test(context.Context) error {
	w.tests++
	if w.tests <= w.failures {
		return errors.New("Waiting for Google Voice background connection")
	}
	return nil
}
func (w *waitingVoiceClient) List(context.Context) ([]string, error) { return nil, nil }
func (w *waitingVoiceClient) Get(context.Context, string) (GmailMessage, error) {
	return GmailMessage{}, errors.New("no message")
}
func (w *waitingVoiceClient) SendText(context.Context, string, string) error { return nil }

func shortenBridgeStartPacing(t *testing.T) {
	t.Helper()
	retry, grace := bridgeStartRetryDelay, bridgeStartGraceBeforeWarning
	bridgeStartRetryDelay = time.Millisecond
	bridgeStartGraceBeforeWarning = time.Hour
	t.Cleanup(func() {
		bridgeStartRetryDelay, bridgeStartGraceBeforeWarning = retry, grace
	})
}

// The regression this locks down: FlipAi asked the Google Voice browser once,
// at boot, whether it was connected. That browser restores its saved session
// alongside every agent browser, so on a cold machine the answer was routinely
// "not yet" -- and FlipAi gave up for the whole run. The listener came up a
// minute later and kept receiving and authorizing texts that no bridge existed
// to deliver, so every message was detected and then silently never answered.
func TestBridgeStartRetriesUntilTheGoogleVoiceBrowserIsReady(t *testing.T) {
	shortenBridgeStartPacing(t)
	statePath := t.TempDir() + "/state.json"
	mc := &waitingVoiceClient{failures: 3}
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodGoogleVoice

	started := 0
	retryUntilBridgeStarts(context.Background(), activityLogForStatePath(statePath), func(ctx context.Context) error {
		if err := bridgeTransportGate(ctx, cfg, mc); err != nil {
			return err
		}
		started++
		return nil
	})

	if started != 1 {
		t.Fatalf("SMS processing started %d times, want exactly 1", started)
	}
	if mc.tests != 4 {
		t.Fatalf("the transport was tested %d times, want 4 (3 not-ready answers, then ready)", mc.tests)
	}
	lines := activityMessages(t, statePath)
	if !containsSubstring(lines, "Waiting to start SMS processing") {
		t.Fatalf("the wait was never explained in the Activity log: %v", lines)
	}
	if !containsSubstring(lines, "SMS processing started") {
		t.Fatalf("a late start was never reported in the Activity log: %v", lines)
	}
}

// A healthy install must not pay for the retry: the first attempt starts SMS
// processing and nothing is written about waiting.
func TestBridgeStartSaysNothingWhenTheTransportIsReadyImmediately(t *testing.T) {
	shortenBridgeStartPacing(t)
	statePath := t.TempDir() + "/state.json"
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.Method = GmailMethodGoogleVoice

	retryUntilBridgeStarts(context.Background(), activityLogForStatePath(statePath), func(ctx context.Context) error {
		return bridgeTransportGate(ctx, cfg, &waitingVoiceClient{})
	})

	if lines := activityMessages(t, statePath); len(lines) != 0 {
		t.Fatalf("a clean start wrote Activity noise: %v", lines)
	}
}

// Shutdown has to end the retry, or quitting FlipAi would hang on a transport
// that is never coming back.
func TestBridgeStartRetryStopsWhenTheAppShutsDown(t *testing.T) {
	shortenBridgeStartPacing(t)
	statePath := t.TempDir() + "/state.json"
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		retryUntilBridgeStarts(ctx, activityLogForStatePath(statePath), func(context.Context) error {
			return errors.New("Waiting for Google Voice background connection")
		})
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge start supervisor kept retrying after shutdown")
	}
}

// A transport that never arrives must say so in the Activity log rather than
// leaving the user with a silent install and no explanation.
func TestBridgeStartWarnsWhenTheTransportNeverArrives(t *testing.T) {
	shortenBridgeStartPacing(t)
	bridgeStartGraceBeforeWarning = 0
	statePath := t.TempDir() + "/state.json"
	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	retryUntilBridgeStarts(ctx, activityLogForStatePath(statePath), func(context.Context) error {
		attempts++
		if attempts >= 3 {
			cancel()
		}
		return errors.New("Google Voice SMS connection test failed: Waiting for Google Voice background connection")
	})

	lines := activityMessages(t, statePath)
	if !containsSubstring(lines, "SMS processing did not start") {
		t.Fatalf("a transport that never arrived was never called out: %v", lines)
	}
}

// The gate is what decides whether SMS can flow at all, so each refusal has to
// name a cause the user can act on.
func TestBridgeTransportGateNamesWhyItIsNotReady(t *testing.T) {
	base := defaultConfig(t.TempDir())

	notSelected := base
	notSelected.Gmail.Method = GmailMethodAppPassword
	if err := bridgeTransportGate(context.Background(), notSelected, &waitingVoiceClient{}); err == nil ||
		!strings.Contains(err.Error(), "Google Voice SMS is not connected") {
		t.Fatalf("an unselected transport was not reported: %v", err)
	}

	voice := base
	voice.Gmail.Method = GmailMethodGoogleVoice
	if err := bridgeTransportGate(context.Background(), voice, nil); err == nil ||
		!strings.Contains(err.Error(), "background connection is not ready") {
		t.Fatalf("a missing transport was not reported: %v", err)
	}

	if err := bridgeTransportGate(context.Background(), voice, &waitingVoiceClient{failures: 1}); err == nil ||
		!strings.Contains(err.Error(), "Google Voice SMS connection test failed") {
		t.Fatalf("a failing transport test was not reported: %v", err)
	}

	if err := bridgeTransportGate(context.Background(), voice, &waitingVoiceClient{}); err != nil {
		t.Fatalf("a ready transport was refused: %v", err)
	}
}

// Starting SMS processing must be supervised, not attempted once. A single
// attempt at boot is the bug: it raced the Google Voice browser's session
// restore and lost, leaving texts detected but never handed to any agent.
func TestHostSupervisesBridgeStartInsteadOfTryingOnce(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.Contains(src, "app.superviseBridgeStart(ctx)") {
		t.Fatal("the host no longer supervises bridge startup")
	}
	if strings.Contains(src, "app.startBridge(ctx)") {
		t.Fatal("the host went back to a single unsupervised bridge start attempt")
	}
}

// Connecting Google Voice writes the choice to bridge.json while the host is
// already running. The host kept the transport it booted with, so a first
// connection carried no text at all until FlipAi was restarted by hand.
func TestBridgeStartAdoptsAGoogleVoiceConnectionMadeAfterStartup(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bridge.json")
	cfg := defaultConfig(dir)
	cfg.Gmail.Method = ""
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	// The host as it exists before the user connects anything.
	app := &App{dataDir: dir, configPath: cfgPath, statePath: filepath.Join(dir, "state.json"), tokenPath: filepath.Join(dir, "token.json"), cfg: cfg}
	if err := bridgeTransportGate(context.Background(), app.cfg, app.mail); err == nil {
		t.Fatal("an unconnected host reported a usable transport")
	}

	// The Connections page connects Google Voice: bridge.json changes on disk.
	connected := cfg
	connected.Gmail.Method = GmailMethodGoogleVoice
	if err := saveConfig(cfgPath, connected); err != nil {
		t.Fatal(err)
	}

	app.adoptConnectedVoiceTransport()

	if app.cfg.Gmail.Method != GmailMethodGoogleVoice {
		t.Fatalf("the running host ignored the connection, method is %q", app.cfg.Gmail.Method)
	}
	if app.mail == nil || !app.mail.Authorized() {
		t.Fatal("the running host did not build the Google Voice transport it was just given")
	}
}
