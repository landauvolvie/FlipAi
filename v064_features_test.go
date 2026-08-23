package main

import (
    "context"
    "encoding/json"
    "os"
    "testing"
    "time"
)

func TestSecurityCodeCanBeDisabledWithoutWeakeningRouting(t *testing.T) {
    cfg := defaultConfig(t.TempDir())
    cfg.Security.RequireCode = false
    cfg.DefaultAgent = "C"

    tests := []struct {
        raw, agent, text string
        status, newThread bool
    }{
        {raw: "C: hello", agent: "C", text: "hello"},
        {raw: "A: hello", agent: "A", text: "hello"},
        {raw: "plain default request", agent: "C", text: "plain default request"},
        {raw: "STATUS", status: true},
        {raw: "C NEW", agent: "C", newThread: true},
        {raw: "A: NEW", agent: "A", newThread: true},
    }
    for _, tc := range tests {
        rc, err := parseRemoteCommand(tc.raw, cfg)
        if err != nil { t.Fatalf("%q: %v", tc.raw, err) }
        if rc.Agent != tc.agent || rc.Text != tc.text || rc.Status != tc.status || rc.New != tc.newThread {
            t.Fatalf("%q parsed as %#v", tc.raw, rc)
        }
    }
}

func TestSecurityCodeStillEnforcedWhenEnabled(t *testing.T) {
    cfg := defaultConfig(t.TempDir())
    if !cfg.Security.RequireCode { t.Fatal("security code must default to enabled") }
    if err := setSecurityCode(&cfg, "482913"); err != nil { t.Fatal(err) }
    if _, err := parseRemoteCommand("C: hello", cfg); err == nil {
        t.Fatal("message without required code was accepted")
    }
    rc, err := parseRemoteCommand("482913 A: hello", cfg)
    if err != nil || rc.Agent != "A" || rc.Text != "hello" {
        t.Fatalf("required-code route failed: %#v %v", rc, err)
    }
}

func TestCodexFullWindowsUserAccessProtocolShapes(t *testing.T) {
    thread := applyCodexRequestDefaults("thread/start", map[string]any{"cwd": `C:\\Users\\User`}).(map[string]any)
    if thread["approvalPolicy"] != "never" || thread["sandbox"] != "danger-full-access" || thread["ephemeral"] != false {
        t.Fatalf("wrong thread access defaults: %#v", thread)
    }
    turn := applyCodexRequestDefaults("turn/start", map[string]any{"threadId": "thr"}).(map[string]any)
    sp, _ := turn["sandboxPolicy"].(map[string]any)
    if turn["approvalPolicy"] != "never" || sp["type"] != "dangerFullAccess" {
        t.Fatalf("wrong turn access defaults: %#v", turn)
    }
    smoke := applyCodexRequestDefaults("thread/start", map[string]any{"ephemeral": true}).(map[string]any)
    if smoke["ephemeral"] != true { t.Fatal("explicit ephemeral smoke-test setting was overwritten") }
}

func TestCodexFullAccessAndThreadHandoffEndToEnd(t *testing.T) {
    t.Setenv("FLIPAI_TEST_REQUIRE_FULL_ACCESS", "1")
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    c := NewCodexClient(os.Args[0], "")
    if err := c.Start(ctx); err != nil { t.Fatal(err) }
    defer c.Close()

    raw, err := c.Request(ctx, "thread/start", map[string]any{"modelProvider": "openai"})
    if err != nil { t.Fatal(err) }
    var started struct { Thread struct { ID string `json:"id"` } `json:"thread"` }
    if json.Unmarshal(raw, &started) != nil || started.Thread.ID == "" { t.Fatal("missing test thread id") }
    if c.threadSubscribed(started.Thread.ID) {
        t.Fatal("FlipAi kept a newly created persisted thread subscribed instead of handing it back to Desktop")
    }

    raw, err = c.Request(ctx, "turn/start", map[string]any{
        "threadId": started.Thread.ID,
        "input": []map[string]any{{"type": "text", "text": "FLIPAI_CODEX_OK"}},
    })
    if err != nil { t.Fatal(err) }
    var turn struct { Turn struct { ID string `json:"id"` } `json:"turn"` }
    if json.Unmarshal(raw, &turn) != nil || turn.Turn.ID == "" { t.Fatal("missing turn id") }

    deadline := time.Now().Add(3 * time.Second)
    completed := false
    for time.Now().Before(deadline) {
        select {
        case n := <-c.notifications:
            if n.Method == "turn/completed" { completed = true }
        default:
        }
        if completed && !c.threadSubscribed(started.Thread.ID) { return }
        time.Sleep(20 * time.Millisecond)
    }
    t.Fatalf("thread was not released after completed turn; completed=%v subscribed=%v", completed, c.threadSubscribed(started.Thread.ID))
}
