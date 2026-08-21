package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Bridge struct {
	cfg       Config
	statePath string
	gmail     MailClient
	codex     *CodexClient
	claude    *ClaudeClient
	mu        sync.Mutex
	state     State
	busy      bool
	runCtx    context.Context
}

func NewBridge(cfg Config, statePath string, state State, g MailClient, c *CodexClient, a *ClaudeClient) *Bridge {
	return &Bridge{cfg: cfg, statePath: statePath, state: state, gmail: g, codex: c, claude: a}
}

var footerPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^google voice$`), regexp.MustCompile(`(?i)^view.*message`), regexp.MustCompile(`(?i)^new text message from`), regexp.MustCompile(`(?i)^reply`), regexp.MustCompile(`(?i)^voice\.google\.com`), regexp.MustCompile(`(?i)^you received.*message`), regexp.MustCompile(`(?i)^to respond`), regexp.MustCompile(`(?i)^sent via google voice`),
}

func looksFooter(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	for _, r := range footerPatterns {
		if r.MatchString(line) {
			return true
		}
	}
	return false
}
func googleDKIMPassed(v string) bool {
	s := strings.ToLower(v)
	return strings.Contains(s, "dkim=pass") && (strings.Contains(s, "header.d=google.com") || strings.Contains(s, "header.i=@google.com") || strings.Contains(s, "header.d=voice.google.com"))
}

func parseGoogleVoiceBody(m GmailMessage, allowed, requiredPhrase string) (string, string, bool) {
	fromLower := strings.ToLower(m.From)
	if !strings.Contains(fromLower, "voice-noreply@google.com") && !strings.Contains(fromLower, "@txt.voice.google.com") {
		return "", "", false
	}
	if !googleDKIMPassed(m.AuthenticationResults) {
		return "", "", false
	}
	sender, ok := googleVoiceSender(m, requiredPhrase)
	if !ok || !allowedPhone(allowed, sender) {
		return "", "", false
	}
	body := strings.ReplaceAll(m.Body, "\r\n", "\n")
	kept := []string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if looksFooter(line) || normalizeUSPhone(line) == sender {
			continue
		}
		l := strings.ToLower(line)
		if strings.Contains(l, "unsubscribe") || strings.Contains(l, "privacy") {
			continue
		}
		kept = append(kept, line)
	}
	cmd := strings.TrimSpace(strings.Join(kept, "\n"))
	if cmd == "" {
		cmd = strings.TrimSpace(m.Snippet)
	}
	if cmd == "" {
		return "", "", false
	}
	lower := strings.ToLower(cmd)
	if i := strings.Index(lower, "new text message from"); i >= 0 {
		if j := strings.Index(cmd[i:], ":"); j >= 0 && i+j+1 < len(cmd) {
			cmd = strings.TrimSpace(cmd[i+j+1:])
		}
	}
	return cmd, sender, true
}

type remoteCommand struct {
	Agent  string
	Text   string
	Sender string
	New    bool
	Status bool
}

func parseRemoteCommand(raw string, cfg Config) (remoteCommand, error) {
	f := strings.Fields(strings.TrimSpace(raw))
	if len(f) < 2 {
		return remoteCommand{}, errors.New("missing SMS security code or command")
	}
	if !verifySecurityCode(cfg, f[0]) {
		return remoteCommand{}, errors.New("invalid SMS security code")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), f[0]))
	up := strings.ToUpper(rest)
	if up == "STATUS" {
		return remoteCommand{Status: true}, nil
	}
	agent := cfg.DefaultAgent
	text := rest
	if strings.HasPrefix(up, "C:") {
		agent = "C"
		text = strings.TrimSpace(rest[2:])
	} else if strings.HasPrefix(up, "A:") {
		agent = "A"
		text = strings.TrimSpace(rest[2:])
	} else if up == "C NEW" || up == "C: NEW" {
		return remoteCommand{Agent: "C", New: true}, nil
	} else if up == "A NEW" || up == "A: NEW" {
		return remoteCommand{Agent: "A", New: true}, nil
	}
	if agent != "A" && agent != "C" {
		agent = "C"
	}
	if text == "" {
		return remoteCommand{}, errors.New("empty command")
	}
	return remoteCommand{Agent: agent, Text: text}, nil
}

func (b *Bridge) ensureCodex(ctx context.Context) error {
	b.mu.Lock()
	c := b.codex
	runCtx := b.runCtx
	b.mu.Unlock()
	if c != nil && c.Alive() {
		return nil
	}
	if runCtx == nil {
		runCtx = context.Background()
	}
	nc := NewCodexClient(b.cfg.CodexPath, b.cfg.Cwd)
	if err := nc.Start(runCtx); err != nil {
		return err
	}
	acctCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	raw, err := nc.Account(acctCtx)
	cancel()
	if err != nil {
		nc.Close()
		return err
	}
	if !codexAccountIsChatGPT(raw) {
		nc.Close()
		return errors.New("Codex is not signed in with ChatGPT")
	}
	b.mu.Lock()
	b.codex = nc
	b.mu.Unlock()
	return nil
}

func (b *Bridge) initCodexThread(ctx context.Context) error {
	if err := b.ensureCodex(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	tid := b.state.CodexThreadID
	b.mu.Unlock()
	if tid != "" {
		if _, err := b.codex.Request(ctx, "thread/resume", map[string]any{"threadId": tid}); err == nil {
			return nil
		}
	}
	p := map[string]any{}
	if b.cfg.Cwd != "" {
		p["cwd"] = b.cfg.Cwd
	}
	raw, err := b.codex.Request(ctx, "thread/start", p)
	if err != nil {
		return err
	}
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Thread.ID == "" {
		return errors.New("thread/start returned no thread id")
	}
	b.mu.Lock()
	b.state.CodexThreadID = r.Thread.ID
	s := b.state
	b.mu.Unlock()
	return saveState(b.statePath, s)
}

func (b *Bridge) Run(ctx context.Context) {
	b.mu.Lock()
	b.runCtx = ctx
	b.mu.Unlock()
	if b.state.GmailBaselineUnix == 0 {
		b.mu.Lock()
		b.state.GmailBaselineUnix = time.Now().Unix()
		s := b.state
		b.mu.Unlock()
		_ = saveState(b.statePath, s)
		log.Printf("Gmail baseline established; old messages will not execute")
	}
	b.poll(ctx)

	// App Password mode implements IMAP IDLE, so Gmail can wake the bridge as
	// soon as a mailbox change arrives. Keep a 30-second fallback poll in case
	// an IDLE connection is dropped without a useful notification.
	if waiter, ok := b.gmail.(MailChangeWaiter); ok {
		wake := make(chan struct{}, 1)
		go func() {
			for ctx.Err() == nil {
				waitCtx, cancel := context.WithTimeout(ctx, 25*time.Minute)
				err := waiter.WaitForChange(waitCtx)
				cancel()
				if ctx.Err() != nil {
					return
				}
				if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					log.Printf("Gmail IMAP IDLE: %v", err)
					time.Sleep(time.Second)
				}
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}()
		fallback := time.NewTicker(30 * time.Second)
		defer fallback.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				b.poll(ctx)
			case <-fallback.C:
				b.poll(ctx)
			}
		}
	}

	// Gmail API/OAuth has no local mailbox IDLE channel. Poll quickly; true
	// Gmail API push would require the user's own Pub/Sub project, which this
	// bridge intentionally does not require.
	poll := b.cfg.Gmail.PollSeconds
	if poll < 1 {
		poll = 1
	}
	t := time.NewTicker(time.Duration(poll) * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.poll(ctx)
		}
	}
}

func (b *Bridge) processed(id string) bool {
	for _, x := range b.state.ProcessedMessageIDs {
		if x == id {
			return true
		}
	}
	return false
}
func (b *Bridge) markProcessed(id string) {
	b.state.ProcessedMessageIDs = append(b.state.ProcessedMessageIDs, id)
	if len(b.state.ProcessedMessageIDs) > 2000 {
		b.state.ProcessedMessageIDs = b.state.ProcessedMessageIDs[len(b.state.ProcessedMessageIDs)-2000:]
	}
	b.state.LastMessageID = id
}
func (b *Bridge) poll(ctx context.Context) {
	b.mu.Lock()
	if b.busy {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	ids, err := b.gmail.List(ctx)
	if err != nil {
		log.Printf("Gmail poll: %v", err)
		return
	}
	for idx := len(ids) - 1; idx >= 0; idx-- {
		id := ids[idx]
		b.mu.Lock()
		done := b.processed(id)
		baseline := b.state.GmailBaselineUnix
		b.mu.Unlock()
		if done {
			continue
		}
		m, err := b.gmail.Get(ctx, id)
		if err != nil {
			log.Printf("Gmail get %s: %v", id, err)
			continue
		}
		if !m.InternalDate.IsZero() && m.InternalDate.Unix() < baseline {
			b.mu.Lock()
			b.markProcessed(id)
			s := b.state
			b.mu.Unlock()
			_ = saveState(b.statePath, s)
			continue
		}
		raw, sender, ok := parseGoogleVoiceBody(m, b.cfg.GoogleVoice.AllowedFrom, b.cfg.GoogleVoice.RequiredSubjectPhrase)
		b.mu.Lock()
		b.markProcessed(id)
		s := b.state
		b.mu.Unlock()
		_ = saveState(b.statePath, s)
		if !ok {
			continue
		}
		rc, err := parseRemoteCommand(raw, b.cfg)
		if err != nil {
			log.Printf("Rejected remote SMS %s from %s: %v", id, sender, err)
			continue
		}
		rc.Sender = sender
		b.execute(ctx, m, rc)
	}
}

func (b *Bridge) composePrompt(command, agent, sender string) string {
	max := b.cfg.GoogleVoice.ReplyMaxChars
	if max <= 0 {
		max = 300
	}
	suffix := ""
	if b.cfg.GoogleVoice.SendReplyViaAgentBrowser {
		suffix = fmt.Sprintf(`\n\nREMOTE SMS RETURN-CHANNEL INSTRUCTIONS (MANDATORY):\nThis request came from an authenticated SMS sender: %s. Complete the requested work using your available tools. When the work is finished, send the completion update back to THIS EXACT PHONE NUMBER through Google Voice. Prefer an already-authenticated Google Voice tab/session. If the built-in browser is not signed in, use an available authenticated Chrome/browser integration instead. Open https://voice.google.com, select or search for the conversation with %s, verify the destination before sending, and send a concise result of at most %d characters. Do not send the result to any other number. Do not enter, reveal, or request passwords, recovery codes, or 2FA secrets. If browser-based Google Voice is unavailable or not already authenticated, do not attempt to sign in with credentials; simply return the concise result to the bridge so its Gmail reply fallback can deliver it. If and only if you actually confirmed that Google Voice sent the message to %s, end your final response with the exact marker SMS_BRIDGE_SENT.`, sender, sender, max, sender)
	}
	_ = agent
	return command + suffix
}

func (b *Bridge) execute(parent context.Context, m GmailMessage, rc remoteCommand) {
	b.mu.Lock()
	if b.busy {
		b.mu.Unlock()
		return
	}
	b.busy = true
	b.state.LastRunAt = time.Now()
	b.state.LastAgent = rc.Agent
	s := b.state
	b.mu.Unlock()
	_ = saveState(b.statePath, s)
	defer func() { b.mu.Lock(); b.busy = false; b.mu.Unlock() }()
	timeout := time.Duration(b.cfg.TurnTimeoutMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 90 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var final string
	var err error
	if rc.Status {
		b.mu.Lock()
		final = fmt.Sprintf("Bridge online. Codex thread: %v. Claude session: %v. Busy: %v", b.state.CodexThreadID != "", b.state.ClaudeSessionID != "", b.busy)
		b.mu.Unlock()
	} else if rc.New {
		if rc.Agent == "C" {
			err = b.newCodexThread(ctx)
			final = "New Codex conversation started."
		} else {
			b.mu.Lock()
			b.state.ClaudeSessionID = ""
			s := b.state
			b.mu.Unlock()
			err = saveState(b.statePath, s)
			final = "New Claude conversation started."
		}
	} else if rc.Agent == "A" {
		final, err = b.runClaude(ctx, rc.Text, rc.Sender)
	} else {
		final, err = b.runCodex(ctx, rc.Text, rc.Sender)
	}
	if err != nil {
		final = "FAILED: " + truncate(err.Error(), b.cfg.GoogleVoice.ReplyMaxChars)
	}
	if b.cfg.GoogleVoice.GmailReplyFallback && !strings.Contains(final, "SMS_BRIDGE_SENT") && m.ReplyTo != "" {
		msg := strings.ReplaceAll(final, "SMS_BRIDGE_SENT", "")
		msg = truncate(msg, b.cfg.GoogleVoice.ReplyMaxChars)
		if e := b.gmail.SendText(ctx, m.ReplyTo, msg); e != nil {
			log.Printf("Google Voice Gmail reply fallback: %v", e)
		}
	}
}

func (b *Bridge) newCodexThread(ctx context.Context) error {
	p := map[string]any{}
	if b.cfg.Cwd != "" {
		p["cwd"] = b.cfg.Cwd
	}
	raw, err := b.codex.Request(ctx, "thread/start", p)
	if err != nil {
		return err
	}
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Thread.ID == "" {
		return errors.New("no Codex thread id")
	}
	b.mu.Lock()
	b.state.CodexThreadID = r.Thread.ID
	s := b.state
	b.mu.Unlock()
	return saveState(b.statePath, s)
}
func (b *Bridge) runClaude(ctx context.Context, command, sender string) (string, error) {
	if b.claude == nil {
		return "", errors.New("Claude Code unavailable")
	}
	b.mu.Lock()
	sid := b.state.ClaudeSessionID
	b.mu.Unlock()
	res, nsid, err := b.claude.Run(ctx, sid, b.composePrompt(command, "A", sender))
	if err != nil {
		return "", err
	}
	if nsid != "" && nsid != sid {
		b.mu.Lock()
		b.state.ClaudeSessionID = nsid
		s := b.state
		b.mu.Unlock()
		_ = saveState(b.statePath, s)
	}
	return res, nil
}
func (b *Bridge) runCodex(ctx context.Context, command, sender string) (string, error) {
	if err := b.ensureCodex(ctx); err != nil {
		return "", err
	}
	acctCtx, acctCancel := context.WithTimeout(ctx, 10*time.Second)
	rawAcct, acctErr := b.codex.Account(acctCtx)
	acctCancel()
	if acctErr != nil || !codexAccountIsChatGPT(rawAcct) {
		return "", errors.New("Codex is not authenticated with Sign in with ChatGPT")
	}
	b.mu.Lock()
	tid := b.state.CodexThreadID
	b.mu.Unlock()
	if tid == "" {
		if err := b.initCodexThread(ctx); err != nil {
			return "", err
		}
		b.mu.Lock()
		tid = b.state.CodexThreadID
		b.mu.Unlock()
	}
	params := map[string]any{"threadId": tid, "input": []map[string]any{{"type": "text", "text": b.composePrompt(command, "C", sender)}}}
	if b.cfg.Codex.ApprovalPolicy != "" {
		params["approvalPolicy"] = b.cfg.Codex.ApprovalPolicy
	}
	raw, err := b.codex.Request(ctx, "turn/start", params)
	if err != nil {
		return "", err
	}
	var r struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Turn.ID == "" {
		return "", errors.New("turn/start returned no turn id")
	}
	turnID := r.Turn.ID
	final := ""
	for {
		select {
		case <-ctx.Done():
			return final, ctx.Err()
		case <-b.codex.done:
			b.mu.Lock()
			b.codex = nil
			b.mu.Unlock()
			return final, errors.New("Codex App Server stopped; it will be restarted on the next SMS")
		case n := <-b.codex.notifications:
			switch n.Method {
			case "item/completed":
				var p struct {
					TurnID string `json:"turnId"`
					Item   struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				}
				if json.Unmarshal(n.Params, &p) == nil && (p.TurnID == "" || p.TurnID == turnID) && p.Item.Type == "agentMessage" && p.Item.Text != "" {
					final = p.Item.Text
				}
			case "turn/completed":
				var p struct {
					Turn struct {
						ID     string `json:"id"`
						Status string `json:"status"`
						Error  *struct {
							Message string `json:"message"`
						} `json:"error"`
					} `json:"turn"`
				}
				if json.Unmarshal(n.Params, &p) == nil && p.Turn.ID == turnID {
					if p.Turn.Error != nil && p.Turn.Error.Message != "" {
						return final, errors.New(p.Turn.Error.Message)
					}
					return final, nil
				}
			}
		}
	}
}
func truncate(s string, n int) string {
	if n <= 0 {
		n = 300
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
