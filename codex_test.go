package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
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
				fmt.Print(`{"loggedIn":true,"authMethod":"oauth","apiProvider":"firstParty"}`)
				os.Exit(0)
			}
		case "--help":
			fmt.Print("--chrome\n")
			os.Exit(0)
		case "-p":
			fmt.Print(`{"type":"result","subtype":"success","is_error":false,"result":"Claude done.","session_id":"claude_session_test"}`)
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func mockCodexServer() {
	sc := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
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
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"account": map[string]any{"type": "chatgpt"}, "requiresOpenaiAuth": false}})
		case "thread/start":
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thr_test"}}})
		case "turn/start":
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": "turn_test", "status": "inProgress"}}})
			_ = enc.Encode(map[string]any{"method": "item/completed", "params": map[string]any{"turnId": "turn_test", "item": map[string]any{"type": "agentMessage", "text": "Done."}}})
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
	raw, err := c.Account(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" {
		t.Fatal("empty account response")
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

func TestClaudeSubscriptionConnector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
