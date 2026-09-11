from pathlib import Path


def replace_once(path: str, old: str, new: str, label: str) -> None:
    p = Path(path)
    s = p.read_text()
    if new in s:
        return
    if old not in s:
        raise SystemExit(f"missing expected {path} block: {label}")
    p.write_text(s.replace(old, new, 1))


p = Path("bridge.go")
s = p.read_text()

if "func (b *Bridge) dispatchQueuedJob" not in s:
    def repl(old: str, new: str, label: str) -> None:
        global s
        if old not in s:
            raise SystemExit(f"missing expected bridge.go block: {label}")
        s = s.replace(old, new, 1)

    repl("""\tactivity *ActivityLog
\tmu       sync.Mutex
\trunMu    sync.Mutex
\tstate    State
\tbusy     bool
\trunCtx   context.Context
""", """\tactivity *ActivityLog
\tmu       sync.Mutex
\t// Browser/model turns are isolated by provider. agentTail preserves FIFO
\t// ordering within one provider while allowing unrelated providers to run
\t// concurrently; agentRunMu is a defensive same-provider execution lock.
\tagentTail       map[string]chan struct{}
\tagentRunMu      map[string]*sync.Mutex
\tpendingByAgent  map[string]int
\tactiveTurns     int
\tstate           State
\tbusy            bool
\trunCtx          context.Context
""", "Bridge concurrency fields")

    repl("""\t\tactivity: activityLogForStatePath(statePath),
\t\tqueueSig: make(chan struct{}, 1),
\t\tpaused:   cfg.Paused,
""", """\t\tactivity:       activityLogForStatePath(statePath),
\t\tqueueSig:        make(chan struct{}, 1),
\t\tagentTail:       make(map[string]chan struct{}),
\t\tagentRunMu:      make(map[string]*sync.Mutex),
\t\tpendingByAgent:  make(map[string]int),
\t\tpaused:          cfg.Paused,
""", "NewBridge initialization")

    repl("""func (b *Bridge) enqueue(j bridgeJob) int {
\tb.mu.Lock()
\tb.queue = append(b.queue, j)
\tdepth := len(b.queue)
\tb.mu.Unlock()
\tselect {
\tcase b.queueSig <- struct{}{}:
\tdefault:
\t}
\treturn depth
}
""", """func bridgeAgentKey(agent string) string {
\tkey := strings.ToUpper(strings.TrimSpace(agent))
\tif key == "" {
\t\treturn "C"
\t}
\treturn key
}

func (b *Bridge) enqueue(j bridgeJob) int {
\tb.mu.Lock()
\tb.queue = append(b.queue, j)
\tif b.pendingByAgent == nil {
\t\tb.pendingByAgent = make(map[string]int)
\t}
\tkey := bridgeAgentKey(j.cmd.Agent)
\tb.pendingByAgent[key]++
\tdepth := b.pendingByAgent[key]
\tb.mu.Unlock()
\tselect {
\tcase b.queueSig <- struct{}{}:
\tdefault:
\t}
\treturn depth
}

func (b *Bridge) finishQueuedAgent(agent string) {
\tb.mu.Lock()
\tdefer b.mu.Unlock()
\tkey := bridgeAgentKey(agent)
\tif b.pendingByAgent[key] <= 1 {
\t\tdelete(b.pendingByAgent, key)
\t\treturn
\t}
\tb.pendingByAgent[key]--
}

func (b *Bridge) agentMutex(agent string) *sync.Mutex {
\tb.mu.Lock()
\tdefer b.mu.Unlock()
\tif b.agentRunMu == nil {
\t\tb.agentRunMu = make(map[string]*sync.Mutex)
\t}
\tkey := bridgeAgentKey(agent)
\tm := b.agentRunMu[key]
\tif m == nil {
\t\tm = &sync.Mutex{}
\t\tb.agentRunMu[key] = m
\t}
\treturn m
}
""", "provider-local enqueue")

    repl("""func (b *Bridge) drainQueue(ctx context.Context) {
\tfor ctx.Err() == nil {
\t\tj, ok := b.dequeue()
\t\tif !ok {
\t\t\treturn
\t\t}
\t\tb.execute(ctx, j.msg, j.cmd)
\t\tif j.done != nil {
\t\t\tclose(j.done)
\t\t}
\t}
}
""", """func (b *Bridge) drainQueue(ctx context.Context) {
\tfor ctx.Err() == nil {
\t\tj, ok := b.dequeue()
\t\tif !ok {
\t\t\treturn
\t\t}
\t\tb.execute(ctx, j.msg, j.cmd)
\t\tb.finishQueuedAgent(j.cmd.Agent)
\t\tif j.done != nil {
\t\t\tclose(j.done)
\t\t}
\t}
}

// dispatchQueuedJob chains jobs for the same provider in FIFO order without
// making a stalled provider block unrelated models. Different providers have
// independent predecessor gates and therefore run concurrently.
func (b *Bridge) dispatchQueuedJob(ctx context.Context, j bridgeJob) {
\tkey := bridgeAgentKey(j.cmd.Agent)
\tgate := make(chan struct{})
\tb.mu.Lock()
\tif b.agentTail == nil {
\t\tb.agentTail = make(map[string]chan struct{})
\t}
\tprev := b.agentTail[key]
\tb.agentTail[key] = gate
\tb.mu.Unlock()

\tgo func() {
\t\tdefer func() {
\t\t\tb.finishQueuedAgent(j.cmd.Agent)
\t\t\tif j.done != nil {
\t\t\t\tclose(j.done)
\t\t\t}
\t\t\tclose(gate)
\t\t\tb.mu.Lock()
\t\t\tif b.agentTail[key] == gate {
\t\t\t\tdelete(b.agentTail, key)
\t\t\t}
\t\t\tb.mu.Unlock()
\t\t}()
\t\tif prev != nil {
\t\t\tselect {
\t\t\tcase <-prev:
\t\t\tcase <-ctx.Done():
\t\t\t\treturn
\t\t\t}
\t\t}
\t\tif ctx.Err() != nil {
\t\t\treturn
\t\t}
\t\tb.execute(ctx, j.msg, j.cmd)
\t}()
}

func (b *Bridge) dispatchQueue(ctx context.Context) {
\tfor ctx.Err() == nil {
\t\tj, ok := b.dequeue()
\t\tif !ok {
\t\t\treturn
\t\t}
\t\tb.dispatchQueuedJob(ctx, j)
\t}
}
""", "provider dispatch queue")

    repl("""\t\t\tcase <-b.queueSig:
\t\t\t\tb.drainQueue(ctx)
""", """\t\t\tcase <-b.queueSig:
\t\t\t\tb.dispatchQueue(ctx)
""", "Run dispatcher")

    repl("""\tif step := b.progress; step != "" {
\t\tline += " Now: " + truncate(step, 120)
\t}
""", "", "status progress detail")

    repl("""func (b *Bridge) execute(parent context.Context, m GmailMessage, rc remoteCommand) {
\t// One agent turn at a time, enforced structurally. The old busy flag
\t// returned early instead of waiting, which would now silently discard a
\t// queued job rather than merely delaying it.
\tb.runMu.Lock()
\tdefer b.runMu.Unlock()
\tstartedAt := time.Now()
\tb.mu.Lock()
\tb.busy = true
\tb.progress = ""
""", """func (b *Bridge) execute(parent context.Context, m GmailMessage, rc remoteCommand) {
\t// Same-provider turns remain serialized even if a caller bypasses the live
\t// queue. Unrelated providers use different mutexes and cannot block each other.
\trunMu := b.agentMutex(rc.Agent)
\trunMu.Lock()
\tdefer runMu.Unlock()
\tstartedAt := time.Now()
\tb.mu.Lock()
\tb.activeTurns++
\tb.busy = true
\tb.progress = ""
""", "per-provider execute lock")

    repl("""\tdefer func() { b.mu.Lock(); b.busy = false; b.progress = ""; b.mu.Unlock() }()
""", """\tdefer func() {
\t\tb.mu.Lock()
\t\tif b.activeTurns > 0 {
\t\t\tb.activeTurns--
\t\t}
\t\tb.busy = b.activeTurns > 0
\t\tb.progress = ""
\t\tb.mu.Unlock()
\t}()
""", "aggregate busy cleanup")

    repl("""\t\tcase <-t.C:
\t\t\tline := agentName + " still working…"
\t\t\tif step := b.currentProgress(); step != "" {
\t\t\t\tline += " " + truncate(step, 120)
\t\t\t}
\t\t\tb.notify(ctx, m, line)
""", """\t\tcase <-t.C:
\t\t\t// Never forward thought/tool/intermediate UI text to the phone.
\t\t\tline := agentName + " still working…"
\t\t\tb.notify(ctx, m, line)
""", "generic heartbeat")

    repl("""\t\t\t\t\tif p.Item.Type == "agentMessage" {
\t\t\t\t\t\tfinal = p.Item.Text
\t\t\t\t\t} else {
\t\t\t\t\t\t// Any other completed item is a step worth naming in a
\t\t\t\t\t\t// progress heartbeat.
\t\t\t\t\t\tb.setProgress(p.Item.Text)
\t\t\t\t\t}
""", """\t\t\t\t\tif p.Item.Type == "agentMessage" {
\t\t\t\t\t\tfinal = p.Item.Text
\t\t\t\t\t}
""", "Codex intermediate progress")

    p.write_text(s)

Path("bridge_provider_isolation_test.go").write_text(r'''package main

import (
    "os"
    "strings"
    "testing"
)

func TestBridgeQueueDepthIsProviderLocal(t *testing.T) {
    b := NewBridge(Config{}, "", State{}, nil, nil, nil)
    if got := b.enqueue(bridgeJob{cmd: remoteCommand{Agent: "X"}}); got != 1 {
        t.Fatalf("first Grok depth = %d, want 1", got)
    }
    if got := b.enqueue(bridgeJob{cmd: remoteCommand{Agent: "G"}}); got != 1 {
        t.Fatalf("first ChatGPT depth = %d, want 1; another provider must not count as ahead", got)
    }
    if got := b.enqueue(bridgeJob{cmd: remoteCommand{Agent: "X"}}); got != 2 {
        t.Fatalf("second Grok depth = %d, want 2", got)
    }
    if b.agentMutex("X") == b.agentMutex("G") {
        t.Fatal("different providers unexpectedly share one execution mutex")
    }
    if b.agentMutex("G") != b.agentMutex("G") {
        t.Fatal("same provider did not reuse its execution mutex")
    }
}

func TestLiveBridgeUsesProviderIsolatedDispatcher(t *testing.T) {
    raw, err := os.ReadFile("bridge.go")
    if err != nil {
        t.Fatal(err)
    }
    s := string(raw)
    for _, want := range []string{"b.dispatchQueue(ctx)", "b.dispatchQueuedJob(ctx, j)", "b.agentMutex(rc.Agent)", "pendingByAgent"} {
        if !strings.Contains(s, want) {
            t.Fatalf("bridge.go lost provider-isolation marker %q", want)
        }
    }
    for _, forbidden := range []string{"b.runMu.Lock()", "line += \" \" + truncate(step"} {
        if strings.Contains(s, forbidden) {
            t.Fatalf("bridge.go still contains global/intermediate behavior %q", forbidden)
        }
    }
}
''')

# The remaining browser Test buttons must be health checks only. They must never
# create a conversation or inject FLIPAI_OK into the user's model history.
replace_once(
    "copilot_chat_webview_windows.go",
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost {\n\t\t\thttp.Error(rw, "POST required", http.StatusMethodNotAllowed)\n\t\t\treturn\n\t\t}\n\t\tturn(rw, r, "Reply with exactly: FLIPAI_OK", true)\n\t})''',
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost {\n\t\t\thttp.Error(rw, "POST required", http.StatusMethodNotAllowed)\n\t\t\treturn\n\t\t}\n\t\tif !authorized(r) {\n\t\t\thttp.Error(rw, "FlipAi token required", http.StatusForbidden)\n\t\t\treturn\n\t\t}\n\t\tif !copilotChatPageIsSignedIn(dev) {\n\t\t\trw.WriteHeader(http.StatusUnauthorized)\n\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Microsoft Copilot Chat is not signed in inside FlipAi."})\n\t\t\treturn\n\t\t}\n\t\tmutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {\n\t\t\ts.Connected, s.SignedIn, s.LastEvent, s.LastError = true, true, "health-check-ok", ""\n\t\t})\n\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "detail": "signed-in browser session ready"})\n\t})''',
    "Copilot read-only test endpoint",
)

replace_once(
    "muse_chat_webview_windows.go",
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost { http.Error(rw, "POST required", http.StatusMethodNotAllowed); return }\n\t\tturn(rw, r, "Reply with exactly: FLIPAI_OK", true)\n\t})''',
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost { http.Error(rw, "POST required", http.StatusMethodNotAllowed); return }\n\t\tif !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }\n\t\tif !museChatPageIsSignedIn(dev) {\n\t\t\trw.WriteHeader(http.StatusUnauthorized)\n\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Muse is not signed in inside FlipAi."})\n\t\t\treturn\n\t\t}\n\t\tmutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) {\n\t\t\ts.Connected, s.SignedIn, s.LastEvent, s.LastError = true, true, "health-check-ok", ""\n\t\t})\n\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "detail": "signed-in browser session ready"})\n\t})''',
    "Muse read-only test endpoint",
)

replace_once(
    "copilot_chat_webview.go",
    '''\tcopilotChatActivity(a.dataDir, "info", "copilot-chat-test", "Microsoft Copilot Chat completed a real browser turn successfully.", time.Since(started))\n\tmessage := "Copilot returned a real response through FlipAi's dedicated browser session."''',
    '''\tcopilotChatActivity(a.dataDir, "info", "copilot-chat-test", "Microsoft Copilot Chat signed-in browser session verified without sending a prompt.", time.Since(started))\n\tmessage := "Copilot's saved signed-in browser session is ready. No test prompt was sent."''',
    "Copilot test success copy",
)

replace_once(
    "muse_chat_webview.go",
    '''\tmuseChatActivity(a.dataDir, "info", "muse-chat-test", "Muse completed a real browser turn successfully.", time.Since(started))\n\tmessage := "Muse returned a real response through FlipAi's dedicated browser session."''',
    '''\tmuseChatActivity(a.dataDir, "info", "muse-chat-test", "Muse signed-in browser session verified without sending a prompt.", time.Since(started))\n\tmessage := "Muse's saved signed-in browser session is ready. No test prompt was sent."''',
    "Muse test success copy",
)
