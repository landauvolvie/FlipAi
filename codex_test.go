package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "app-server":
			mockCodexServer()
			os.Exit(0)
		case "auth":
			if len(os.Args) > 2 && os.Args[2] == "status" {
				if os.Getenv("FLIPAI_TEST_CLAUDE_STATUS_FALSE") == "1" {
					fmt.Print(`{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}`)
					os.Exit(1)
				}
				if os.Getenv("FLIPAI_TEST_CLAUDE_API_BILLING") == "1" {
					fmt.Print(`{"loggedIn":true,"authMethod":"api_key","apiProvider":"firstParty"}`)
					os.Exit(0)
				}
				fmt.Print(`{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","subscriptionType":"pro"}`)
				os.Exit(0)
			}
		case "--help":
			fmt.Print("--chrome\n")
			os.Exit(0)
		case "-p":
			if os.Getenv("FLIPAI_TEST_CLAUDE_PRINT_FAIL") == "1" {
				fmt.Fprint(os.Stderr, "Not logged in. Please run /login")
				os.Exit(1)
			}
			result := "Claude done."
			if len(os.Args) > 2 && strings.Contains(os.Args[2], "FLIPAI_CLAUDE_OK") {
				result = "FLIPAI_CLAUDE_OK"
			}
			b, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": result, "session_id": "claude_session_test"})
			fmt.Print(string(b))
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func mockCodexServer() {
	sc := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	// Model the two Codex persistence rules that matter here:
	//   1. ephemeral threads never get a rollout and can never be resumed;
	//   2. a new durable thread does not have a resumable rollout until its first
	//      turn gives it content. Releasing an empty durable thread and then
	//      resuming it is the exact C NEW failure this mock must catch.
	ephemeral := map[string]bool{}
	hasRollout := map[string]bool{}
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		method, _ := m["method"].(string)
		id, hasID := m["id"]
		if !hasID {
			continue
		}
		switch method {
		case "initialize":
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"codexHome": "test"}})
		case "account/read":
			// This is the important current Codex shape: a user can be signed into
			// ChatGPT while requiresOpenaiAuth is true. That flag describes the
			// selected provider; it does not mean the user is signed out.
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"account": map[string]any{"type": "chatgpt", "email": "test@example.com", "planType": "pro"}, "requiresOpenaiAuth": true}})
		case "thread/start":
			if os.Getenv("FLIPAI_TEST_REQUIRE_FULL_ACCESS") == "1" {
				p, _ := m["params"].(map[string]any)
				if p["approvalPolicy"] != "never" || p["sandbox"] != "danger-full-access" || p["ephemeral"] != false {
					_ = enc.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32001, "message": "missing full user access/durable thread settings"}})
					continue
				}
			}
			{
				p, _ := m["params"].(map[string]any)
				isEph, _ := p["ephemeral"].(bool)
				ephemeral["thr_test"] = isEph
				hasRollout["thr_test"] = false
			}
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thr_test", "ephemeral": true, "modelProvider": "openai"}, "modelProvider": "openai"}})
		case "thread/resume":
			p, _ := m["params"].(map[string]any)
			tid, _ := p["threadId"].(string)
			if ephemeral[tid] || !hasRollout[tid] {
				_ = enc.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32600, "message": "no rollout found for thread id " + tid}})
				continue
			}
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{}})
		case "turn/start":
			if os.Getenv("FLIPAI_TEST_REQUIRE_FULL_ACCESS") == "1" {
				p, _ := m["params"].(map[string]any)
				sp, _ := p["sandboxPolicy"].(map[string]any)
				if p["approvalPolicy"] != "never" || sp["type"] != "dangerFullAccess" {
					_ = enc.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32002, "message": "missing full user access turn settings"}})
					continue
				}
			}
			p, _ := m["params"].(map[string]any)
			tid, _ := p["threadId"].(string)
			if tid != "" && !ephemeral[tid] {
				hasRollout[tid] = true
			}
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": "turn_test", "status": "inProgress"}}})
			params, _ := json.Marshal(m["params"])
			reply := "Done."
			if strings.Contains(string(params), "FLIPAI_CODEX_OK") {
				reply = "FLIPAI_CODEX_OK"
			}
			_ = enc.Encode(map[string]any{"method": "item/completed", "params": map[string]any{"turnId": "turn_test", "item": map[string]any{"type": "agentMessage", "text": reply}}})
			_ = enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "turn_test", "status": "completed"}}})
		default:
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{}})
		}
	}
}

func TestCodexAppServerHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := NewCodexClient(os.Args[0], "")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, err := c.Account(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !codexAccountIsChatGPT(raw) {
		t.Fatalf("ChatGPT account with requiresOpenaiAuth=true was incorrectly rejected: %s", raw)
	}
	raw, err = c.Request(ctx, "thread/start", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var tr struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	_ = json.Unmarshal(raw, &tr)
	if tr.Thread.ID != "thr_test" {
		t.Fatalf("thread %q", tr.Thread.ID)
	}
	raw, err = c.Request(ctx, "turn/start", map[string]any{"threadId": "thr_test", "input": []map[string]any{{"type": "text", "text": "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("empty turn response")
	}
	select {
	case n := <-c.notifications:
		if n.Method != "item/completed" {
			t.Fatalf("unexpected notification %s", n.Method)
		}
	case <-ctx.Done():
		t.Fatal(fmt.Errorf("notification timeout: %w", ctx.Err()))
	}
}

func TestCodexEphemeralSmokeTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := NewCodexClient(os.Args[0], "")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SmokeTest(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeSubscriptionConnector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := NewClaudeClient(os.Args[0], "", ClaudeConfig{PermissionMode: "acceptEdits", UseChrome: true})
	if err := c.Test(ctx); err != nil {
		t.Fatal(err)
	}
	res, sid, err := c.Run(ctx, "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if res != "Claude done." || sid != "claude_session_test" {
		t.Fatalf("result=%q sid=%q", res, sid)
	}
}

func TestClaudeStatusFalseExitOneStillTriesRealFirstPartyRequest(t *testing.T) {
	t.Setenv("FLIPAI_TEST_CLAUDE_STATUS_FALSE", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := NewClaudeClient(os.Args[0], "", ClaudeConfig{PermissionMode: "acceptEdits"})
	if err := c.Test(ctx); err != nil {
		t.Fatalf("real Claude request should override false/inconclusive auth status: %v", err)
	}
	res, _, err := c.Run(ctx, "", "hello")
	if err != nil {
		t.Fatalf("Run should use the working first-party background path: %v", err)
	}
	if res != "Claude done." {
		t.Fatalf("result=%q", res)
	}
}

func TestClaudeStatusFalseAndRealRequestFailureExplainsCLILogin(t *testing.T) {
	t.Setenv("FLIPAI_TEST_CLAUDE_STATUS_FALSE", "1")
	t.Setenv("FLIPAI_TEST_CLAUDE_PRINT_FAIL", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := NewClaudeClient(os.Args[0], "", ClaudeConfig{PermissionMode: "acceptEdits"})
	err := c.Test(ctx)
	if err == nil {
		t.Fatal("expected Claude test failure")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "desktop") || !strings.Contains(msg, "cli") || !strings.Contains(msg, "/login") {
		t.Fatalf("failure should explain separate Desktop/CLI login and remediation: %v", err)
	}
}

func TestClaudeExplicitAPIBillingIsRejected(t *testing.T) {
	t.Setenv("FLIPAI_TEST_CLAUDE_API_BILLING", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := NewClaudeClient(os.Args[0], "", ClaudeConfig{})
	err := c.Test(ctx)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "api") {
		t.Fatalf("expected API billing rejection, got %v", err)
	}
}

func TestAuthEnvironmentScrubbing(t *testing.T) {
	codexEnv := scrubOpenAIEnv([]string{"PATH=x", "OPENAI_API_KEY=secret", "openai_base_url=http://example", "CODEX_HOME=C:/Users/User/.codex"})
	joined := strings.ToUpper(strings.Join(codexEnv, "\n"))
	if strings.Contains(joined, "OPENAI_API_KEY=") || strings.Contains(joined, "OPENAI_BASE_URL=") {
		t.Fatalf("OpenAI API environment leaked into Codex: %v", codexEnv)
	}
	if !strings.Contains(joined, "CODEX_HOME=") {
		t.Fatalf("CODEX_HOME should be preserved: %v", codexEnv)
	}

	claudeEnv := scrubAnthropicEnv([]string{"PATH=x", "ANTHROPIC_API_KEY=secret", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_OAUTH_TOKEN=subscription-token"})
	joined = strings.ToUpper(strings.Join(claudeEnv, "\n"))
	if strings.Contains(joined, "ANTHROPIC_API_KEY=") || strings.Contains(joined, "CLAUDE_CODE_USE_BEDROCK=") {
		t.Fatalf("Claude API/provider environment leaked: %v", claudeEnv)
	}
	if !strings.Contains(joined, "CLAUDE_CODE_OAUTH_TOKEN=") {
		t.Fatalf("Claude subscription OAuth token should be preserved: %v", claudeEnv)
	}
}

// Ephemeral threads never persist a rollout, so they must remain subscribed for
// their whole lifetime and must never be resumed.
func TestEphemeralThreadIsNeitherReleasedNorResumed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := NewCodexClient(os.Args[0], "")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Request(ctx, "thread/start", map[string]any{"ephemeral": true}); err != nil {
		t.Fatalf("ephemeral thread/start: %v", err)
	}
	if !c.threadIsEphemeral("thr_test") {
		t.Fatal("ephemeral thread was not recorded as ephemeral")
	}
	if !c.threadSubscribed("thr_test") {
		t.Fatal("ephemeral thread was released for desktop handoff; it has no rollout to hand over")
	}
	// Would fail with "no rollout found" if a resume were attempted.
	if _, err := c.Request(ctx, "turn/start", map[string]any{"threadId": "thr_test", "input": []map[string]any{{"type": "text", "text": "hi"}}}); err != nil {
		t.Fatalf("turn on an ephemeral thread must not resume it: %v", err)
	}
}

func TestFreshDurableThreadWaitsForFirstTurnBeforeHandoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := NewCodexClient(os.Args[0], "")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Request(ctx, "thread/start", map[string]any{}); err != nil {
		t.Fatalf("durable thread/start: %v", err)
	}
	if c.threadIsEphemeral("thr_test") {
		t.Fatal("durable thread was misclassified as ephemeral")
	}
	// A brand-new durable thread has no rollout yet. It must stay subscribed
	// until its first turn instead of being handed off and immediately resumed.
	if !c.threadSubscribed("thr_test") {
		t.Fatal("fresh durable thread was released before its first turn")
	}
	if _, err := c.Request(ctx, "turn/start", map[string]any{"threadId": "thr_test", "input": []map[string]any{{"type": "text", "text": "hi"}}}); err != nil {
		t.Fatalf("first durable thread turn should run without a resume: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for c.threadSubscribed("thr_test") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if c.threadSubscribed("thr_test") {
		t.Fatal("durable thread was not handed off after its first completed turn")
	}

	// After the first turn created a rollout and the thread was handed off,
	// the next turn should transparently resume the persisted conversation.
	if _, err := c.Request(ctx, "turn/start", map[string]any{"threadId": "thr_test", "input": []map[string]any{{"type": "text", "text": "again"}}}); err != nil {
		t.Fatalf("persisted durable thread should resume cleanly: %v", err)
	}
}
