package main

import (
	"os"
	"strings"
	"time"
)

// hostStartedAt is set when the background host boots so the Advanced page can
// report a real uptime instead of an estimate.
var hostStartedAt = time.Now()

// uiStatus is the single snapshot every page reads. Collecting it in one place
// keeps each page honest: a tile can only claim what this struct actually knows.
type uiStatus struct {
	Version string
	Listen  string
	DataDir string
	Uptime  time.Duration

	Running bool
	Paused  bool
	Busy    bool

	GmailReady       bool
	GmailMethod      string
	GmailMethodLabel string
	GmailEmail       string
	GmailCheck       Check
	LastPollAt       time.Time
	LastPollErr      string
	SubjectPhrase    string

	CodexCheck     Check
	ClaudeCheck    Check
	CodexPath      string
	ClaudePath     string
	CodexResolved  string
	ClaudeResolved string
	CodexFound     bool
	ClaudeFound    bool
	HasClaudeToken bool
	PermissionMode string

	// PermissionModeLabel names the access level in the same plain words the
	// Codex card uses, so the two agents can be compared at a glance.
	PermissionModeLabel string

	// ClaudeUseChrome reports whether SMS turns pass --chrome.
	ClaudeUseChrome bool

	// ChromeTokenNotice explains a machine where Chrome is switched on but the
	// stored token is the only sign-in, which Claude Code answers by turning
	// Chrome off. Empty when there is nothing to warn about.
	ChromeTokenNotice string

	// ClaudeConn* describe which credential FlipAi is connected with, because
	// that single fact decides whether a text can drive Chrome or reach
	// claude.ai/code. ClaudeConnNeedsSignIn marks the states Connect Claude
	// fixes, so the page can put the button where the problem is.
	ClaudeConnKind        string
	ClaudeConnLabel       string
	ClaudeConnDetail      string
	ClaudeConnChromeReady bool
	ClaudeConnNeedsSignIn bool

	// ClaudeSessionMode is the configured mode, and ClaudeSessionModeLabel names
	// it in the same plain words the access level uses.
	// ClaudeProgressInterval and CodexProgressInterval are the per-agent
	// heartbeat overrides, 0 meaning "follow the shared Phone setting".
	ClaudeProgressInterval int
	CodexProgressInterval  int

	ClaudeSessionMode      string
	ClaudeSessionModeLabel string

	// The SMS framing FlipAi puts after every command. SharedReplyStyle is the
	// fallback both agents use; the two Custom flags say whether an agent has
	// wording of its own, so the editor can show "Following shared default"
	// rather than pretending an empty box means an empty instruction.
	// The Effective values are what that agent actually sends today.
	SharedReplyStyle          string
	CodexReplyStyle           string
	ClaudeReplyStyle          string
	CodexReplyStyleCustom     bool
	ClaudeReplyStyleCustom    bool
	CodexReplyStyleEffective  string
	ClaudeReplyStyleEffective string
	DefaultReplyStyle         string
	ReplyStyleMaxChars        int

	// LiveActive reports whether live mode is not merely selected but actually
	// running. The two differ whenever a preflight refused it, and the page has
	// to show the mode that is really in use rather than the one chosen.
	LiveActive bool

	// LiveRemoteControl reports whether the live session reaches claude.ai/code.
	// LiveNotice explains whatever is missing — a refused mode, or a live
	// session running without the browser view — and is empty when live mode is
	// off or fully working.
	LiveRemoteControl bool
	LiveNotice        string

	// ClaudeLiveSessionID is the live session's own id, kept apart from the
	// per-message conversation id so the page can show each for what it is.
	ClaudeLiveSessionID string

	// AutoUpdate and UpdateCheckMinutes drive the Updates card on Settings.
	AutoUpdate         bool
	UpdateCheckMinutes int

	// ClaudeSessionID and ClaudeSessionName are what the Agents page needs to
	// tell the user how to reopen the SMS conversation in Claude Code, which
	// lists sessions per working folder rather than handing them to a desktop
	// app the way Codex does.
	ClaudeSessionID   string
	ClaudeSessionName string

	CodexThreadActive   bool
	ClaudeSessionActive bool
	LastAgent           string
	LastRunAt           time.Time

	Cwd               string
	CwdOK             bool
	CodexCwd          string
	ClaudeCwd         string
	CodexCwdOK        bool
	ClaudeCwdOK       bool
	DefaultAgent      string
	DefaultAgentName  string
	CodexPrefix       string
	ClaudePrefix      string
	NewSessionCommand string
	TurnTimeout       int

	AllowedNumbers []AllowedNumber
	AllowedCount   int
	RequireCode    bool
	HasCode        bool

	ReplyMaxChars    int
	MaxReplyParts    int
	ProgressInterval int
	ReplyAck         bool
	ProgressUpdates  bool

	StartupEnabled     bool
	BootStartupEnabled bool
	MachineSecrets     bool
	CloseToTray        bool
	Theme              string
	Compact            bool
	Alerts             bool
	AlertSound         bool

	// Update is the last release check, so Settings and the update banner can
	// report a newer build without hitting the network on every render.
	Update ReleaseInfo
}

func (s uiStatus) SetupComplete() bool {
	return s.GmailReady && s.AllowedCount > 0
}

// SetupSteps lists what still has to happen before a text can run an agent.
// Home shows it only while something is outstanding.
func (s uiStatus) SetupSteps() []setupStep {
	steps := []setupStep{
		{Label: "Connect Gmail", Detail: "Choose App Password or your own Google OAuth project.", Href: "/connections", Done: s.GmailReady},
		{Label: "Allow a phone number", Detail: "Add the phone you text from to the agent it should reach.", Href: "/agents", Done: s.AllowedCount > 0},
		{Label: "Test an agent", Detail: "Confirm Codex or Claude answers on this PC.", Href: "/agents", Done: s.CodexCheck.OK || s.ClaudeCheck.OK},
	}
	return steps
}

func (s uiStatus) SetupPending() int {
	n := 0
	for _, step := range s.SetupSteps() {
		if !step.Done {
			n++
		}
	}
	return n
}

type setupStep struct {
	Label, Detail, Href string
	Done                bool
}

// AgentTone reports how a dependency check should read: verified good, verified
// bad, or never tested. Nothing here guesses — an untested agent says so.
func checkLabel(c Check, okLabel string) (string, string) {
	switch {
	case !c.Known():
		return "Not tested yet", "warn"
	case c.OK:
		return okLabel, "ok"
	default:
		return "Needs attention", "bad"
	}
}

func executableExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func directoryExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func (a *App) status() uiStatus {
	a.mu.Lock()
	cfg := a.cfg
	mc := a.mail
	b := a.bridge
	a.mu.Unlock()

	st := loadState(a.statePath)

	s := uiStatus{
		Version: version, Listen: cfg.Listen, DataDir: a.dataDir, Uptime: time.Since(hostStartedAt),
		Paused:                 cfg.Paused,
		GmailReady:             mc != nil && mc.Authorized(),
		GmailMethod:            cfg.Gmail.Method,
		GmailMethodLabel:       gmailMethodLabel(cfg.Gmail.Method),
		GmailEmail:             cfg.Gmail.Email,
		GmailCheck:             st.GmailCheck,
		SubjectPhrase:          cfg.Gmail.SubjectPhrase,
		CodexCheck:             st.CodexCheck,
		ClaudeCheck:            st.ClaudeCheck,
		CodexPath:              cfg.CodexPath,
		ClaudePath:             cfg.ClaudePath,
		CodexResolved:          resolveCodexExecutable(cfg.CodexPath),
		ClaudeResolved:         resolveClaudeExecutable(cfg.ClaudePath),
		HasClaudeToken:         hasClaudeToken(claudeTokenPath(a.dataDir)),
		PermissionMode:         normalizeClaudePermissionMode(cfg.Claude.PermissionMode),
		ClaudeUseChrome:        cfg.Claude.UseChrome,
		ClaudeProgressInterval: cfg.Claude.ProgressIntervalSeconds,
		CodexProgressInterval:  cfg.Codex.ProgressIntervalSeconds,
		ClaudeSessionMode:      normalizeClaudeSessionMode(cfg.Claude.SessionMode),
		ClaudeSessionModeLabel: claudeSessionModeLabel(cfg.Claude.SessionMode),
		ClaudeLiveSessionID:    st.ClaudeLiveSessionID,
		AutoUpdate:             cfg.Updates.Automatic,
		UpdateCheckMinutes:     cfg.Updates.normalizedCheckMinutes(),
		ClaudeSessionID:        st.ClaudeSessionID,
		ClaudeSessionName:      st.ClaudeSessionName,
		LastAgent:              st.LastAgent,
		LastRunAt:              st.LastRunAt,
		Cwd:                    cfg.Cwd,
		DefaultAgent:           cfg.DefaultAgent,
		CodexPrefix:            configuredCodexPrefix(cfg),
		ClaudePrefix:           configuredClaudePrefix(cfg),
		NewSessionCommand:      configuredNewSessionCommand(cfg),
		TurnTimeout:            cfg.TurnTimeoutMinutes,
		AllowedNumbers:         cfg.GoogleVoice.AllowedNumbers,
		RequireCode:            cfg.Security.RequireCode,
		HasCode:                cfg.Security.CodeHash != "",
		ReplyMaxChars:          cfg.GoogleVoice.ReplyMaxChars,
		MaxReplyParts:          cfg.GoogleVoice.MaxReplyParts,
		ProgressInterval:       cfg.GoogleVoice.ProgressIntervalSeconds,
		ReplyAck:               cfg.GoogleVoice.ReplyAck,
		ProgressUpdates:        cfg.GoogleVoice.ProgressUpdates,
		StartupEnabled:         autostartEnabled(),
		BootStartupEnabled:     bootStartupEnabled(),
		MachineSecrets:         cfg.Security.MachineScopeSecrets,
		CloseToTray:            cfg.UI.CloseToTray,
		Theme:                  normalizeTheme(cfg.UI.Theme),
		Compact:                cfg.UI.Compact,
		Alerts:                 cfg.UI.Alerts,
		AlertSound:             cfg.UI.AlertSound,
		Update:                 loadUpdateState(a.statePath),
	}
	s.SharedReplyStyle = strings.TrimSpace(cfg.GoogleVoice.ReplyStyleHint)
	if s.SharedReplyStyle == "" {
		s.SharedReplyStyle = defaultReplyStyleHint
	}
	s.CodexReplyStyle = strings.TrimSpace(cfg.Codex.Instruction)
	s.ClaudeReplyStyle = strings.TrimSpace(cfg.Claude.Instruction)
	s.CodexReplyStyleCustom = s.CodexReplyStyle != ""
	s.ClaudeReplyStyleCustom = s.ClaudeReplyStyle != ""
	s.CodexReplyStyleEffective = cfg.replyStyleHintFor("C")
	s.ClaudeReplyStyleEffective = cfg.replyStyleHintFor("A")
	s.DefaultReplyStyle = defaultReplyStyleHint
	s.ReplyStyleMaxChars = replyStyleHintMaxChars
	s.CodexFound = executableExists(s.CodexResolved)
	s.ClaudeFound = executableExists(s.ClaudeResolved)
	s.CodexCwd = cfg.codexWorkingDir()
	s.ClaudeCwd = cfg.claudeWorkingDir()
	s.CwdOK = directoryExists(cfg.Cwd)
	s.CodexCwdOK = directoryExists(s.CodexCwd)
	s.ClaudeCwdOK = directoryExists(s.ClaudeCwd)
	// Counted from the agents, because that is where an allowed number lives.
	s.AllowedCount = len(allAgentPhones(cfg))
	s.CodexThreadActive = st.CodexThreadID != ""
	s.ClaudeSessionActive = st.ClaudeSessionID != ""
	s.PermissionModeLabel = claudePermissionModeLabel(s.PermissionMode)

	// Live mode's real state, which is not the same as the configured mode: a
	// preflight can refuse it, and it can run without reaching claude.ai/code.
	// The page reports what is actually happening.
	a.mu.Lock()
	live, support, claude := a.liveClaude, a.liveSupport, a.claude
	a.mu.Unlock()
	if claude != nil {
		// Reads a cached probe; it never starts a subprocess from a page render.
		s.ChromeTokenNotice = claude.CachedChromeTokenConflict()
	}
	conn := a.cachedClaudeConnection()
	s.ClaudeConnKind, s.ClaudeConnLabel, s.ClaudeConnDetail = conn.Kind, conn.Label, conn.Detail
	s.ClaudeConnChromeReady, s.ClaudeConnNeedsSignIn = conn.ChromeReady, conn.NeedsSignIn
	s.LiveActive = live != nil
	s.LiveRemoteControl = s.LiveActive && support.RemoteControl
	if s.ClaudeSessionMode == claudeSessionModeLive {
		s.LiveNotice = support.Reason
	}
	s.DefaultAgentName = "Codex"
	if cfg.DefaultAgent == "A" {
		s.DefaultAgentName = "Claude"
	}
	if b != nil {
		s.Running = true
		b.mu.Lock()
		s.Busy = b.busy
		s.Paused = b.paused
		b.mu.Unlock()
		s.LastPollAt, s.LastPollErr = b.pollStatus()
	}
	return s
}

// recentEvents reads the newest activity events. Pages that render event data
// call it directly; the status snapshot deliberately does not, so polling it
// stays free of log I/O.
func (a *App) recentEvents(limit int) []ActivityEvent {
	return activityLogForStatePath(a.statePath).Recent(limit)
}

// lastError returns the newest error event for the Advanced page's snippet. It
// reports metadata already in the activity log, never message text.
func lastError(events []ActivityEvent) (ActivityEvent, bool) {
	for _, e := range events {
		if e.Level == "error" {
			return e, true
		}
	}
	return ActivityEvent{}, false
}

// recordCheck stores the outcome of a dependency test in state.json so the UI
// can report tested state across restarts.
func (a *App) recordCheck(which string, ok bool, detail string) {
	st := loadState(a.statePath)
	c := Check{OK: ok, At: time.Now(), Detail: truncate(detail, 220)}
	switch which {
	case "gmail":
		st.GmailCheck = c
	case "codex":
		st.CodexCheck = c
	case "claude":
		st.ClaudeCheck = c
	default:
		return
	}
	_ = saveState(a.statePath, st)
}
