package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}
type rpcEnvelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}
type pendingResponse struct {
	result json.RawMessage
	err    *RPCError
}

type CodexClient struct {
	path, cwd     string
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	writeMu       sync.Mutex
	pendingMu     sync.Mutex
	pending       map[int64]chan pendingResponse
	nextID        atomic.Int64
	notifications chan rpcEnvelope
	done          chan struct{}
	threadMu      sync.Mutex
	subscribed    map[string]bool
	turnThreads   map[string]string
}

func NewCodexClient(path, cwd string) *CodexClient {
	return &CodexClient{path: path, cwd: cwd, pending: map[int64]chan pendingResponse{}, notifications: make(chan rpcEnvelope, 2048), done: make(chan struct{}), subscribed: map[string]bool{}, turnThreads: map[string]string{}}
}

// scrubOpenAIEnv prevents an unrelated machine-level API key or custom API
// endpoint from silently changing FlipAi from ChatGPT subscription auth to API
// billing. Codex keeps its normal on-disk ChatGPT OAuth state under CODEX_HOME.
func scrubOpenAIEnv(env []string) []string {
	deny := map[string]bool{
		"OPENAI_API_KEY":    true,
		"OPENAI_BASE_URL":   true,
		"OPENAI_ORG_ID":     true,
		"OPENAI_PROJECT_ID": true,
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		k := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			k = e[:i]
		}
		if !deny[strings.ToUpper(k)] {
			out = append(out, e)
		}
	}
	return out
}

func (c *CodexClient) Start(ctx context.Context) error {
	if strings.TrimSpace(c.path) == "" {
		c.path = "codex"
	}
	exe := resolveCodexExecutable(c.path)
	c.cmd = exec.CommandContext(ctx, exe, "app-server", "--listen", "stdio://")
	c.cmd.Env = augmentCodexEnv(exe, scrubOpenAIEnv(os.Environ()))
	if c.cwd != "" {
		c.cmd.Dir = c.cwd
	}
	out, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	er, err := c.cmd.StderrPipe()
	if err != nil {
		return err
	}
	in, err := c.cmd.StdinPipe()
	if err != nil {
		return err
	}
	c.stdin = in
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start Codex app-server using %q: %w (install/open Codex Desktop or set the Codex executable path in FlipAi)", exe, err)
	}
	c.path = exe
	go c.readStdout(out)
	go c.readStderr(er)
	go func() { _ = c.cmd.Wait(); close(c.done) }()
	_, err = c.Request(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "flipai_sms_bridge", "title": "FlipAi AI SMS Bridge", "version": version}})
	if err != nil {
		c.Close()
		return fmt.Errorf("initialize Codex app-server: %w", err)
	}
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		c.Close()
		return err
	}
	return nil
}

func (c *CodexClient) readStdout(r io.Reader) {
	br := bufio.NewReaderSize(r, 256*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var m rpcEnvelope
			if json.Unmarshal(bytes.TrimSpace(line), &m) == nil {
				c.route(m)
			}
		}
		if err != nil {
			return
		}
	}
}
func (c *CodexClient) readStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 2<<20)
	for sc.Scan() {
		if x := strings.TrimSpace(sc.Text()); x != "" {
			log.Printf("codex: %s", x)
		}
	}
}
func rawIDInt(raw json.RawMessage) (int64, bool) {
	var n int64
	if len(raw) > 0 && json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	return 0, false
}
func (c *CodexClient) route(m rpcEnvelope) {
	if len(m.ID) > 0 && m.Method != "" {
		c.handleServerRequest(m)
		return
	}
	if id, ok := rawIDInt(m.ID); ok && m.Method == "" {
		c.pendingMu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.pendingMu.Unlock()
		if ch != nil {
			ch <- pendingResponse{m.Result, m.Error}
		}
		return
	}
	if m.Method != "" {
		select {
		case c.notifications <- m:
		default:
			log.Printf("Codex notification queue full: %s", m.Method)
		}
		if m.Method == "turn/completed" {
			var p struct { Turn struct { ID string `json:"id"` } `json:"turn"` }
			if json.Unmarshal(m.Params, &p) == nil && p.Turn.ID != "" {
				if tid := c.takeTurnThread(p.Turn.ID); tid != "" {
					go c.releaseThreadWithRetry(tid)
				}
			}
		}
	}
}
func (c *CodexClient) handleServerRequest(m rpcEnvelope) {
	var id any
	_ = json.Unmarshal(m.ID, &id)
	result := map[string]any{}
	switch m.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		result = map[string]any{"decision": "decline"}
	}
	_ = c.send(map[string]any{"id": id, "result": result})
}
func cloneParamsMap(params any) map[string]any {
	src, ok := params.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any, len(src)+4)
	for k, v := range src {
		out[k] = v
	}
	return out
}

// applyCodexRequestDefaults gives SMS-created Codex work the full permissions
// of the current Windows user while intentionally never requesting UAC/admin
// elevation. These shapes match the current Codex App Server v2 protocol:
// thread start/resume use sandbox="danger-full-access"; turns use the
// SandboxPolicy object {type:"dangerFullAccess"}.
func applyCodexRequestDefaults(method string, params any) any {
	m := cloneParamsMap(params)
	if m == nil {
		return params
	}
	switch method {
	case "thread/start":
		m["approvalPolicy"] = "never"
		m["sandbox"] = "danger-full-access"
		if _, exists := m["ephemeral"]; !exists {
			m["ephemeral"] = false
		}
	case "thread/resume":
		m["approvalPolicy"] = "never"
		m["sandbox"] = "danger-full-access"
	case "turn/start":
		m["approvalPolicy"] = "never"
		m["sandboxPolicy"] = map[string]any{"type": "dangerFullAccess"}
	}
	return m
}

func stringParam(params any, key string) string {
	m, _ := params.(map[string]any)
	v, _ := m[key].(string)
	return v
}

func (c *CodexClient) setThreadSubscribed(threadID string, subscribed bool) {
	if threadID == "" {
		return
	}
	c.threadMu.Lock()
	c.subscribed[threadID] = subscribed
	c.threadMu.Unlock()
}

func (c *CodexClient) threadSubscribed(threadID string) bool {
	c.threadMu.Lock()
	defer c.threadMu.Unlock()
	return c.subscribed[threadID]
}

func (c *CodexClient) rememberTurnThread(turnID, threadID string) {
	if turnID == "" || threadID == "" {
		return
	}
	c.threadMu.Lock()
	c.turnThreads[turnID] = threadID
	c.threadMu.Unlock()
}

func (c *CodexClient) takeTurnThread(turnID string) string {
	c.threadMu.Lock()
	defer c.threadMu.Unlock()
	tid := c.turnThreads[turnID]
	delete(c.turnThreads, turnID)
	return tid
}

func startedThreadID(raw json.RawMessage) string {
	var v struct { Thread struct { ID string `json:"id"` } `json:"thread"` }
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v.Thread.ID
}

func startedTurnID(raw json.RawMessage) string {
	var v struct { Turn struct { ID string `json:"id"` } `json:"turn"` }
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v.Turn.ID
}

func (c *CodexClient) requestRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan pendingResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("%s (%d)", r.err.Message, r.err.Code)
		}
		return r.result, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, errors.New("codex app-server stopped")
	}
}

func (c *CodexClient) releaseThread(ctx context.Context, threadID string) error {
	if threadID == "" || !c.threadSubscribed(threadID) {
		return nil
	}
	_, err := c.requestRaw(ctx, "thread/unsubscribe", map[string]any{"threadId": threadID})
	if err == nil {
		c.setThreadSubscribed(threadID, false)
	}
	return err
}

func (c *CodexClient) releaseThreadWithRetry(threadID string) {
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := c.releaseThread(ctx, threadID)
		cancel()
		if err == nil {
			return
		}
		log.Printf("Codex thread/unsubscribe %s attempt %d: %v", threadID, attempt+1, err)
		time.Sleep(150 * time.Millisecond)
	}
}

// Request wraps the raw JSON-RPC request with two FlipAi invariants:
// 1) Codex gets full access available to this normal Windows user (never UAC).
// 2) Persisted threads are released whenever FlipAi is not actively running a
//    turn, so Codex Desktop can open the same history and continue it normally.
func (c *CodexClient) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	params = applyCodexRequestDefaults(method, params)
	if method == "turn/start" {
		tid := stringParam(params, "threadId")
		if tid != "" && !c.threadSubscribed(tid) {
			resume := applyCodexRequestDefaults("thread/resume", map[string]any{"threadId": tid})
			if _, err := c.requestRaw(ctx, "thread/resume", resume); err != nil {
				return nil, fmt.Errorf("resume Codex thread %s: %w", tid, err)
			}
			c.setThreadSubscribed(tid, true)
		}
	}

	raw, err := c.requestRaw(ctx, method, params)
	if err != nil {
		return nil, err
	}
	switch method {
	case "thread/start":
		if tid := startedThreadID(raw); tid != "" {
			c.setThreadSubscribed(tid, true)
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if e := c.releaseThread(releaseCtx, tid); e != nil {
				log.Printf("Codex thread/start handoff %s: %v", tid, e)
			}
			cancel()
		}
	case "thread/resume":
		tid := stringParam(params, "threadId")
		c.setThreadSubscribed(tid, true)
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if e := c.releaseThread(releaseCtx, tid); e != nil {
			log.Printf("Codex thread/resume handoff %s: %v", tid, e)
		}
		cancel()
	case "turn/start":
		c.rememberTurnThread(startedTurnID(raw), stringParam(params, "threadId"))
	case "thread/unsubscribe":
		c.setThreadSubscribed(stringParam(params, "threadId"), false)
	}
	return raw, nil
}

func (c *CodexClient) Notify(method string, params any) error {
	return c.send(map[string]any{"method": method, "params": params})
}
func (c *CodexClient) send(v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	b = append(b, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.stdin == nil {
		return errors.New("codex stdin is not available")
	}
	_, e = c.stdin.Write(b)
	return e
}

// Account asks Codex to refresh its persisted ChatGPT OAuth state before
// reporting the account. The current app-server protocol allows
// requiresOpenaiAuth=true at the same time as account.type=chatgpt; that flag
// describes the provider, not whether the user is signed out.
//
// Older FlipAi UI code interpreted requiresOpenaiAuth as a sign-out flag. For
// a confirmed ChatGPT account we normalize that one field to false before
// returning the payload so every existing caller uses the correct meaning.
func (c *CodexClient) Account(ctx context.Context) (json.RawMessage, error) {
	raw, err := c.Request(ctx, "account/read", map[string]any{"refreshToken": true})
	if err != nil {
		return nil, err
	}
	if codexAccountIsChatGPT(raw) {
		var v map[string]any
		if json.Unmarshal(raw, &v) == nil {
			v["requiresOpenaiAuth"] = false
			if normalized, marshalErr := json.Marshal(v); marshalErr == nil {
				return normalized, nil
			}
		}
	}
	return raw, nil
}
func (c *CodexClient) Alive() bool {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}
func (c *CodexClient) Close() {
	if c == nil {
		return
	}
	c.writeMu.Lock()
	if c.stdin != nil {
		_ = c.stdin.Close()
		c.stdin = nil
	}
	c.writeMu.Unlock()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}
func codexAccountIsChatGPT(raw json.RawMessage) bool {
	var v struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Account == nil {
		return false
	}
	return strings.EqualFold(v.Account.Type, "chatgpt")
}
