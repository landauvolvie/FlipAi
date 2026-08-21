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
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
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
}

func NewCodexClient(path, cwd string) *CodexClient {
	return &CodexClient{path: path, cwd: cwd, pending: map[int64]chan pendingResponse{}, notifications: make(chan rpcEnvelope, 2048), done: make(chan struct{})}
}

func (c *CodexClient) Start(ctx context.Context) error {
	if strings.TrimSpace(c.path) == "" {
		return errors.New("Codex path is empty")
	}
	c.cmd = exec.CommandContext(ctx, c.path, "app-server", "--listen", "stdio://")
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
		return fmt.Errorf("start codex app-server: %w", err)
	}
	go c.readStdout(out)
	go c.readStderr(er)
	go func() { _ = c.cmd.Wait(); close(c.done) }()
	_, err = c.Request(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "codex_googlevoice_bridge", "title": "Codex Google Voice Bridge", "version": version}})
	if err != nil {
		return err
	}
	return c.Notify("initialized", map[string]any{})
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
	}
}
func (c *CodexClient) handleServerRequest(m rpcEnvelope) {
	// The SMS bridge cannot pause and ask through the same Codex turn. For approval
	// requests we rely on guardian_subagent when configured. Unexpected requests
	// are rejected instead of guessed/auto-approved.
	var id any
	_ = json.Unmarshal(m.ID, &id)
	result := map[string]any{}
	switch m.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		result = map[string]any{"decision": "decline"}
	default:
		result = map[string]any{}
	}
	_ = c.send(map[string]any{"id": id, "result": result})
}
func (c *CodexClient) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan pendingResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("%s (%d)", r.err.Message, r.err.Code)
		}
		return r.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("codex app-server stopped")
	}
}
func (c *CodexClient) Notify(method string, params any) error {
	return c.send(map[string]any{"method": method, "params": params})
}
func (c *CodexClient) send(v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, e = c.stdin.Write(append(b, '\n'))
	return e
}

func (c *CodexClient) Account(ctx context.Context) (json.RawMessage, error) {
	return c.Request(ctx, "account/read", map[string]any{"refreshToken": true})
}
func (c *CodexClient) StartChatGPTLogin(ctx context.Context) (string, error) {
	raw, err := c.Request(ctx, "account/login/start", map[string]any{"type": "chatgpt", "useHostedLoginSuccessPage": true, "appBrand": "codex"})
	if err != nil {
		return "", err
	}
	var r struct {
		AuthURL string `json:"authUrl"`
	}
	if json.Unmarshal(raw, &r) != nil || r.AuthURL == "" {
		return "", errors.New("Codex did not return a login URL")
	}
	return r.AuthURL, nil
}

func (c *CodexClient) Alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *CodexClient) Close() {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
}

func codexAccountIsChatGPT(raw json.RawMessage) bool {
	var v struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
		Requires bool `json:"requiresOpenaiAuth"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Requires || v.Account == nil {
		return false
	}
	return strings.EqualFold(v.Account.Type, "chatgpt")
}
