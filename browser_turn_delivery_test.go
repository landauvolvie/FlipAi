package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// This is the "still working…" that never ended. An empty reply is what a
// failed text turn produces, and it was read as "an image is still rendering",
// so every timed-out browser turn fell into the wait for media that was never
// coming -- on plain text questions with no image anywhere in them.
func TestFailedTextTurnIsNotMistakenForAPendingImage(t *testing.T) {
	if browserChatReplySuggestsPendingImage("") {
		t.Fatal("an empty reply is still read as a pending image, so every failed text turn waits for media")
	}

	turnErr := errors.New("ChatGPT started answering but did not finish within 90 seconds.")
	start := time.Now()
	reply, err := finishBrowserGeneratedImageTurn(context.Background(), "hi whats your name?", "", turnErr)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a failed text turn waited %v for an image that was never requested", elapsed)
	}
	if err == nil {
		t.Fatal("a failed text turn reported success")
	}
	if !strings.Contains(err.Error(), "90 seconds") {
		t.Fatalf("the real failure was replaced with something else: %v", err)
	}
	if reply != "" {
		t.Fatalf("unexpected reply %q", reply)
	}
}

// A turn that did ask for an image and produced no text yet is still an image
// turn: that case must keep waiting for the media.
func TestRequestedImageWithNoTextStillWaitsForMedia(t *testing.T) {
	if !browserChatPromptRequestsGeneratedImage("make me a picture of a dog") {
		t.Fatal("an explicit image request is no longer recognized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, _ = finishBrowserGeneratedImageTurn(ctx, "make me a picture of a dog", "",
		errors.New("ChatGPT started answering but did not finish within 90 seconds."))
	if time.Since(start) < time.Second {
		t.Fatal("an image turn stopped waiting for its media immediately")
	}
}

// The host gave up on the worker at 100 seconds, but the worker's own budget
// for one /chat request runs to about 140 seconds. A turn that used its full
// page checkpoint was abandoned by the host while the worker was still
// finishing, with the model's answer already complete in the browser.
func TestTurnRequestBudgetOutlastsTheWorkersOwnBudget(t *testing.T) {
	const workerWorstCase = 25*time.Second + 95*time.Second + 20*time.Second
	if browserChatTurnRequestBudget <= workerWorstCase {
		t.Fatalf("request budget %v does not outlast the worker's own %v, so the host abandons finished answers",
			browserChatTurnRequestBudget, workerWorstCase)
	}
	for _, file := range []string{
		"chatgpt_webview.go", "claude_chat_webview.go", "gemini_chat_webview.go",
		"grok_chat_webview.go", "copilot_chat_webview.go", "muse_chat_webview.go",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "Timeout: 100 * time.Second") || strings.Contains(string(raw), "Timeout:100*time.Second") {
			t.Fatalf("%s still gives up on the worker after 100 seconds", file)
		}
	}
}

// When the host does hit its own deadline the model is usually still
// answering, so the answer has to be collected rather than thrown away.
func TestHostTimeoutCollectsTheAnswerInsteadOfFailing(t *testing.T) {
	for _, err := range []error{
		context.DeadlineExceeded,
		fmt.Errorf(`Post "http://127.0.0.1:58403/chat": %w`, context.DeadlineExceeded),
		errors.New(`Post "http://127.0.0.1:58403/chat": context deadline exceeded`),
		errors.New(`Get "http://127.0.0.1:1/chat": net/http: request canceled (Client.Timeout exceeded while awaiting headers)`),
	} {
		if !browserChatTurnRequestTimedOut(err) {
			t.Fatalf("a host-side deadline was not recognized: %v", err)
		}
	}
	for _, err := range []error{
		nil,
		errors.New("Gemini Chat is not signed in inside FlipAi"),
		errors.New("connection refused"),
	} {
		if browserChatTurnRequestTimedOut(err) {
			t.Fatalf("a real failure was mistaken for a host-side deadline: %v", err)
		}
	}

	for _, file := range []string{
		"sms_sticky_chatgpt.go", "sms_claude_chat.go", "sms_gemini_chat.go",
		"sms_grok_chat.go", "sms_copilot_chat.go", "sms_muse_chat.go",
	} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "browserChatTurnRequestTimedOut") {
			t.Fatalf("%s still discards the answer when the host hits its own deadline", file)
		}
	}
}

// "M" means Microsoft Copilot in the route table and Gemini in the legacy alias
// table. A number not allowed on the agent the sender named was being answered
// by whichever other agent happened to share the letter and be allowed, so
// "m: ..." was silently answered by Gemini.
func TestExplicitRouteIsNeverSilentlyAnsweredByAnotherAgent(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	// This phone reaches Codex, Claude, ChatGPT, Claude Chat, Gemini, Grok and
	// Muse -- every agent except Microsoft Copilot.
	const reaches = "CAGHMXU"

	route, err := selectStickySMSRoute("m: hi whats your name?", cfg, reaches, "")
	if err == nil {
		t.Fatalf("a message addressed to Microsoft Copilot was answered by %s (%s)", route.Display, route.Agent)
	}
	if !strings.Contains(err.Error(), agentDisplayName("P")) {
		t.Fatalf("the refusal does not name the agent the sender asked for: %v", err)
	}

	// A number that *is* allowed on Copilot still reaches it.
	route, err = selectStickySMSRoute("m: hi whats your name?", cfg, reaches+"P", "")
	if err != nil {
		t.Fatalf("an allowed number could not reach Microsoft Copilot: %v", err)
	}
	if route.Agent != "P" {
		t.Fatalf("m: reached %s (%s), want Microsoft Copilot Chat", route.Display, route.Agent)
	}

	// The legacy redirect inside one vendor's family still works: "A:" names
	// Claude Chat in the route table but used to name Claude Code Local, so a
	// number allowed only on Claude Code Local still reaches it.
	route, err = selectStickySMSRoute("A: hello Claude", cfg, "CA", "")
	if err != nil || route.Agent != "A" {
		t.Fatalf("a number allowed only on Claude Code Local lost its legacy A: route: agent=%q err=%v", route.Agent, err)
	}

	// Routes the number is allowed on are unaffected.
	for prefix, wantAgent := range map[string]string{
		"g: hello":  "M", // Gemini Chat
		"a: hello":  "H", // Claude Chat
		"x: hello":  "X", // Grok Chat
		"mu: hello": "U", // Muse
		"o: hello":  "G", // ChatGPT Chat
	} {
		route, err := selectStickySMSRoute(prefix, cfg, reaches, "")
		if err != nil {
			t.Fatalf("%q was refused: %v", prefix, err)
		}
		if route.Agent != wantAgent {
			t.Fatalf("%q reached agent %q (%s), want %q", prefix, route.Agent, route.Display, wantAgent)
		}
	}
}
