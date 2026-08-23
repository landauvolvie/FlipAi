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

	CodexThreadActive   bool
	ClaudeSessionActive bool
	LastAgent           string
	LastRunAt           time.Time

	Cwd              string
	CwdOK            bool
	CodexCwd         string
	ClaudeCwd        string
	CodexCwdOK       bool
	ClaudeCwdOK      bool
	DefaultAgent     string
	DefaultAgentName string
	TurnTimeout      int

	AllowedNumbers []AllowedNumber
	AllowedCount   int
	RequireCode    bool
	HasCode        bool

	ReplyMaxChars    int
	MaxReplyParts    int
	ProgressInterval int
	ReplyAck         bool
	ProgressUpdates  bool

	StartupEnabled bool
	CloseToTray    bool
	Theme          string
	Compact        bool
	Alerts         bool
	AlertSound     bool
}

func (s uiStatus) SetupComplete() bool {
	return s.GmailReady && s.AllowedCount > 0 && (!s.RequireCode || s.HasCode)
}

// SetupSteps lists what still has to happen before a text can run an agent.
// Home shows it only while something is outstanding.
func (s uiStatus) SetupSteps() []setupStep {
	steps := []setupStep{
		{Label: "Connect Gmail", Detail: "Choose App Password or your own Google OAuth project.", Href: "/connections", Done: s.GmailReady},
		{Label: "Allow a phone number", Detail: "Only numbers on the allowlist can reach your agents.", Href: "/phone", Done: s.AllowedCount > 0},
		{Label: "Set the SMS security code", Detail: "Required while code protection is on.", Href: "/phone", Done: !s.RequireCode || s.HasCode},
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
		Paused:           cfg.Paused,
		GmailReady:       mc != nil && mc.Authorized(),
		GmailMethod:      cfg.Gmail.Method,
		GmailMethodLabel: gmailMethodLabel(cfg.Gmail.Method),
		GmailEmail:       cfg.Gmail.Email,
		GmailCheck:       st.GmailCheck,
		SubjectPhrase:    cfg.Gmail.SubjectPhrase,
		CodexCheck:       st.CodexCheck,
		ClaudeCheck:      st.ClaudeCheck,
		CodexPath:        cfg.CodexPath,
		ClaudePath:       cfg.ClaudePath,
		CodexResolved:    resolveCodexExecutable(cfg.CodexPath),
		ClaudeResolved:   resolveClaudeExecutable(cfg.ClaudePath),
		HasClaudeToken:   hasClaudeToken(claudeTokenPath(a.dataDir)),
		PermissionMode:   cfg.Claude.PermissionMode,
		LastAgent:        st.LastAgent,
		LastRunAt:        st.LastRunAt,
		Cwd:              cfg.Cwd,
		DefaultAgent:     cfg.DefaultAgent,
		TurnTimeout:      cfg.TurnTimeoutMinutes,
		AllowedNumbers:   cfg.GoogleVoice.AllowedNumbers,
		RequireCode:      cfg.Security.RequireCode,
		HasCode:          cfg.Security.CodeHash != "",
		ReplyMaxChars:    cfg.GoogleVoice.ReplyMaxChars,
		MaxReplyParts:    cfg.GoogleVoice.MaxReplyParts,
		ProgressInterval: cfg.GoogleVoice.ProgressIntervalSeconds,
		ReplyAck:         cfg.GoogleVoice.ReplyAck,
		ProgressUpdates:  cfg.GoogleVoice.ProgressUpdates,
		StartupEnabled:   autostartEnabled(),
		CloseToTray:      cfg.UI.CloseToTray,
		Theme:            normalizeTheme(cfg.UI.Theme),
		Compact:          cfg.UI.Compact,
		Alerts:           cfg.UI.Alerts,
		AlertSound:       cfg.UI.AlertSound,
	}
	s.CodexFound = executableExists(s.CodexResolved)
	s.ClaudeFound = executableExists(s.ClaudeResolved)
	s.CodexCwd = cfg.codexWorkingDir()
	s.ClaudeCwd = cfg.claudeWorkingDir()
	s.CwdOK = directoryExists(cfg.Cwd)
	s.CodexCwdOK = directoryExists(s.CodexCwd)
	s.ClaudeCwdOK = directoryExists(s.ClaudeCwd)
	s.AllowedCount = len(s.AllowedNumbers)
	s.CodexThreadActive = st.CodexThreadID != ""
	s.ClaudeSessionActive = st.ClaudeSessionID != ""
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
