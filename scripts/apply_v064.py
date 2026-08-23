from pathlib import Path
import re


def must_replace(text, old, new, label):
    if old not in text:
        raise SystemExit(f"missing expected text for {label}")
    return text.replace(old, new, 1)


def must_regex(text, pattern, repl, label):
    out, n = re.subn(pattern, repl, text, count=1, flags=re.S)
    if n != 1:
        raise SystemExit(f"expected one regex replacement for {label}, got {n}")
    return out

# VERSION
Path("VERSION").write_text("0.6.4\n", encoding="utf-8")

# config.go: version, optional code setting, and Codex no-sandbox approval policy.
p = Path("config.go")
s = p.read_text(encoding="utf-8")
s = must_replace(s, 'const version = "0.6.3"', 'const version = "0.6.4"', "version constant")
s = must_replace(
    s,
    'type SecurityConfig struct {\n\tCodeSalt string `json:"codeSalt,omitempty"`\n\tCodeHash string `json:"codeHash,omitempty"`\n}',
    'type SecurityConfig struct {\n\tRequireCode bool   `json:"requireCode"`\n\tCodeSalt    string `json:"codeSalt,omitempty"`\n\tCodeHash    string `json:"codeHash,omitempty"`\n}',
    "SecurityConfig",
)
s = must_replace(
    s,
    '\t\tCodex:        CodexConfig{ApprovalPolicy: "on-request"},\n\t\tClaude:       ClaudeConfig{PermissionMode: "acceptEdits", UseChrome: true},',
    '\t\tCodex:        CodexConfig{ApprovalPolicy: "never"},\n\t\tClaude:       ClaudeConfig{PermissionMode: "acceptEdits", UseChrome: true},\n\t\tSecurity:     SecurityConfig{RequireCode: true},',
    "default security and Codex policy",
)
s = must_replace(
    s,
    '\tif cfg.Codex.ApprovalPolicy == "" || cfg.Codex.ApprovalPolicy == "unlessTrusted" {\n\t\tcfg.Codex.ApprovalPolicy = "on-request"\n\t}',
    '\t// SMS turns intentionally use Codex full normal-user access. This removes\n\t// the Codex sandbox/approval layer but does not elevate the Windows process.\n\tcfg.Codex.ApprovalPolicy = "never"\n\t// Older configs predate RequireCode. Because loadConfig starts from\n\t// defaultConfig, they inherit RequireCode=true. If a manually edited config\n\t// disables the code without a stored hash, create an unguessable placeholder\n\t// so the older startup readiness check remains satisfied; parsing ignores it.\n\tif !cfg.Security.RequireCode && cfg.Security.CodeHash == "" {\n\t\tif placeholder, e := secureRandomToken(24); e == nil {\n\t\t\t_ = setSecurityCode(&cfg, placeholder)\n\t\t}\n\t}',
    "Codex approval migration",
)
p.write_text(s, encoding="utf-8")

# bridge.go: security code becomes optional while the sender allowlist remains mandatory.
p = Path("bridge.go")
s = p.read_text(encoding="utf-8")
new_parser = r'''func parseRemoteCommand(raw string, cfg Config) (remoteCommand, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return remoteCommand{}, errors.New("empty command")
	}
	rest := raw
	if cfg.Security.RequireCode {
		f := strings.Fields(raw)
		if len(f) < 2 {
			return remoteCommand{}, errors.New("missing SMS security code or command")
		}
		if !verifySecurityCode(cfg, f[0]) {
			return remoteCommand{}, errors.New("invalid SMS security code")
		}
		rest = strings.TrimSpace(strings.TrimPrefix(raw, f[0]))
	}
	up := strings.ToUpper(rest)
	if up == "STATUS" {
		return remoteCommand{Status: true}, nil
	}
	if up == "C NEW" || up == "C: NEW" {
		return remoteCommand{Agent: "C", New: true}, nil
	}
	if up == "A NEW" || up == "A: NEW" {
		return remoteCommand{Agent: "A", New: true}, nil
	}
	agent := cfg.DefaultAgent
	text := rest
	if strings.HasPrefix(up, "C:") {
		agent = "C"
		text = strings.TrimSpace(rest[2:])
	} else if strings.HasPrefix(up, "A:") {
		agent = "A"
		text = strings.TrimSpace(rest[2:])
	}
	if agent != "A" && agent != "C" {
		agent = "C"
	}
	if text == "" {
		return remoteCommand{}, errors.New("empty command")
	}
	return remoteCommand{Agent: agent, Text: text}, nil
}

'''
s = must_regex(
    s,
    r'func parseRemoteCommand\(raw string, cfg Config\) \(remoteCommand, error\) \{.*?\n\}\n\n(?=func \(b \*Bridge\) ensureCodex)',
    new_parser,
    "optional security-code parser",
)
p.write_text(s, encoding="utf-8")

# codex.go: full Windows-user access and automatic thread handoff/unsubscribe.
p = Path("codex.go")
s = p.read_text(encoding="utf-8")
s = must_replace(s, '\t"sync/atomic"\n)', '\t"sync/atomic"\n\t"time"\n)', "codex time import")
s = must_replace(
    s,
    '\tnextID        atomic.Int64\n\tnotifications chan rpcEnvelope\n\tdone          chan struct{}\n}',
    '\tnextID        atomic.Int64\n\tnotifications chan rpcEnvelope\n\tdone          chan struct{}\n\tthreadMu      sync.Mutex\n\tsubscribed    map[string]bool\n\tturnThreads   map[string]string\n}',
    "CodexClient thread state",
)
s = must_replace(
    s,
    'return &CodexClient{path: path, cwd: cwd, pending: map[int64]chan pendingResponse{}, notifications: make(chan rpcEnvelope, 2048), done: make(chan struct{})}',
    'return &CodexClient{path: path, cwd: cwd, pending: map[int64]chan pendingResponse{}, notifications: make(chan rpcEnvelope, 2048), done: make(chan struct{}), subscribed: map[string]bool{}, turnThreads: map[string]string{}}',
    "CodexClient constructor",
)

# Extend route so a completed turn releases only FlipAi's subscription to the persisted thread.
old_route_tail = '''\tif m.Method != "" {
\t\tselect {
\t\tcase c.notifications <- m:
\t\tdefault:
\t\t\tlog.Printf("Codex notification queue full: %s", m.Method)
\t\t}
\t}
}'''
new_route_tail = '''\tif m.Method != "" {
\t\tselect {
\t\tcase c.notifications <- m:
\t\tdefault:
\t\t\tlog.Printf("Codex notification queue full: %s", m.Method)
\t\t}
\t\tif m.Method == "turn/completed" {
\t\t\tvar p struct { Turn struct { ID string `json:"id"` } `json:"turn"` }
\t\t\tif json.Unmarshal(m.Params, &p) == nil && p.Turn.ID != "" {
\t\t\t\tif tid := c.takeTurnThread(p.Turn.ID); tid != "" {
\t\t\t\t\tgo c.releaseThreadWithRetry(tid)
\t\t\t\t}
\t\t\t}
\t\t}
\t}
}'''
s = must_replace(s, old_route_tail, new_route_tail, "turn-complete thread handoff")

request_pattern = r'''func \(c \*CodexClient\) Request\(ctx context\.Context, method string, params any\) \(json\.RawMessage, error\) \{.*?\n\}\n(?=func \(c \*CodexClient\) Notify)'''
request_replacement = r'''func cloneParamsMap(params any) map[string]any {
\tsrc, ok := params.(map[string]any)
\tif !ok {
\t\treturn nil
\t}
\tout := make(map[string]any, len(src)+4)
\tfor k, v := range src {
\t\tout[k] = v
\t}
\treturn out
}

// applyCodexRequestDefaults gives SMS-created Codex work the full permissions
// of the current Windows user while intentionally never requesting UAC/admin
// elevation. These shapes match the current Codex App Server v2 protocol:
// thread start/resume use sandbox="danger-full-access"; turns use the
// SandboxPolicy object {type:"dangerFullAccess"}.
func applyCodexRequestDefaults(method string, params any) any {
\tm := cloneParamsMap(params)
\tif m == nil {
\t\treturn params
\t}
\tswitch method {
\tcase "thread/start":
\t\tm["approvalPolicy"] = "never"
\t\tm["sandbox"] = "danger-full-access"
\t\tif _, exists := m["ephemeral"]; !exists {
\t\t\tm["ephemeral"] = false
\t\t}
\tcase "thread/resume":
\t\tm["approvalPolicy"] = "never"
\t\tm["sandbox"] = "danger-full-access"
\tcase "turn/start":
\t\tm["approvalPolicy"] = "never"
\t\tm["sandboxPolicy"] = map[string]any{"type": "dangerFullAccess"}
\t}
\treturn m
}

func stringParam(params any, key string) string {
\tm, _ := params.(map[string]any)
\tv, _ := m[key].(string)
\treturn v
}

func (c *CodexClient) setThreadSubscribed(threadID string, subscribed bool) {
\tif threadID == "" {
\t\treturn
\t}
\tc.threadMu.Lock()
\tc.subscribed[threadID] = subscribed
\tc.threadMu.Unlock()
}

func (c *CodexClient) threadSubscribed(threadID string) bool {
\tc.threadMu.Lock()
\tdefer c.threadMu.Unlock()
\treturn c.subscribed[threadID]
}

func (c *CodexClient) rememberTurnThread(turnID, threadID string) {
\tif turnID == "" || threadID == "" {
\t\treturn
\t}
\tc.threadMu.Lock()
\tc.turnThreads[turnID] = threadID
\tc.threadMu.Unlock()
}

func (c *CodexClient) takeTurnThread(turnID string) string {
\tc.threadMu.Lock()
\tdefer c.threadMu.Unlock()
\ttid := c.turnThreads[turnID]
\tdelete(c.turnThreads, turnID)
\treturn tid
}

func startedThreadID(raw json.RawMessage) string {
\tvar v struct { Thread struct { ID string `json:"id"` } `json:"thread"` }
\tif json.Unmarshal(raw, &v) != nil {
\t\treturn ""
\t}
\treturn v.Thread.ID
}

func startedTurnID(raw json.RawMessage) string {
\tvar v struct { Turn struct { ID string `json:"id"` } `json:"turn"` }
\tif json.Unmarshal(raw, &v) != nil {
\t\treturn ""
\t}
\treturn v.Turn.ID
}

func (c *CodexClient) requestRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
\tid := c.nextID.Add(1)
\tch := make(chan pendingResponse, 1)
\tc.pendingMu.Lock()
\tc.pending[id] = ch
\tc.pendingMu.Unlock()
\tif err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
\t\tc.pendingMu.Lock()
\t\tdelete(c.pending, id)
\t\tc.pendingMu.Unlock()
\t\treturn nil, err
\t}
\tselect {
\tcase r := <-ch:
\t\tif r.err != nil {
\t\t\treturn nil, fmt.Errorf("%s (%d)", r.err.Message, r.err.Code)
\t\t}
\t\treturn r.result, nil
\tcase <-ctx.Done():
\t\tc.pendingMu.Lock()
\t\tdelete(c.pending, id)
\t\tc.pendingMu.Unlock()
\t\treturn nil, ctx.Err()
\tcase <-c.done:
\t\tc.pendingMu.Lock()
\t\tdelete(c.pending, id)
\t\tc.pendingMu.Unlock()
\t\treturn nil, errors.New("codex app-server stopped")
\t}
}

func (c *CodexClient) releaseThread(ctx context.Context, threadID string) error {
\tif threadID == "" || !c.threadSubscribed(threadID) {
\t\treturn nil
\t}
\t_, err := c.requestRaw(ctx, "thread/unsubscribe", map[string]any{"threadId": threadID})
\tif err == nil {
\t\tc.setThreadSubscribed(threadID, false)
\t}
\treturn err
}

func (c *CodexClient) releaseThreadWithRetry(threadID string) {
\tfor attempt := 0; attempt < 3; attempt++ {
\t\tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
\t\terr := c.releaseThread(ctx, threadID)
\t\tcancel()
\t\tif err == nil {
\t\t\treturn
\t\t}
\t\tlog.Printf("Codex thread/unsubscribe %s attempt %d: %v", threadID, attempt+1, err)
\t\ttime.Sleep(150 * time.Millisecond)
\t}
}

// Request wraps the raw JSON-RPC request with two FlipAi invariants:
// 1) Codex gets full access available to this normal Windows user (never UAC).
// 2) Persisted threads are released whenever FlipAi is not actively running a
//    turn, so Codex Desktop can open the same history and continue it normally.
func (c *CodexClient) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
\tparams = applyCodexRequestDefaults(method, params)
\tif method == "turn/start" {
\t\ttid := stringParam(params, "threadId")
\t\tif tid != "" && !c.threadSubscribed(tid) {
\t\t\tresume := applyCodexRequestDefaults("thread/resume", map[string]any{"threadId": tid})
\t\t\tif _, err := c.requestRaw(ctx, "thread/resume", resume); err != nil {
\t\t\t\treturn nil, fmt.Errorf("resume Codex thread %s: %w", tid, err)
\t\t\t}
\t\t\tc.setThreadSubscribed(tid, true)
\t\t}
\t}

\traw, err := c.requestRaw(ctx, method, params)
\tif err != nil {
\t\treturn nil, err
\t}
\tswitch method {
\tcase "thread/start":
\t\tif tid := startedThreadID(raw); tid != "" {
\t\t\tc.setThreadSubscribed(tid, true)
\t\t\treleaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
\t\t\tif e := c.releaseThread(releaseCtx, tid); e != nil {
\t\t\t\tlog.Printf("Codex thread/start handoff %s: %v", tid, e)
\t\t\t}
\t\t\tcancel()
\t\t}
\tcase "thread/resume":
\t\ttid := stringParam(params, "threadId")
\t\tc.setThreadSubscribed(tid, true)
\t\treleaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
\t\tif e := c.releaseThread(releaseCtx, tid); e != nil {
\t\t\tlog.Printf("Codex thread/resume handoff %s: %v", tid, e)
\t\t}
\t\tcancel()
\tcase "turn/start":
\t\tc.rememberTurnThread(startedTurnID(raw), stringParam(params, "threadId"))
\tcase "thread/unsubscribe":
\t\tc.setThreadSubscribed(stringParam(params, "threadId"), false)
\t}
\treturn raw, nil
}

'''
s = must_regex(s, request_pattern, request_replacement, "Codex Request wrapper")
p.write_text(s, encoding="utf-8")

# activity_web.go: settings toggle + enhanced save + clearer permission/thread wording.
p = Path("activity_web.go")
s = p.read_text(encoding="utf-8")
insert_before = 'func copyRecordedResponse(w http.ResponseWriter, rec *httptest.ResponseRecorder, body []byte) {'
enhanced_save = r'''func (a *App) saveSetupEnhanced(w http.ResponseWriter, r *http.Request) {
\tif err := r.ParseMultipartForm(2 << 20); err != nil {
\t\trenderResult(w, 400, false, "Could not read settings", err.Error())
\t\treturn
\t}
\trequireCode := r.FormValue("requireSecurityCode") == "1"
\ta.mu.Lock()
\toldCfg := a.cfg
\tcfg := a.cfg
\ta.mu.Unlock()
\tprovidedCode := strings.TrimSpace(r.FormValue("securityCode"))
\tif requireCode && (!cfg.Security.RequireCode || cfg.Security.CodeHash == "") && providedCode == "" {
\t\trenderResult(w, 400, false, "Set an SMS security code", "Enter a new security code when turning code protection on.")
\t\treturn
\t}
\tif !requireCode && cfg.Security.CodeHash == "" {
\t\tplaceholder, err := secureRandomToken(24)
\t\tif err != nil || setSecurityCode(&cfg, placeholder) != nil {
\t\t\trenderResult(w, 500, false, "Could not disable the SMS code", "FlipAi could not create its internal disabled-code placeholder.")
\t\t\treturn
\t\t}
\t}
\tcfg.Security.RequireCode = requireCode
\ta.mu.Lock()
\ta.cfg = cfg
\ta.mu.Unlock()

\trec := httptest.NewRecorder()
\ta.saveSetup(rec, r)
\tif rec.Code >= 400 {
\t\ta.mu.Lock()
\t\ta.cfg = oldCfg
\t\ta.mu.Unlock()
\t}
\tcopyRecordedResponse(w, rec, rec.Body.Bytes())
}

'''
s = must_replace(s, insert_before, enhanced_save + insert_before, "enhanced settings save")
s = must_replace(
    s,
    '\t\tcase "/codex/test":',
    '\t\tcase "/setup/save":\n\t\t\ta.requireAuth(a.saveSetupEnhanced)(w, r)\n\t\t\treturn\n\t\tcase "/codex/test":',
    "setup/save route override",
)
# Add current-config dependent UI transformations immediately after s := string(body).
s = must_replace(
    s,
    '\t\t\ts := string(body)\n\t\t\ts = strings.Replace(s, `<a href="#diagnostics">Diagnostics</a>`,',
    '\t\t\ts := string(body)\n\t\t\ta.mu.Lock()\n\t\t\trequireCode := a.cfg.Security.RequireCode\n\t\t\ta.mu.Unlock()\n\t\t\ts = strings.Replace(s, `<a href="#diagnostics">Diagnostics</a>`,',
    "read RequireCode for page",
)
# Add transformations before body = []byte(s).
marker = '\t\t\ts = strings.Replace(s, `Tray → Open Settings reopens this page.`, `Tray → Open Settings reopens this page. Use Activity & Logs to trace each SMS end-to-end.`, 1)\n\t\t\tbody = []byte(s)'
replacement = '''\t\t\ts = strings.Replace(s, `Tray → Open Settings reopens this page.`, `Tray → Open Settings reopens this page. Use Activity & Logs to trace each SMS end-to-end.`, 1)
\t\t\ts = strings.Replace(s, `FlipAi verifies the sender and security code, then routes`, `FlipAi verifies the sender and, when enabled, the security code, then routes`, 1)
\t\t\ttoggle := `<label class="checkrow"><input type="checkbox" name="requireSecurityCode" value="1"`
\t\t\tif requireCode { toggle += ` checked` }
\t\t\ttoggle += `><span><b>Require SMS security code</b><span>Optional extra protection. The allowed phone-number list is always enforced even when this is off.</span></span></label>`
\t\t\ts = strings.Replace(s, `<div class="field"><label>SMS security code`, toggle+`<div class="field"><label>SMS security code`, 1)
\t\t\ts = strings.Replace(s, `Uses the local Codex App Server. FlipAi requires <b>Sign in with ChatGPT</b> and rejects API/provider auth.`, `Uses the local Codex App Server with <b>Sign in with ChatGPT</b>. SMS turns get full permissions of this Windows user (no Codex sandbox and no UAC/admin elevation), then the thread is released so Codex Desktop can open the same history.`, 1)
\t\t\tif !requireCode {
\t\t\t\ts = strings.Replace(s, `Private code required at the start of every text`, `Optional code — turn on “Require SMS security code” to enforce it`, 1)
\t\t\t\ts = strings.ReplaceAll(s, `YOURCODE C:`, `C:`)
\t\t\t\ts = strings.ReplaceAll(s, `YOURCODE A:`, `A:`)
\t\t\t\ts = strings.ReplaceAll(s, `YOURCODE STATUS`, `STATUS`)
\t\t\t}
\t\t\tbody = []byte(s)'''
s = must_replace(s, marker, replacement, "settings UI enhancements")
s = s.replace("sender verification, security-code validation, Codex/Claude", "sender verification, optional security-code validation, Codex/Claude")
p.write_text(s, encoding="utf-8")

# codex_test.go: make the mock able to reject non-full-access requests.
p = Path("codex_test.go")
s = p.read_text(encoding="utf-8")
s = must_replace(
    s,
    '''\t\tcase "thread/start":
\t\t\t_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thr_test", "ephemeral": true, "modelProvider": "openai"}, "modelProvider": "openai"}})
\t\tcase "turn/start":
\t\t\t_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": "turn_test", "status": "inProgress"}}})''',
    '''\t\tcase "thread/start":
\t\t\tif os.Getenv("FLIPAI_TEST_REQUIRE_FULL_ACCESS") == "1" {
\t\t\t\tp, _ := m["params"].(map[string]any)
\t\t\t\tif p["approvalPolicy"] != "never" || p["sandbox"] != "danger-full-access" {
\t\t\t\t\t_ = enc.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32001, "message": "missing full user access thread settings"}})
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t}
\t\t\t_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thr_test", "ephemeral": true, "modelProvider": "openai"}, "modelProvider": "openai"}})
\t\tcase "turn/start":
\t\t\tif os.Getenv("FLIPAI_TEST_REQUIRE_FULL_ACCESS") == "1" {
\t\t\t\tp, _ := m["params"].(map[string]any)
\t\t\t\tsp, _ := p["sandboxPolicy"].(map[string]any)
\t\t\t\tif p["approvalPolicy"] != "never" || sp["type"] != "dangerFullAccess" {
\t\t\t\t\t_ = enc.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32002, "message": "missing full user access turn settings"}})
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t}
\t\t\t_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": "turn_test", "status": "inProgress"}}})''',
    "mock full-access enforcement",
)
p.write_text(s, encoding="utf-8")

# New feature tests.
Path("v064_features_test.go").write_text(r'''package main

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
''', encoding="utf-8")

print("v0.6.4 feature patch applied")
