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
	"unicode"
)

// bridgeJob is one authenticated SMS command waiting for an agent turn.
type bridgeJob struct {
	msg GmailMessage
	cmd remoteCommand
}

type Bridge struct {
	cfg       Config
	statePath string
	gmail     MailClient
	codex     *CodexClient
	claude    *ClaudeClient
	activity  *ActivityLog
	mu        sync.Mutex
	runMu     sync.Mutex
	state     State
	busy      bool
	runCtx    context.Context

	// queue decouples reading the mailbox from running an agent turn. Turns can
	// last many minutes, and previously the single poll loop sat blocked inside
	// execute for the whole turn, so a text sent meanwhile was not even read
	// until the turn finished. poll now enqueues and returns; a worker drains
	// the queue one job at a time, preserving the one-turn-at-a-time invariant.
	queue    []bridgeJob
	queueSig chan struct{}

	// progress is the agent's most recent step, surfaced by the optional
	// progress heartbeat so a long turn reports something specific.
	progress string

	// processedSet indexes state.ProcessedMessageIDs for O(1) replay checks.
	processedSet map[string]struct{}
}

func NewBridge(cfg Config, statePath string, state State, g MailClient, c *CodexClient, a *ClaudeClient) *Bridge {
	return &Bridge{
		cfg: cfg, statePath: statePath, state: state,
		gmail: g, codex: c, claude: a,
		activity: activityLogForStatePath(statePath),
		queueSig: make(chan struct{}, 1),
	}
}

// enqueue adds a job and signals the worker without ever blocking the caller.
func (b *Bridge) enqueue(j bridgeJob) int {
	b.mu.Lock()
	b.queue = append(b.queue, j)
	depth := len(b.queue)
	b.mu.Unlock()
	select {
	case b.queueSig <- struct{}{}:
	default:
	}
	return depth
}

func (b *Bridge) dequeue() (bridgeJob, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queue) == 0 {
		return bridgeJob{}, false
	}
	j := b.queue[0]
	b.queue = b.queue[1:]
	return j, true
}

// drainQueue runs every queued job to completion, one at a time. Run calls it
// from a dedicated worker goroutine; tests call it directly after poll.
func (b *Bridge) drainQueue(ctx context.Context) {
	for ctx.Err() == nil {
		j, ok := b.dequeue()
		if !ok {
			return
		}
		b.execute(ctx, j.msg, j.cmd)
	}
}

func (b *Bridge) setProgress(step string) {
	b.mu.Lock()
	b.progress = strings.TrimSpace(step)
	b.mu.Unlock()
}

func (b *Bridge) currentProgress() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.progress
}

func (b *Bridge) event(level, stage, message, sender, agent, messageID string) {
	if b != nil && b.activity != nil {
		b.activity.Add(level, stage, message, sender, agent, messageID)
	}
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

func voiceGoogleLinkOnly(line string) bool {
	s := strings.ToLower(strings.TrimSpace(line))
	s = strings.Trim(s, "<>")
	return s == "https://voice.google.com" || s == "http://voice.google.com" || s == "voice.google.com"
}

func voiceFooterStart(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(l, "your account") ||
		strings.HasPrefix(l, "help center") ||
		strings.HasPrefix(l, "help forum") ||
		strings.HasPrefix(l, "to respond to this text message") ||
		strings.HasPrefix(l, "this email was sent to you because") ||
		strings.HasPrefix(l, "if you don't want to receive") ||
		strings.HasPrefix(l, "if you don’t want to receive") ||
		strings.HasPrefix(l, "google llc") ||
		strings.HasPrefix(l, "1600 amphitheatre")
}

// extractGoogleVoiceCommand extracts only the SMS body from the plain-text
// Google Voice notification. Real Voice mail currently starts with a standalone
// <https://voice.google.com> line, then the SMS, then a YOUR ACCOUNT/help/legal
// footer. The older parser kept that leading URL, causing the security-code
// parser to see the URL as token #1 and reject every real SMS.
func extractGoogleVoiceCommand(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var kept []string
	started := false
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if !started {
			if line == "" || strings.EqualFold(line, "Google Voice") || voiceGoogleLinkOnly(line) {
				continue
			}
			if voiceFooterStart(line) {
				break
			}
			started = true
			kept = append(kept, line)
			continue
		}
		if voiceFooterStart(line) {
			break
		}
		if line == "" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func parseGoogleVoiceBodyDetailed(m GmailMessage, allowed, requiredPhrase string) (string, string, bool, string) {
	fromLower := strings.ToLower(m.From)
	if !strings.Contains(fromLower, "voice-noreply@google.com") && !strings.Contains(fromLower, "@txt.voice.google.com") {
		return "", "", false, "sender is not a Google Voice notification address"
	}
	if !googleDKIMPassed(m.AuthenticationResults) {
		return "", "", false, "Google DKIM verification is missing or failed"
	}
	sender, ok := googleVoiceSender(m, requiredPhrase)
	if !ok {
		return "", "", false, "could not extract the SMS sender from trusted Google Voice headers"
	}
	if !allowedPhone(allowed, sender) {
		return "", sender, false, "sender is not on the allowed phone-number list"
	}
	cmd := extractGoogleVoiceCommand(m.Body)
	if cmd == "" {
		return "", sender, false, "Google Voice message contained no SMS command text"
	}
	return cmd, sender, true, ""
}

func parseGoogleVoiceBody(m GmailMessage, allowed, requiredPhrase string) (string, string, bool) {
	cmd, sender, ok, _ := parseGoogleVoiceBodyDetailed(m, allowed, requiredPhrase)
	return cmd, sender, ok
}

type remoteCommand struct {
	Agent  string
	Text   string
	Sender string
	New    bool
	Status bool
}

func parseRemoteCommand(raw string, cfg Config) (remoteCommand, error) {
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
	b.event("info", "bridge", "Background bridge started and is monitoring Gmail", "", "", "")

	// Agent turns run on their own worker so the mailbox loop below keeps
	// reading and acknowledging new texts during a long turn.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-b.queueSig:
				b.drainQueue(ctx)
			}
		}
	}()

	if b.state.GmailBaselineUnix == 0 {
		b.mu.Lock()
		b.state.GmailBaselineUnix = time.Now().Unix()
		s := b.state
		b.mu.Unlock()
		_ = saveState(b.statePath, s)
		log.Printf("Gmail baseline established; old messages will not execute")
		b.event("info", "gmail", "Mailbox baseline established; older messages will not execute", "", "", "")
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
					b.event("warn", "gmail", "IMAP IDLE connection had a problem; retrying: "+truncate(err.Error(), 220), "", "", "")
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

// processedSetLocked lazily indexes the on-disk checkpoint list. The slice is
// kept as-is for state.json compatibility; the map exists so the per-message
// lookup is O(1) instead of scanning up to 2000 entries for every candidate.
// Callers already hold b.mu.
func (b *Bridge) processedSetLocked() map[string]struct{} {
	if b.processedSet == nil {
		b.processedSet = make(map[string]struct{}, len(b.state.ProcessedMessageIDs)+16)
		for _, x := range b.state.ProcessedMessageIDs {
			b.processedSet[x] = struct{}{}
		}
	}
	return b.processedSet
}

func (b *Bridge) processed(id string) bool {
	_, ok := b.processedSetLocked()[id]
	return ok
}

func (b *Bridge) markProcessed(id string) {
	set := b.processedSetLocked()
	if _, dup := set[id]; !dup {
		b.state.ProcessedMessageIDs = append(b.state.ProcessedMessageIDs, id)
		set[id] = struct{}{}
	}
	if len(b.state.ProcessedMessageIDs) > 2000 {
		dropped := b.state.ProcessedMessageIDs[:len(b.state.ProcessedMessageIDs)-2000]
		b.state.ProcessedMessageIDs = b.state.ProcessedMessageIDs[len(b.state.ProcessedMessageIDs)-2000:]
		for _, x := range dropped {
			delete(set, x)
		}
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
		b.event("error", "gmail", "Mailbox check failed: "+truncate(err.Error(), 240), "", "", "")
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
			b.event("error", "gmail", "Could not read matching Gmail message: "+truncate(err.Error(), 220), "", "", id)
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
		b.event("info", "gmail", "New Google Voice candidate detected in Gmail", "", "", id)
		raw, sender, ok, reason := parseGoogleVoiceBodyDetailed(m, b.cfg.GoogleVoice.AllowedFrom, b.cfg.GoogleVoice.RequiredSubjectPhrase)
		b.mu.Lock()
		b.markProcessed(id)
		s := b.state
		b.mu.Unlock()
		_ = saveState(b.statePath, s)
		if !ok {
			b.event("warn", "security", "Message ignored: "+reason, sender, "", id)
			continue
		}
		b.event("success", "security", "Google Voice sender verified and allowed", sender, "", id)
		rc, err := parseRemoteCommand(raw, b.cfg)
		if err != nil {
			log.Printf("Rejected remote SMS %s from %s: %v", id, sender, err)
			b.event("warn", "security", "SMS rejected: "+err.Error(), sender, "", id)
			continue
		}
		rc.Sender = sender

		// STATUS needs no agent, so answer it inline. That keeps it instant even
		// while a long turn is running, instead of queueing behind it.
		if rc.Status {
			b.event("success", "routing", "STATUS answered directly", sender, "", id)
			b.deliver(ctx, m, rc, b.statusLine())
			continue
		}

		agentName := "Codex"
		if rc.Agent == "A" {
			agentName = "Claude"
		}
		b.event("success", "routing", "Authenticated SMS routed to "+agentName, sender, rc.Agent, id)
		depth := b.enqueue(bridgeJob{msg: m, cmd: rc})
		if b.cfg.GoogleVoice.ReplyAck {
			line := "✓ " + agentName + " working on it…"
			if depth > 1 {
				line = fmt.Sprintf("✓ Queued for %s (%d ahead)…", agentName, depth-1)
			}
			b.notify(ctx, m, line)
		}
	}
}

func (b *Bridge) statusLine() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	line := fmt.Sprintf("Bridge online. Codex thread: %v. Claude session: %v. Busy: %v.",
		b.state.CodexThreadID != "", b.state.ClaudeSessionID != "", b.busy)
	if n := len(b.queue); n > 0 {
		line += fmt.Sprintf(" Queued: %d.", n)
	}
	if step := b.progress; step != "" {
		line += " Now: " + truncate(step, 120)
	}
	return line
}

// composePrompt hands the SMS to the agent with as little framing as possible.
//
// FlipAi is a transport: it delivers the reply itself over the authenticated
// Google Voice email address, so the agent is never instructed to open a
// browser, find a conversation, or emit a delivery marker. Whatever the agent
// can do at the desktop it can still do here — including using the browser,
// when the user's own text asks for it.
//
// The command is fenced so untrusted SMS text is read as data rather than as
// instructions, and exactly one configurable line explains that the answer
// travels as a text message.
func (b *Bridge) composePrompt(command string) string {
	hint := strings.TrimSpace(b.cfg.GoogleVoice.ReplyStyleHint)
	if hint == "" {
		hint = defaultReplyStyleHint
	}
	return "<sms_command>\n" + strings.TrimSpace(command) + "\n</sms_command>\n\n" + hint
}

func googleVoiceReplyTarget(m GmailMessage) string {
	for _, candidate := range []string{m.ReplyTo, m.From} {
		if addr, err := safeGoogleVoiceReplyAddress(candidate); err == nil {
			return addr
		}
	}
	return ""
}

func (b *Bridge) execute(parent context.Context, m GmailMessage, rc remoteCommand) {
	// One agent turn at a time, enforced structurally. The old busy flag
	// returned early instead of waiting, which would now silently discard a
	// queued job rather than merely delaying it.
	b.runMu.Lock()
	defer b.runMu.Unlock()
	b.mu.Lock()
	b.busy = true
	b.progress = ""
	b.state.LastRunAt = time.Now()
	b.state.LastAgent = rc.Agent
	s := b.state
	b.mu.Unlock()
	_ = saveState(b.statePath, s)
	defer func() { b.mu.Lock(); b.busy = false; b.progress = ""; b.mu.Unlock() }()
	timeout := time.Duration(b.cfg.TurnTimeoutMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 90 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	// Optional heartbeat so a long turn reports in, the way watching the
	// desktop app does. Stops as soon as the turn returns.
	if b.cfg.GoogleVoice.ProgressUpdates && !rc.Status && !rc.New {
		stop := make(chan struct{})
		defer close(stop)
		go b.heartbeat(ctx, stop, m, rc)
	}

	var final string
	var err error
	if rc.Status {
		b.event("info", "agent", "STATUS command executing", rc.Sender, "", m.ID)
		final = b.statusLine()
	} else if rc.New {
		b.event("info", "agent", "Starting a new agent conversation", rc.Sender, rc.Agent, m.ID)
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
		b.event("info", "agent", "Claude command started", rc.Sender, "A", m.ID)
		final, err = b.runClaude(ctx, rc.Text, rc.Sender)
	} else {
		b.event("info", "agent", "Codex command started", rc.Sender, "C", m.ID)
		final, err = b.runCodex(ctx, rc.Text, rc.Sender)
	}
	if err != nil {
		b.event("error", "agent", "Agent failed: "+truncate(err.Error(), 240), rc.Sender, rc.Agent, m.ID)
		// Send one actionable sentence rather than a truncated JSON blob.
		final = "FAILED: " + truncate(friendlyAgentError(err), b.cfg.GoogleVoice.ReplyMaxChars)
	} else {
		b.event("success", "agent", "Agent completed successfully", rc.Sender, rc.Agent, m.ID)
	}

	// Delivery is unconditional and happens here, in Go. The agent is never
	// asked to send anything itself, so nothing in its output can change where
	// this reply goes. Use the parent context: the turn's own context may have
	// just expired, and the timeout notice still has to reach the phone.
	sendCtx, sendCancel := context.WithTimeout(parent, 2*time.Minute)
	defer sendCancel()
	b.deliver(sendCtx, m, rc, final)
}

// heartbeat texts a periodic "still working" line during a long turn, naming
// the agent's current step when one is known.
func (b *Bridge) heartbeat(ctx context.Context, stop <-chan struct{}, m GmailMessage, rc remoteCommand) {
	every := time.Duration(b.cfg.GoogleVoice.ProgressIntervalSeconds) * time.Second
	if every < 30*time.Second {
		every = 120 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	agentName := "Codex"
	if rc.Agent == "A" {
		agentName = "Claude"
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
			line := agentName + " still working…"
			if step := b.currentProgress(); step != "" {
				line += " " + truncate(step, 120)
			}
			b.notify(ctx, m, line)
			b.event("info", "reply", "Progress update texted to the sender", rc.Sender, rc.Agent, m.ID)
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
	res, nsid, err := b.claude.Run(ctx, sid, b.composePrompt(command))
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

// codexThreadIsGone reports whether a turn failed because the stored thread's
// on-disk rollout no longer exists, rather than for a transient reason. Codex
// answers "no rollout found for thread id …" with JSON-RPC code -32600.
func codexThreadIsGone(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if !strings.Contains(s, "resume codex thread") && !strings.Contains(s, "thread") {
		return false
	}
	return strings.Contains(s, "no rollout found") ||
		strings.Contains(s, "thread not found") ||
		strings.Contains(s, "-32600")
}

func (b *Bridge) runCodex(ctx context.Context, command, sender string) (string, error) {
	// Set when a dead conversation forced a fresh one, so the reply can say so
	// instead of silently losing the previous context.
	recovered := false
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
	params := map[string]any{"threadId": tid, "input": []map[string]any{{"type": "text", "text": b.composePrompt(command)}}}
	if b.cfg.Codex.ApprovalPolicy != "" {
		params["approvalPolicy"] = b.cfg.Codex.ApprovalPolicy
	}
	raw, err := b.codex.Request(ctx, "turn/start", params)
	if err != nil && codexThreadIsGone(err) {
		// The stored conversation's rollout is gone — Codex reinstalled, history
		// cleared, or a different CODEX_HOME. Without this, the saved thread id
		// stays poisoned and every future text fails the same way forever.
		b.event("warn", "agent", "Stored Codex conversation is gone; starting a new one", sender, "C", "")
		b.mu.Lock()
		b.state.CodexThreadID = ""
		s := b.state
		b.mu.Unlock()
		_ = saveState(b.statePath, s)
		if nerr := b.newCodexThread(ctx); nerr != nil {
			return "", nerr
		}
		b.mu.Lock()
		params["threadId"] = b.state.CodexThreadID
		b.mu.Unlock()
		recovered = true
		raw, err = b.codex.Request(ctx, "turn/start", params)
	}
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
				if json.Unmarshal(n.Params, &p) == nil && (p.TurnID == "" || p.TurnID == turnID) && p.Item.Text != "" {
					if p.Item.Type == "agentMessage" {
						final = p.Item.Text
					} else {
						// Any other completed item is a step worth naming in a
						// progress heartbeat.
						b.setProgress(p.Item.Text)
					}
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
					if recovered {
						final = "(previous Codex conversation was gone — started a new one)\n" + final
					}
					return final, nil
				}
			}
		}
	}
}

// splitReply breaks a long answer into numbered SMS parts instead of cutting it
// off. Truncating at ReplyMaxChars silently lost the end of any desktop-length
// answer, which defeats the point of getting the same result by text.
func splitReply(s string, max, maxParts int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if max <= 0 {
		max = 300
	}
	if maxParts < 1 {
		maxParts = 1
	}
	r := []rune(s)
	if len(r) <= max {
		return []string{s}
	}
	if maxParts == 1 {
		return []string{truncate(s, max)}
	}
	// Leave room for the "12/12 " prefix each part carries.
	body := max - 7
	if body < 20 {
		body = max
	}
	var chunks []string
	for len(r) > 0 && len(chunks) < maxParts {
		if len(r) <= body {
			chunks = append(chunks, strings.TrimSpace(string(r)))
			r = nil
			break
		}
		cut := body
		// Prefer breaking on whitespace so words survive the split.
		for i := body; i > body/2; i-- {
			if unicode.IsSpace(r[i]) {
				cut = i
				break
			}
		}
		chunks = append(chunks, strings.TrimSpace(string(r[:cut])))
		r = []rune(strings.TrimLeft(string(r[cut:]), " \t\r\n"))
	}
	if len(r) > 0 && len(chunks) > 0 {
		chunks[len(chunks)-1] += " …"
	}
	if len(chunks) == 1 {
		return chunks
	}
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = fmt.Sprintf("%d/%d %s", i+1, len(chunks), c)
	}
	return out
}

// deliver sends the agent's answer back as SMS by replying to the authenticated
// Google Voice address. This is the only delivery path: it runs in Go, so a
// prompt-injected SMS cannot redirect or suppress the reply.
func (b *Bridge) deliver(ctx context.Context, m GmailMessage, rc remoteCommand, text string) {
	target := googleVoiceReplyTarget(m)
	if target == "" {
		b.event("error", "reply", "Could not find a safe @txt.voice.google.com reply address", rc.Sender, rc.Agent, m.ID)
		return
	}
	parts := splitReply(text, b.cfg.GoogleVoice.ReplyMaxChars, b.cfg.GoogleVoice.MaxReplyParts)
	if len(parts) == 0 {
		return
	}
	for _, p := range parts {
		if err := b.gmail.SendText(ctx, target, p); err != nil {
			log.Printf("Google Voice reply: %v", err)
			b.event("error", "reply", "Google Voice reply failed: "+truncate(err.Error(), 220), rc.Sender, rc.Agent, m.ID)
			return
		}
	}
	msg := "Reply sent through Google Voice"
	if len(parts) > 1 {
		msg = fmt.Sprintf("Reply sent through Google Voice in %d parts", len(parts))
	}
	b.event("success", "reply", msg, rc.Sender, rc.Agent, m.ID)
}

// notify sends a single short status line (the ack or a progress heartbeat).
// Delivery failures are non-fatal: they must never derail the actual answer.
func (b *Bridge) notify(ctx context.Context, m GmailMessage, line string) {
	target := googleVoiceReplyTarget(m)
	if target == "" {
		return
	}
	if err := b.gmail.SendText(ctx, target, truncate(line, b.cfg.GoogleVoice.ReplyMaxChars)); err != nil {
		log.Printf("Google Voice status text: %v", err)
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
