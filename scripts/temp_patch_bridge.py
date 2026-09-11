from pathlib import Path

p = Path("bridge.go")
s = p.read_text()

if "b.dispatchQueue(ctx)" in s:
    print("Provider isolation is already present.")
    raise SystemExit(0)

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
\t// Turns are serialized per provider, never across unrelated providers.
\tagentRunMu     map[string]*sync.Mutex
\tpendingByAgent map[string]int
\tactiveTurns    int
\tstate          State
\trunCtx         context.Context
""", "Bridge concurrency fields")

repl("""\t\tactivity: activityLogForStatePath(statePath),
\t\tqueueSig: make(chan struct{}, 1),
\t\tpaused:   cfg.Paused,
""", """\t\tactivity:       activityLogForStatePath(statePath),
\t\tqueueSig:        make(chan struct{}, 1),
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

// dispatchQueue is used by the live bridge. Jobs are independent across
// providers, while execute still serializes turns belonging to the same provider.
func (b *Bridge) dispatchQueue(ctx context.Context) {
\tfor ctx.Err() == nil {
\t\tj, ok := b.dequeue()
\t\tif !ok {
\t\t\treturn
\t\t}
\t\tgo func(j bridgeJob) {
\t\t\tb.execute(ctx, j.msg, j.cmd)
\t\t\tb.finishQueuedAgent(j.cmd.Agent)
\t\t\tif j.done != nil {
\t\t\t\tclose(j.done)
\t\t\t}
\t\t}(j)
\t}
}
""", "dispatch queue")

repl("""\t\t\tcase <-b.queueSig:
\t\t\t\tb.drainQueue(ctx)
""", """\t\t\tcase <-b.queueSig:
\t\t\t\tb.dispatchQueue(ctx)
""", "Run dispatcher")

repl("""\tline := fmt.Sprintf("Bridge online. Codex thread: %v. Claude session: %v. Busy: %v.",
\t\tb.state.CodexThreadID != "", b.state.ClaudeSessionID != "", b.busy)
""", """\tline := fmt.Sprintf("Bridge online. Codex thread: %v. Claude session: %v. Busy: %v.",
\t\tb.state.CodexThreadID != "", b.state.ClaudeSessionID != "", b.activeTurns > 0)
""", "status busy state")

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
\t// Preserve order only within the same provider. A stalled Grok turn must not
\t// block ChatGPT, Gemini, Claude, Copilot, Muse, Codex, or another provider.
\trunMu := b.agentMutex(rc.Agent)
\trunMu.Lock()
\tdefer runMu.Unlock()
\tstartedAt := time.Now()
\tb.mu.Lock()
\tb.activeTurns++
\tb.progress = ""
""", "per-provider execute lock")

repl("""\tdefer func() { b.mu.Lock(); b.busy = false; b.progress = ""; b.mu.Unlock() }()
""", """\tdefer func() {
\t\tb.mu.Lock()
\t\tif b.activeTurns > 0 {
\t\t\tb.activeTurns--
\t\t}
\t\tb.progress = ""
\t\tb.mu.Unlock()
\t}()
""", "active turn cleanup")

repl("""\t\tcase <-t.C:
\t\t\tline := agentName + " still working…"
\t\t\tif step := b.currentProgress(); step != "" {
\t\t\t\tline += " " + truncate(step, 120)
\t\t\t}
\t\t\tb.notify(ctx, m, line)
""", """\t\tcase <-t.C:
\t\t\t// Deliberately generic: never forward thought/tool/intermediate UI text.
\t\t\tline := agentName + " still working…"
\t\t\tb.notify(ctx, m, line)
""", "generic heartbeat")

repl("""func (b *Bridge) Busy() bool {
\tif b == nil {
\t\treturn false
\t}
\tb.mu.Lock()
\tdefer b.mu.Unlock()
\treturn b.busy
}
""", """func (b *Bridge) Busy() bool {
\tif b == nil {
\t\treturn false
\t}
\tb.mu.Lock()
\tdefer b.mu.Unlock()
\treturn b.activeTurns > 0
}
""", "Busy helper")

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

func TestLiveBridgeUsesConcurrentProviderDispatcher(t *testing.T) {
    raw, err := os.ReadFile("bridge.go")
    if err != nil {
        t.Fatal(err)
    }
    s := string(raw)
    for _, want := range []string{"b.dispatchQueue(ctx)", "b.agentMutex(rc.Agent)", "go func(j bridgeJob)"} {
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
