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
	msg  GmailMessage
	cmd  remoteCommand
	done chan struct{}
}

type Bridge struct {
	cfg       Config
	statePath string
	gmail     MailClient
	codex     *CodexClient
	claude    *ClaudeClient

	// live is the supervised Claude Code session used by live mode. It is nil
	// in per-message mode, which is both the default and the fallback, so every
	// use is guarded rather than assumed.
	live *ClaudeLiveClient

	// onAgentResult records the outcome of a real agent turn as that agent's
	// health check. Without it the Agents page reports the last time someone
	// pressed Test and nothing else, so a working bridge reads as disconnected
	// for as long as nobody presses it again.
	onAgentResult func(agent string, ok bool, detail string)

	activity *ActivityLog
	mu       sync.Mutex
	runMu    sync.Mutex
	state    State
	busy     bool
	runCtx   context.Context

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

	// paused stops the poll loop from claiming new texts. Nothing is discarded
	// while paused: unread mail simply stays unread until FlipAi resumes.
	paused bool

	// lastPollAt/lastPollErr record how the most recent mailbox check went so
	// the desktop UI can report a real "last sync" instead of assuming one.
	lastPollAt  time.Time
	lastPollErr string

	// processedSet indexes state.ProcessedMessageIDs for O(1) replay checks.
	processedSet map[string]struct{}
}

func NewBridge(cfg Config, statePath string, state State, g MailClient, c *CodexClient, a *ClaudeClient) *Bridge {
	return &Bridge{
		cfg: cfg, statePath: statePath, state: state,
		gmail: g, codex: c, claude: a,
		activity: activityLogForStatePath(statePath),
		queueSig: make(chan struct{}, 1),
		paused:   cfg.Paused,
	}
}

// SetPaused takes effect on the next mailbox check, with no restart. A paused
// bridge finishes the turn it is already running rather than abandoning it.
func (b *Bridge) SetPaused(paused bool) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.paused = paused
	b.mu.Unlock()
}

func (b *Bridge) Paused() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.paused
}

// pollStatus reports when the mailbox was last checked and whether that check
// succeeded, for the Connections and Home pages.
func (b *Bridge) pollStatus() (time.Time, string) {
	if b == nil {
		return time.Time{}, ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastPollAt, b.lastPollErr
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
		if j.done != nil {
			close(j.done)
		}
	}
}

func (b *Bridge) sendReceipt(ctx context.Context, m GmailMessage, rc remoteCommand, depth int) {
	agentName := remoteCommandDisplayName(rc)
	line := "✓ " + agentName + " working on it…"
	if depth > 1 {
		line = fmt.Sprintf("✓ Queued for %s (%d ahead)…", agentName, depth-1)
	}
	b.notify(ctx, m, line)
}

func (b *Bridge) scheduleReceipt(ctx context.Context, m GmailMessage, rc remoteCommand, depth int, done <-chan struct{}) {
	settings := agentSettings(b.cfg, rc.Agent)
	if !settings.ackEnabled() {
		return
	}
	delay := settings.ackDelay()
	if delay <= 0 {
		b.sendReceipt(ctx, m, rc, depth)
		return
	}
	go func() {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-t.C:
			// If completion and the timer became ready together, completion wins.
			select {
			case <-done:
				return
			default:
			}
			b.sendReceipt(ctx, m, rc, depth)
		}
	}()
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

// timedEvent is event plus how long the reported step took, so the Activity
// page can show a real duration for the work FlipAi measures itself.
func (b *Bridge) timedEvent(level, stage, message, sender, agent, messageID string, took time.Duration) {
	if b != nil && b.activity != nil {
		b.activity.AddTimed(level, stage, message, sender, agent, messageID, took)
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
	if cmd == "" && !hasSupportedMailAttachments(m.Attachments) {
		return "", sender, false, "Google Voice message contained no SMS command text or supported attachment"
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

// parseRemoteCommand reads one authenticated text for the agent the sending
// number belongs to. The number decides the agent, so a prefix can only agree
// with that choice or be refused -- there is no longer a shared allowlist for a
// prefix to steer.
func parseRemoteCommand(raw string, cfg Config, agent string) (remoteCommand, error) {
	if agent != "A" && agent != "C" {
		agent = "C"
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return remoteCommand{}, errors.New("empty command")
	}
	settings := agentSettings(cfg, agent)
	rest := raw
	if settings.RequireCode {
		f := strings.Fields(raw)
		if len(f) < 2 {
			return remoteCommand{}, fmt.Errorf("missing the %s security code or the command", agentDisplayName(agent))
		}
		if !verifyAgentCode(settings, f[0]) {
			return remoteCommand{}, fmt.Errorf("invalid security code for %s", agentDisplayName(agent))
		}
		rest = strings.TrimSpace(strings.TrimPrefix(raw, f[0]))
	}
	if strings.EqualFold(rest, "STATUS") {
		return remoteCommand{Status: true}, nil
	}

	codexPrefix := configuredCodexPrefix(cfg)
	claudePrefix := configuredClaudePrefix(cfg)
	newSession := configuredNewSessionCommand(cfg)

	// The new-session word is configurable too. It works by itself, or after the
	// prefix of the agent this number reaches.
	if strings.EqualFold(strings.TrimSpace(rest), newSession) {
		return remoteCommand{Agent: agent, New: true}, nil
	}
	if isAgentNewSession(rest, codexPrefix, newSession) {
		if agent != "C" {
			return remoteCommand{}, wrongAgentForNumber(agent, "C")
		}
		return remoteCommand{Agent: "C", New: true}, nil
	}
	if isAgentNewSession(rest, claudePrefix, newSession) {
		if agent != "A" {
			return remoteCommand{}, wrongAgentForNumber(agent, "A")
		}
		return remoteCommand{Agent: "A", New: true}, nil
	}

	text := rest
	if tail, ok := stripAgentCommandPrefix(rest, codexPrefix); ok {
		if agent != "C" {
			return remoteCommand{}, wrongAgentForNumber(agent, "C")
		}
		text = tail
	} else if tail, ok := stripAgentCommandPrefix(rest, claudePrefix); ok {
		if agent != "A" {
			return remoteCommand{}, wrongAgentForNumber(agent, "A")
		}
		text = tail
	}
	if text == "" {
		return remoteCommand{}, errors.New("empty command")
	}
	return remoteCommand{Agent: agent, Text: text}, nil
}

func wrongAgentForNumber(reaches, asked string) error {
	return fmt.Errorf("this number reaches %s, so it cannot address %s; allow the number under %s if that is where it should go",
		agentDisplayName(reaches), agentDisplayName(asked), agentDisplayName(asked))
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
	nc := NewCodexClient(b.cfg.CodexPath, b.cfg.codexWorkingDir())
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
	if cwd := b.cfg.codexWorkingDir(); cwd != "" {
		p["cwd"] = cwd
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
					log.Printf("Gmail IDLE: %v", err)
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

	poll := b.cfg.Gmail.PollSeconds
	if poll < 1 {
		poll = 5
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

func (b *Bridge) poll(ctx context.Context) {
	b.mu.Lock()
	paused := b.paused
	b.mu.Unlock()
	if paused {
		b.mu.Lock()
		b.lastPollAt = time.Now()
		b.lastPollErr = "paused"
		b.mu.Unlock()
		return
	}
	b.mu.Lock()
	if b.processedSet == nil {
		b.processedSet = make(map[string]struct{}, len(b.state.ProcessedMessageIDs))
		for _, id := range b.state.ProcessedMessageIDs {
			b.processedSet[id] = struct{}{}
		}
	}
	b.mu.Unlock()

	since := b.state.GmailBaselineUnix
	if since == 0 {
		since = time.Now().Unix()
	}
	msgs, err := b.gmail.SearchUnreadVoice(ctx, since)
	b.mu.Lock()
	b.lastPollAt = time.Now()
	if err != nil {
		b.lastPollErr = err.Error()
	} else {
		b.lastPollErr = ""
	}
	b.mu.Unlock()
	if err != nil {
		log.Printf("Gmail search: %v", err)
		b.event("error", "gmail", "Gmail search failed: "+truncate(err.Error(), 220), "", "", "")
		return
	}
	for _, m := range msgs {
		id := m.ID
		b.mu.Lock()
		_, seen := b.processedSet[id]
		b.mu.Unlock()
		if seen {
			continue
		}
		b.event("info", "gmail", "New Google Voice candidate detected in Gmail", "", "", id)
		raw, sender, ok, reason := parseGoogleVoiceBodyDetailed(m, b.cfg.GoogleVoice.AllowedFrom, b.cfg.GoogleVoice.RequiredSubjectPhrase)
		if !ok {
			b.event("warn", "security", "Google Voice candidate ignored: "+reason, sender, "", id)
			continue
		}
		b.event("success", "security", "Google Voice sender verified and allowed", sender, "", id)
		senderAgent, phone, allowed := agentForSender(b.cfg, sender)
		if !allowed {
			b.event("warn", "security", "SMS ignored: this number is not allowed on any agent", sender, "", id)
			continue
		}
		if !phone.AllowsSMS() {
			b.event("warn", "security", "SMS ignored: this number is allowed for calls only", sender, "", id)
			continue
		}
		sticky := b.stickySMSAgent(sender)
		rc, err := parseRemoteCommandForMessageSticky(raw, b.cfg, senderAgent, sticky, m)
		if err != nil {
			log.Printf("Rejected remote SMS %s from %s: %v", id, sender, err)
			b.event("warn", "security", "SMS rejected: "+err.Error(), sender, "", id)
			b.notify(ctx, m, truncate(err.Error(), 300))
			continue
		}
		rc.Sender = sender
		if !rc.Status && rc.Agent != "" {
			routeID := explicitSMSRoute(raw, b.cfg)
			if routeID == "" {
				routeID = decodeStickySMSRoute(sticky)
			}
			if routeID == "" {
				routeID = defaultSMSRouteForAgent(rc.Agent)
			}
			if err := b.rememberStickySMSRoute(sender, routeID); err != nil {
				b.event("warn", "routing", "Could not persist the selected SMS route: "+truncate(err.Error(), 180), sender, rc.Agent, id)
			}
		}

		// STATUS needs no agent, so answer it inline. That keeps it instant even
		// while a long turn is running, instead of queueing behind it.
		if rc.Status {
			b.event("success", "routing", "STATUS answered directly", sender, "", id)
			b.deliver(ctx, m, rc, b.statusLine())
			continue
		}

		agentName := remoteCommandDisplayName(rc)
		b.event("success", "routing", "Authenticated SMS routed to "+agentName, sender, rc.Agent, id)
		doneCh := make(chan struct{})
		depth := b.enqueue(bridgeJob{msg: m, cmd: rc, done: doneCh})
		b.scheduleReceipt(ctx, m, rc, depth, doneCh)
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
// instructions, and exactly one configurable block explains that the answer
// travels as a text message. Each agent can carry its own wording — Codex and
// Claude answer a text differently — and an agent with no wording of its own
// falls back to the shared line.
func (b *Bridge) composePrompt(agent, command string) string {
	hint := strings.TrimSpace(b.cfg.replyStyleHintFor(agent))
	command = strings.TrimSpace(command)
	if hint == "" {
		return command
	}
	return command + "\n\n" + hint
}

func (b *Bridge) execute(parent context.Context, m GmailMessage, rc remoteCommand) {
	// One agent turn at a time, enforced structurally. The old busy flag
	// returned early instead of waiting, which would now silently discard a
	// queued job rather than merely delaying it.
	b.runMu.Lock()
	defer b.runMu.Unlock()
	startedAt := time.Now()
	b.mu.Lock()
	b.busy = true
	b.progress = ""
	b.state.LastRunAt = time.Now()
	b.state.LastAgent = rc.Agent
	s := b.state
	b.mu.Unlock()
	_ = saveState(b.statePath, s)
	defer func() { b.mu.Lock(); b.busy = false; b.progress = ""; b.mu.Unlock() }()
	// Agent/model turns have no elapsed-time deadline. Long browser work, Codex,
	// Claude Code, research, and image generation may legitimately run for many
	// minutes. The turn ends only when the provider completes/fails or when the
	// parent app context is cancelled during a real shutdown.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	// A NEW modifier may be reset-only (OW NEW:) or reset-and-run
	// (OW NEW: research this). Treat the latter as a real agent turn for
	// attachments, progress, health, and the final reply.
	freshTurn := rc.New && remoteCommandHasTurn(rc)

	// Optional heartbeat so a long turn reports in, the way watching the
	// desktop app does. Stops as soon as the turn returns.
	if agentSettings(b.cfg, rc.Agent).progressEnabled() && !rc.Status && (!rc.New || freshTurn) {
		stop := make(chan struct{})
		defer close(stop)
		go b.heartbeat(ctx, stop, m, rc)
	}

	var inbound []InboundAttachment
	var cleanupInbound func()
	var prepErr error
	if !rc.Status && (!rc.New || freshTurn) && len(m.Attachments) > 0 {
		inbound, cleanupInbound, prepErr = prepareInboundAttachments(m.Attachments)
		if cleanupInbound != nil {
			defer cleanupInbound()
		}
	}

	var final string
	var err error
	if prepErr != nil {
		err = prepErr
	} else if rc.Status {
		b.event("info", "agent", "STATUS command executing", rc.Sender, "", m.ID)
		final = b.statusLine()
	} else if freshTurn {
		b.event("info", "agent", "Starting a fresh agent conversation and first turn", rc.Sender, rc.Agent, m.ID)
		final, err = b.runFreshAgentTurn(ctx, rc, inbound)
	} else if rc.New && remoteCommandHasBrowserMode(rc) {
		b.event("info", "agent", "Starting a new browser-mode conversation", rc.Sender, rc.Agent, m.ID)
		final, err = b.resetAgentConversation(ctx, rc)
	} else if rc.New {
		b.event("info", "agent", "Starting a new agent conversation", rc.Sender, rc.Agent, m.ID)
		switch rc.Agent {
		case "C":
			err = b.newCodexThread(ctx)
			final = "New Codex conversation started."
		case "G":
			err = b.newChatGPTConversation(ctx)
			final = "New ChatGPT conversation started."
		case "H":
			err = b.newClaudeChatConversation(ctx)
			final = "New Claude Chat conversation started."
		case "M":
			err = b.newGeminiChatConversation(ctx)
			final = "New Gemini Chat conversation started."
		case "X":
			err = b.newGrokChatConversation(ctx)
			final = "New Grok Chat conversation started."
		case "P":
			err = b.newCopilotChatConversation(ctx)
			final = "New Microsoft Copilot Chat conversation started."
		default:
			// Claude sessions are created by the CLI on the turn that uses them.
			b.startNewClaudeSession()
			final = "New Claude conversation started."
		}
	} else if rc.Agent == "A" {
		b.event("info", "agent", "Claude command started", rc.Sender, "A", m.ID)
		final, err = b.runClaudeWithAttachments(ctx, rc.Text, rc.Sender, inbound)
	} else if isBrowserChatAgent(rc.Agent) && len(inbound) > 0 {
		b.event("info", "agent", agentDisplayName(rc.Agent)+" image command started", rc.Sender, rc.Agent, m.ID)
		final, err = b.runBrowserChatSMSWithAttachments(ctx, rc.Agent, rc.Text, inbound)
	} else if rc.Agent == "G" {
		b.event("info", "agent", "ChatGPT Chat command started", rc.Sender, "G", m.ID)
		final, err = b.runChatGPTSMS(ctx, rc.Text)
	} else if rc.Agent == "H" {
		b.event("info", "agent", "Claude Chat command started", rc.Sender, "H", m.ID)
		final, err = b.runClaudeChatSMS(ctx, rc.Text)
	} else if rc.Agent == "M" {
		b.event("info", "agent", "Gemini Chat command started", rc.Sender, "M", m.ID)
		final, err = b.runGeminiChatSMS(ctx, rc.Text)
	} else if rc.Agent == "X" {
		b.event("info", "agent", "Grok Chat command started", rc.Sender, "X", m.ID)
		final, err = b.runGrokChatSMS(ctx, rc.Text)
	} else if rc.Agent == "P" {
		b.event("info", "agent", "Microsoft Copilot Chat command started", rc.Sender, "P", m.ID)
		final, err = b.runCopilotChatSMS(ctx, rc.Text)
	} else {
		b.event("info", "agent", "Codex command started", rc.Sender, "C", m.ID)
		final, err = b.runCodexWithAttachments(ctx, rc.Text, rc.Sender, inbound)
	}
	if err != nil {
		b.timedEvent("error", "agent", "Agent failed: "+truncate(err.Error(), 240), rc.Sender, rc.Agent, m.ID, time.Since(startedAt))
		// Send one actionable sentence rather than a truncated JSON blob.
		final = "FAILED: " + truncate(friendlyAgentError(err), b.cfg.GoogleVoice.ReplyMaxChars)
	} else {
		b.timedEvent("success", "agent", "Agent completed successfully", rc.Sender, rc.Agent, m.ID, time.Since(startedAt))
	}
	// A real turn is the most authoritative health signal there is, better than
	// any probe: it is the exact work the Agents page claims to describe. Status
	// Reset-only commands never reach an agent, but NEW plus a prompt does.
	if !rc.Status && (!rc.New || freshTurn) {
		if err != nil {
			b.recordAgentResult(rc.Agent, false, friendlyAgentError(err))
		} else {
			b.recordAgentResult(rc.Agent, true, "answered an SMS turn")
		}
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
	// Each agent can set its own cadence; progressIntervalFor falls back to the
	// shared Phone setting and applies the same floor.
	every := b.cfg.progressIntervalFor(rc.Agent)
	t := time.NewTicker(every)
	defer t.Stop()
	agentName := remoteCommandDisplayName(rc)
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
	if cwd := b.cfg.codexWorkingDir(); cwd != "" {
		p["cwd"] = cwd
	}
	if err := b.ensureCodex(ctx); err != nil {
		return err
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

const claudeSessionLost = "Previous Claude conversation was unavailable, so a new one was started. "

func parseClaudeEvent(line string) (string, string) {
	var e struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Result  string `json:"result"`
		Message struct {
			Content []struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Name  string `json:"name"`
				Input any    `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &e) != nil {
		return "", ""
	}
	if e.Type == "result" && e.Result != "" {
		return "final", e.Result
	}
	if e.Type == "assistant" {
		for _, c := range e.Message.Content {
			if c.Type == "text" && c.Text != "" {
				return "final", c.Text
			}
			if c.Type == "tool_use" && c.Name != "" {
				return "progress", "Using " + c.Name
			}
		}
	}
	return "", ""
}

func (b *Bridge) runClaude(ctx context.Context, command, sender string) (string, error) {
	return b.runClaudeWithAttachments(ctx, command, sender, nil)
}

func claudeSessionArgs(cfg Config, prompt string, resume bool, sessionID string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	mode := strings.TrimSpace(cfg.Claude.PermissionMode)
	if mode == "" {
		mode = claudeFullAccess
	}
	args = append(args, "--permission-mode", mode)
	if cfg.Claude.UseChrome {
		args = append(args, "--chrome")
	}
	if resume && sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	args = append(args, prompt)
	return args
}

func newClaudeSessionName(now time.Time) string {
	return "Phone " + now.Local().Format("Jan 2 3:04 PM")
}

func (b *Bridge) prepareClaudeConversation(forceNew bool) (resume bool, sessionID, sessionName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if forceNew {
		b.state.ClaudeSessionID = ""
		b.state.ClaudeSessionName = newClaudeSessionName(time.Now())
	}
	if b.state.ClaudeSessionID != "" {
		return true, b.state.ClaudeSessionID, b.state.ClaudeSessionName
	}
	if b.state.ClaudeSessionName == "" {
		b.state.ClaudeSessionName = newClaudeSessionName(time.Now())
	}
	return false, "", b.state.ClaudeSessionName
}

func (b *Bridge) updateClaudeSession(id, name string) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return
	}
	b.mu.Lock()
	b.state.ClaudeSessionID = id
	if name != "" {
		b.state.ClaudeSessionName = name
	}
	s := b.state
	b.mu.Unlock()
	_ = saveState(b.statePath, s)
}

func (b *Bridge) startNewClaudeSession() string {
	name := newClaudeSessionName(time.Now())
	b.mu.Lock()
	live := b.live
	b.state.ClaudeSessionID = ""
	b.state.ClaudeSessionName = name
	s := b.state
	b.mu.Unlock()
	if live != nil {
		live.Stop()
	}
	_ = saveState(b.statePath, s)
	return name
}

// runClaudeWithAttachments prefers the supervised live conversation when the
// user enabled it, but falls back to the original per-message CLI if live mode
// cannot run on this machine. The fallback is deliberate: an SMS command should
// still work rather than fail just because Remote Control is unavailable.
func (b *Bridge) runClaudeWithAttachments(ctx context.Context, command, sender string, inbound []InboundAttachment) (string, error) {
	prompt := b.composePrompt("A", command)
	if b.claudeLiveEnabled() {
		if live, err := b.ensureClaudeLive(ctx); err == nil {
			return live.Turn(ctx, prompt, inbound)
		} else {
			b.event("warn", "agent", "Claude live mode unavailable; falling back to per-message mode: "+truncate(err.Error(), 220), sender, "A", "")
		}
	}
	resume, sessionID, name := b.prepareClaudeConversation(false)
	final, err := b.runClaudeProcess(ctx, claudeSessionArgs(b.cfg, prompt, resume, sessionID), sender, inbound)
	if err == nil {
		return final, nil
	}
	if resume && isClaudeSessionError(err) {
		b.event("warn", "agent", "Stored Claude conversation is gone; starting a new one", sender, "A", "")
		b.startNewClaudeSession()
		final, retryErr := b.runClaudeProcess(ctx, claudeSessionArgs(b.cfg, prompt, false, ""), sender, inbound)
		if retryErr == nil {
			return claudeSessionLost + final, nil
		}
		return "", retryErr
	}
	return "", err
}

func (b *Bridge) runClaudeProcess(ctx context.Context, args []string, sender string, inbound []InboundAttachment) (string, error) {
	if err := b.ensureClaude(ctx); err != nil {
		return "", err
	}
	work := b.cfg.claudeWorkingDir()
	args = claudeArgsWithInboundAttachments(args, inbound, work)
	proc, err := b.claude.Start(ctx, args...)
	if err != nil {
		return "", err
	}
	defer proc.Wait()
	var final string
	for {
		line, err := proc.Next(ctx)
		if err != nil {
			if errors.Is(err, ioEOF) {
				if final == "" {
					return "", errors.New("Claude Code returned no reply")
				}
				return final, nil
			}
			return "", err
		}
		kind, text := parseClaudeEvent(line)
		switch kind {
		case "progress":
			b.setProgress(text)
		case "final":
			final = text
			b.captureClaudeSession(line, "")
		}
	}
}

func isClaudeSessionError(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no conversation found") || strings.Contains(s, "session") && strings.Contains(s, "not found")
}

func (b *Bridge) captureClaudeSession(line, fallback string) {
	var e struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal([]byte(line), &e) == nil && e.SessionID != "" {
		b.mu.Lock()
		name := b.state.ClaudeSessionName
		b.mu.Unlock()
		b.updateClaudeSession(e.SessionID, name)
		return
	}
	if fallback == "" {
		return
	}
	b.mu.Lock()
	if b.state.ClaudeSessionID == "" {
		b.state.ClaudeSessionID = fallback
		s := b.state
		b.mu.Unlock()
		_ = saveState(b.statePath, s)
		return
	}
	b.mu.Unlock()
}

func (b *Bridge) newCodexThreadWithSender(ctx context.Context, sender string) error {
	_ = sender
	return b.newCodexThread(ctx)
}

func (b *Bridge) runCodex(ctx context.Context, command, sender string) (string, error) {
	return b.runCodexWithAttachments(ctx, command, sender, nil)
}

func (b *Bridge) runCodexWithAttachments(ctx context.Context, command, sender string, inbound []InboundAttachment) (string, error) {
	if err := b.initCodexThread(ctx); err != nil {
		return "", err
	}
	prompt := b.composePrompt("C", command)
	input := codexTurnInput(prompt, inbound)
	b.mu.Lock()
	thread := b.state.CodexThreadID
	b.mu.Unlock()
	params := map[string]any{
		"threadId": thread,
		"input":    input,
	}
	var final string
	restarted := false
	for {
		raw, err := b.codex.Request(ctx, "turn/start", params)
		if err != nil {
			if !restarted && codexThreadMissing(err) {
				b.event("warn", "agent", "Stored Codex conversation is gone; starting a new one", sender, "C", "")
				if nerr := b.newCodexThread(ctx); nerr != nil {
					return "", nerr
				}
				b.mu.Lock()
				params["threadId"] = b.state.CodexThreadID
				b.mu.Unlock()
				restarted = true
				continue
			}
			return "", err
		}
		var started struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(raw, &started) != nil || started.Turn.ID == "" {
			return "", errors.New("turn/start returned no turn id")
		}
		turnID := started.Turn.ID
		for {
			n, err := b.codex.Next(ctx)
			if err != nil {
				return final, errors.New("Codex App Server stopped; it will be restarted on the next SMS")
			}
			if n.Method == "item/started" || n.Method == "item/completed" {
				var p struct {
					ThreadID string `json:"threadId"`
					TurnID   string `json:"turnId"`
					Item     struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				}
				_ = json.Unmarshal(n.Params, &p)
				if p.ThreadID != params["threadId"] || p.TurnID != turnID {
					continue
				}
				if n.Method == "item/started" {
					if p.Item.Type == "reasoning" && p.Item.Text != "" {
						// A reasoning item is model-authored progress and is safe to show in the
						// progress heartbeat.
						b.setProgress(p.Item.Text)
					}
					continue
				}
				if p.Item.Type == "agent_message" && p.Item.Text != "" {
					final = p.Item.Text
				}
				continue
			}
			if n.Method == "turn/completed" {
				var p struct {
					ThreadID string `json:"threadId"`
					Turn     struct {
						ID     string `json:"id"`
						Status string `json:"status"`
						Error  *struct {
							Message string `json:"message"`
						} `json:"error"`
					} `json:"turn"`
				}
				_ = json.Unmarshal(n.Params, &p)
				if p.ThreadID != params["threadId"] || p.Turn.ID != turnID {
					continue
				}
				if p.Turn.Status == "failed" || p.Turn.Error != nil {
					if !restarted && p.Turn.Error != nil && codexThreadMissing(errors.New(p.Turn.Error.Message)) {
						b.event("warn", "agent", "Stored Codex conversation is gone; starting a new one", sender, "C", "")
						if nerr := b.newCodexThread(ctx); nerr != nil {
							return "", nerr
						}
						b.mu.Lock()
						params["threadId"] = b.state.CodexThreadID
						b.mu.Unlock()
						restarted = true
						break
					}
					if p.Turn.Error != nil && p.Turn.Error.Message != "" {
						return final, errors.New(p.Turn.Error.Message)
					}
					return final, errors.New("Codex turn failed")
				}
				if restarted && final != "" {
					final = "(previous Codex conversation was gone — started a new one)\n" + final
				}
				if final == "" {
					final = "Codex completed the turn."
				}
				return final, nil
			}
		}
	}
}

func codexThreadMissing(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "thread") && (strings.Contains(s, "not found") || strings.Contains(s, "unknown") || strings.Contains(s, "no such"))
}

func agentDisplayNameFromMarker(agent string) string { return agentDisplayName(agent) }

func splitReply(text string, max, maxParts int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if max < 20 {
		max = 300
	}
	if maxParts < 1 {
		maxParts = 1
	}
	r := []rune(text)
	var chunks []string
	for len(r) > 0 && len(chunks) < maxParts {
		if len(r) <= max {
			chunks = append(chunks, string(r))
			r = nil
			break
		}
		cut := max
		for i := max; i > max/2; i-- {
			if unicode.IsSpace(r[i-1]) {
				cut = i - 1
				break
			}
		}
		chunks = append(chunks, strings.TrimSpace(string(r[:cut])))
		r = r[cut:]
		for len(r) > 0 && unicode.IsSpace(r[0]) {
			r = r[1:]
		}
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
	startedAt := time.Now()
	for _, p := range parts {
		if err := sendThreadedVoiceReply(ctx, b.gmail, m, p); err != nil {
			log.Printf("Google Voice reply: %v", err)
			b.timedEvent("error", "reply", "Google Voice reply failed: "+truncate(err.Error(), 220), rc.Sender, rc.Agent, m.ID, time.Since(startedAt))
			return
		}
	}
	msg := "Reply sent through Google Voice"
	if len(parts) > 1 {
		msg = fmt.Sprintf("Reply sent through Google Voice in %d parts", len(parts))
	}
	b.timedEvent("success", "reply", msg, rc.Sender, rc.Agent, m.ID, time.Since(startedAt))
}

// notify sends a single short status line (the ack or a progress heartbeat).
// Delivery failures are non-fatal: they must never derail the actual answer.
func (b *Bridge) notify(ctx context.Context, m GmailMessage, line string) {
	target := googleVoiceReplyTarget(m)
	if target == "" {
		return
	}
	if err := sendThreadedVoiceReply(ctx, b.gmail, m, truncate(line, b.cfg.GoogleVoice.ReplyMaxChars)); err != nil {
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
