from pathlib import Path

# Make completed-turn handoff race-safe. Codex can emit turn/completed before
# the turn/start response has returned to the caller, so remember completion
# until the turn->thread association is known.
p = Path("codex.go")
s = p.read_text(encoding="utf-8")
old = '''\tsubscribed    map[string]bool
\tturnThreads   map[string]string
}'''
new = '''\tsubscribed    map[string]bool
\tturnThreads   map[string]string
\tcompletedTurns map[string]bool
}'''
if old not in s:
    raise SystemExit("missing CodexClient map fields")
s = s.replace(old, new, 1)
old = 'subscribed: map[string]bool{}, turnThreads: map[string]string{}}'
new = 'subscribed: map[string]bool{}, turnThreads: map[string]string{}, completedTurns: map[string]bool{}}'
if old not in s:
    raise SystemExit("missing CodexClient constructor maps")
s = s.replace(old, new, 1)
old = '''\t\tif m.Method == "turn/completed" {
\t\t\tvar p struct { Turn struct { ID string `json:"id"` } `json:"turn"` }
\t\t\tif json.Unmarshal(m.Params, &p) == nil && p.Turn.ID != "" {
\t\t\t\tif tid := c.takeTurnThread(p.Turn.ID); tid != "" {
\t\t\t\t\tgo c.releaseThreadWithRetry(tid)
\t\t\t\t}
\t\t\t}
\t\t}'''
new = '''\t\tif m.Method == "turn/completed" {
\t\t\tvar p struct { Turn struct { ID string `json:"id"` } `json:"turn"` }
\t\t\tif json.Unmarshal(m.Params, &p) == nil && p.Turn.ID != "" {
\t\t\t\tc.markTurnCompleted(p.Turn.ID)
\t\t\t}
\t\t}'''
if old not in s:
    raise SystemExit("missing turn/completed route")
s = s.replace(old, new, 1)
old = '''func (c *CodexClient) rememberTurnThread(turnID, threadID string) {
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
}'''
new = '''func (c *CodexClient) rememberTurnThread(turnID, threadID string) {
\tif turnID == "" || threadID == "" {
\t\treturn
\t}
\treleaseNow := false
\tc.threadMu.Lock()
\tif c.completedTurns[turnID] {
\t\tdelete(c.completedTurns, turnID)
\t\treleaseNow = true
\t} else {
\t\tc.turnThreads[turnID] = threadID
\t}
\tc.threadMu.Unlock()
\tif releaseNow {
\t\tgo c.releaseThreadWithRetry(threadID)
\t}
}

func (c *CodexClient) markTurnCompleted(turnID string) {
\tif turnID == "" {
\t\treturn
\t}
\tthreadID := ""
\tc.threadMu.Lock()
\tthreadID = c.turnThreads[turnID]
\tif threadID != "" {
\t\tdelete(c.turnThreads, turnID)
\t} else {
\t\tc.completedTurns[turnID] = true
\t}
\tc.threadMu.Unlock()
\tif threadID != "" {
\t\tgo c.releaseThreadWithRetry(threadID)
\t}
}'''
if old not in s:
    raise SystemExit("missing turn mapping helpers")
s = s.replace(old, new, 1)
p.write_text(s, encoding="utf-8")

# If code protection is disabled on a brand-new setup, browser validation must
# not force the password field merely because no code hash exists yet.
p = Path("activity_web.go")
s = p.read_text(encoding="utf-8")
old = '''\t\t\tif !requireCode {
\t\t\t\ts = strings.Replace(s, `Private code required at the start of every text`, `Optional code — turn on “Require SMS security code” to enforce it`, 1)'''
new = '''\t\t\tif !requireCode {
\t\t\t\ts = strings.Replace(s, `name="securityCode" autocomplete="new-password" placeholder="Private code required at the start of every text" required>`, `name="securityCode" autocomplete="new-password" placeholder="Private code required at the start of every text">`, 1)
\t\t\t\ts = strings.Replace(s, `Private code required at the start of every text`, `Optional code — turn on “Require SMS security code” to enforce it`, 1)'''
if old not in s:
    raise SystemExit("missing no-code UI block")
s = s.replace(old, new, 1)
p.write_text(s, encoding="utf-8")

# Strengthen the mock so the full-access test also proves SMS threads are
# explicitly durable rather than ephemeral.
p = Path("codex_test.go")
s = p.read_text(encoding="utf-8")
old = '''\t\t\t\tif p["approvalPolicy"] != "never" || p["sandbox"] != "danger-full-access" {
\t\t\t\t\t_ = enc.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32001, "message": "missing full user access thread settings"}})'''
new = '''\t\t\t\tif p["approvalPolicy"] != "never" || p["sandbox"] != "danger-full-access" || p["ephemeral"] != false {
\t\t\t\t\t_ = enc.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32001, "message": "missing full user access/durable thread settings"}})'''
if old not in s:
    raise SystemExit("missing mock full-access thread check")
s = s.replace(old, new, 1)
p.write_text(s, encoding="utf-8")

print("made v0.6.4 handoff race-safe and no-code UI optional")
