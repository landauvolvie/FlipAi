package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// claudeLiveReadyTimeout is how long FlipAi waits for a freshly started session
// to report itself through the SessionStart hook. A session that has not come
// up by then is treated as unavailable and the turn falls back to print mode,
// rather than holding a text message indefinitely.
const claudeLiveReadyTimeout = 90 * time.Second

// liveTurn is one SMS waiting for its answer. The reply channel is buffered so
// a hook callback never blocks on a caller that has already timed out.
type liveTurn struct {
	reply chan string
}

// ClaudeLiveClient supervises the single long-lived Claude Code session that
// live mode delivers SMS into.
//
// It owns three things that print mode never needed: a child process that has
// to outlive the turn, the session's inbox address, and the correlation between
// an injected prompt and the reply that eventually comes back for it. The last
// one exists because a live session has a second writer — whoever is typing at
// claude.ai/code — and FlipAi must text back only the turns it started.
type ClaudeLiveClient struct {
	path        string
	cwd         string
	cfg         ClaudeConfig
	token       string
	hookCommand string

	// writeInbox is the transport that delivers a frame to the session. It is a
	// field rather than a direct call because the real one is platform-specific
	// — a named pipe on Windows, a Unix socket elsewhere — and the delivery and
	// correlation logic above it is not. Tests substitute it so that logic is
	// exercised on every platform rather than only where the fake transport
	// happens to be constructible.
	writeInbox func(addr string, frame []byte) error

	mu        sync.Mutex
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	ready     chan struct{}
	sessionID string
	socket    string
	msgToken  string
	name      string

	// pending maps a FlipAi marker id to the turn waiting on it; byPrompt maps
	// Claude Code's own prompt id onto that marker once UserPromptSubmit has
	// reported the pairing. Stop only carries the prompt id, so both are needed
	// to get from a finished turn back to the SMS that started it.
	pending  map[string]*liveTurn
	byPrompt map[string]string
}

func NewClaudeLiveClient(path, cwd string, cfg ClaudeConfig, token, hookCommand string) *ClaudeLiveClient {
	return &ClaudeLiveClient{
		path:        resolveClaudeExecutable(path),
		cwd:         cwd,
		cfg:         cfg,
		token:       strings.TrimSpace(token),
		hookCommand: hookCommand,
		writeInbox:  writeClaudeInbox,
		pending:     map[string]*liveTurn{},
		byPrompt:    map[string]string{},
	}
}

// SessionID reports the live session, empty when none is running. The Agents
// page uses it for the same resume hint print mode shows.
func (c *ClaudeLiveClient) SessionID() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

// Running reports whether a supervised session is up and has reported itself.
func (c *ClaudeLiveClient) Running() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cmd != nil && c.socket != ""
}

// childEnv mirrors ClaudeClient.childEnv so a live session is billed and
// authenticated exactly like a print one.
//
// The caller decides whether a token is passed at all: it withholds the stored
// one when this account has a real sign-in, because that sign-in is the only
// credential Remote Control and the Chrome extension can use. An inherited
// CLAUDE_CODE_OAUTH_TOKEN is always stripped first, or a withheld token would
// walk straight back in through the environment and turn the browser off again.
func (c *ClaudeLiveClient) childEnv() []string {
	env := scrubAnthropicEnv(os.Environ())
	out := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(strings.ToUpper(e), "CLAUDE_CODE_OAUTH_TOKEN=") {
			out = append(out, e)
		}
	}
	env = out
	if tok := strings.TrimSpace(c.token); tok != "" {
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+tok)
	}
	return env
}

// Start launches the session and waits for it to report itself. Calling it on a
// client that already has a live session is a no-op, so every turn can call it
// without tracking whether the process is up.
func (c *ClaudeLiveClient) Start(ctx context.Context, sessionName string) error {
	c.mu.Lock()
	if c.cmd != nil && c.socket != "" {
		c.mu.Unlock()
		return nil
	}
	if c.cmd != nil {
		// Started but not yet ready: wait on the existing attempt rather than
		// spawning a second session against the same working folder.
		ready := c.ready
		c.mu.Unlock()
		return c.waitReady(ctx, ready)
	}
	settings, err := claudeLiveSettings(c.cfg, c.hookCommand)
	if err != nil {
		c.mu.Unlock()
		return liveUnavailable("live mode could not be configured: %v", err)
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	cmd := exec.CommandContext(runCtx, c.path, claudeLiveArgs(c.cfg, sessionName, settings)...)
	if c.cwd != "" {
		cmd.Dir = c.cwd
	}
	cmd.Env = c.childEnv()
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		c.mu.Unlock()
		return liveUnavailable("the live Claude session could not start: %v", err)
	}
	ready := make(chan struct{})
	c.cmd, c.cancel, c.ready, c.name = cmd, cancel, ready, sessionName
	c.mu.Unlock()

	// Reap the child so a session that dies on its own is noticed and the next
	// turn starts a fresh one instead of injecting into a dead pipe.
	go func() {
		_ = cmd.Wait()
		c.mu.Lock()
		if c.cmd == cmd {
			c.cmd, c.socket, c.msgToken, c.sessionID = nil, "", "", ""
		}
		c.mu.Unlock()
	}()

	return c.waitReady(ctx, ready)
}

func (c *ClaudeLiveClient) waitReady(ctx context.Context, ready <-chan struct{}) error {
	if ready == nil {
		return liveUnavailable("the live Claude session did not start")
	}
	t := time.NewTimer(claudeLiveReadyTimeout)
	defer t.Stop()
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return liveUnavailable("the live Claude session was still starting when the turn was cancelled")
	case <-t.C:
		// The most likely cause by far is that `claude remote-control` needs a
		// console FlipAi's background host does not have, so say so instead of
		// reporting a bare timeout.
		return liveUnavailable("the live Claude session did not report itself within %s. "+
			"This usually means Claude Code could not start without a terminal on this account; "+
			"FlipAi used per-message mode for this text", claudeLiveReadyTimeout)
	}
}

// Stop ends the supervised session. A conversation reset and a switch back to
// print mode both go through here, so no orphaned session keeps holding the
// working folder.
func (c *ClaudeLiveClient) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	cancel := c.cancel
	c.cmd, c.cancel, c.socket, c.msgToken, c.sessionID = nil, nil, "", "", ""
	for id, turn := range c.pending {
		close(turn.reply)
		delete(c.pending, id)
	}
	c.byPrompt = map[string]string{}
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Deliver feeds one hook callback into the client. It is called from the
// loopback HTTP handler, never from a turn, so it must not block.
//
// It returns whether the payload belonged to a FlipAi turn, which the caller
// logs: a false here is the normal shape of someone typing in the browser, not
// an error.
func (c *ClaudeLiveClient) Deliver(p claudeHookPayload) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	switch p.Event {
	case claudeHookSessionStart:
		if strings.TrimSpace(p.Socket) == "" {
			// A session that reports no inbox cannot be delivered into. Leaving
			// it unready is what makes the turn fall back rather than hang.
			return false
		}
		c.socket, c.msgToken, c.sessionID = p.Socket, p.Token, p.SessionID
		if c.ready != nil {
			select {
			case <-c.ready:
			default:
				close(c.ready)
			}
		}
		return true

	case claudeHookUserPrompt:
		id, ok := claudeLiveMarkerID(p.UserPrompt)
		if !ok {
			return false
		}
		if _, waiting := c.pending[id]; !waiting {
			return false
		}
		if strings.TrimSpace(p.PromptID) != "" {
			c.byPrompt[p.PromptID] = id
		}
		return true

	case claudeHookStop:
		id, ok := c.byPrompt[p.PromptID]
		if !ok {
			return false
		}
		delete(c.byPrompt, p.PromptID)
		turn, waiting := c.pending[id]
		if !waiting {
			return false
		}
		delete(c.pending, id)
		select {
		case turn.reply <- p.LastAssistantMessage:
		default:
		}
		return true
	}
	return false
}

// Run delivers one SMS into the live session and waits for the reply to that
// specific turn.
//
// Every failure here is a claudeLiveUnavailable, because the caller's correct
// response to all of them is the same: run the text through print mode instead.
// A text message is never dropped because live mode had a bad day.
func (c *ClaudeLiveClient) Run(ctx context.Context, sessionName, sender, prompt string) (string, error) {
	if c == nil {
		return "", liveUnavailable("live mode is not configured")
	}
	if err := c.Start(ctx, sessionName); err != nil {
		return "", err
	}

	markerID := randomNameSuffix() + randomNameSuffix()
	turn := &liveTurn{reply: make(chan string, 1)}

	c.mu.Lock()
	socket, token := c.socket, c.msgToken
	c.pending[markerID] = turn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, markerID)
		for pid, id := range c.byPrompt {
			if id == markerID {
				delete(c.byPrompt, pid)
			}
		}
		c.mu.Unlock()
	}()

	if strings.TrimSpace(socket) == "" {
		return "", liveUnavailable("the live Claude session did not report an inbox address")
	}
	frame, err := claudeInboxFrame(token, "FlipAi SMS", claudeLiveMarker(markerID)+"\n"+prompt)
	if err != nil {
		return "", liveUnavailable("the SMS could not be prepared for the live session: %v", err)
	}
	write := c.writeInbox
	if write == nil {
		write = writeClaudeInbox
	}
	if err := write(socket, frame); err != nil {
		return "", liveUnavailable("the SMS could not be delivered into the live Claude session: %v", err)
	}

	select {
	case reply, ok := <-turn.reply:
		if !ok {
			return "", liveUnavailable("the live Claude session ended before it answered")
		}
		answer := strings.TrimSpace(reply)
		if answer == "" {
			return "", liveUnavailable("the live Claude session finished the turn without an answer")
		}
		return answer, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// The turn timeout is the user's own setting, so this is a real
			// timeout rather than a live-mode problem. Report it as such: a
			// print-mode retry would only spend the same time again.
			return "", errors.New("the Claude turn ran past the configured turn timeout")
		}
		return "", liveUnavailable("the live Claude turn was cancelled before it answered")
	}
}
