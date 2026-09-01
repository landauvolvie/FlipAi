package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const version = "0.46.3"

// defaultReplyStyleHint is the only behavioural framing FlipAi adds to an SMS
// command. FlipAi delivers the reply itself, so the agent is never told how or
// where to send anything — only that its answer travels as a text message.
const defaultReplyStyleHint = "Your answer is delivered to the user as an SMS text message, so keep it brief and in plain text."

// replyStyleHintMaxChars caps a hand-written instruction. FlipAi is a transport
// between a phone and the agent the user already trusts, so this line is meant
// to stay a line: a long preamble in front of every text changes what the agent
// is rather than how its answer is shaped, and it is billed on every turn.
const replyStyleHintMaxChars = 2000

type Config struct {
	CodexPath  string `json:"codexPath"`
	ClaudePath string `json:"claudePath"`

	// Cwd is the shared starting folder for local agents. CodexCwd and
	// ClaudeCwd override it per agent when set, so Codex can start in a projects
	// folder while Claude starts somewhere else.
	Cwd       string `json:"cwd"`
	CodexCwd  string `json:"codexCwd,omitempty"`
	ClaudeCwd string `json:"claudeCwd,omitempty"`

	Listen             string `json:"listen"`
	LocalToken         string `json:"localToken"`
	TurnTimeoutMinutes int    `json:"turnTimeoutMinutes"`
	DefaultAgent       string `json:"defaultAgent"`
	CodexPrefix        string `json:"codexPrefix,omitempty"`
	ClaudePrefix       string `json:"claudePrefix,omitempty"`
	NewSessionCommand  string `json:"newSessionCommand,omitempty"`

	// Paused stops the bridge from picking up new texts without shutting the
	// host down. The Home page toggles it, and the poll loop honours it live, so
	// pausing never loses a message: it stays unread in Gmail until FlipAi
	// resumes.
	Paused bool `json:"paused,omitempty"`

	Updates     UpdateConfig      `json:"updates"`
	Gmail       GmailConfig       `json:"gmail"`
	GoogleVoice GoogleVoiceConfig `json:"googleVoice"`
	Codex       CodexConfig       `json:"codex"`
	Claude      ClaudeConfig      `json:"claude"`
	Security    SecurityConfig    `json:"security"`
	UI          UIConfig          `json:"ui"`
}

// UIConfig holds legacy desktop-window preferences. Settings intentionally no
// longer exposes appearance, notification, or close-to-tray controls; loadConfig
// normalizes them to the simple app defaults below so an old config cannot leave
// a hidden preference active forever.
type UIConfig struct {
	Theme   string `json:"theme"`
	Compact bool   `json:"compact"`
	Alerts     bool `json:"alerts"`
	AlertSound bool `json:"alertSound"`
	CloseToTray bool `json:"closeToTray"`
}

type GmailConfig struct {
	Method          string `json:"method,omitempty"`
	Email           string `json:"email,omitempty"`
	CredentialsFile string `json:"credentialsFile"`
	PollSeconds     int    `json:"pollSeconds"`
	SearchQuery     string `json:"searchQuery"`
	SubjectPhrase   string `json:"subjectPhrase"`
}

type GoogleVoiceConfig struct {
	// AllowedFrom stays the newline-separated list every routing and test path
	// already reads. AllowedNumbers is the same allowlist with the label and
	// added-on date the Phone page shows; syncAllowedNumbers keeps the two in
	// step so neither representation can drift.
	AllowedFrom           string          `json:"allowedFrom"`
	AllowedNumbers        []AllowedNumber `json:"allowedNumbers,omitempty"`
	RequiredSubjectPhrase string          `json:"requiredSubjectPhrase"`
	ReplyTo               string          `json:"replyTo"`
	ReplyMaxChars         int             `json:"replyMaxChars"`

	// ReplyStyleHint is the single line of framing FlipAi appends to the SMS
	// command before handing it to the agent. Everything else the agent sees is
	// the user's own text, so texting behaves like sitting at the desktop app.
	ReplyStyleHint string `json:"replyStyleHint"`

	// MaxReplyParts caps how many numbered SMS parts a long answer is split
	// into. Splitting replaced truncation so a desktop-length answer survives
	// the trip to the phone.
	MaxReplyParts int `json:"maxReplyParts"`

	// ReplyAck texts a one-line confirmation the moment a command is
	// authenticated, before the agent starts. ProgressUpdates texts a periodic
	// "still working" line during long turns. Google Voice texts are free, so
	// both default on; both are user toggles in Settings.
	ReplyAck                bool `json:"replyAck"`
	ProgressUpdates         bool `json:"progressUpdates"`
	ProgressIntervalSeconds int  `json:"progressIntervalSeconds"`

	// Deprecated: FlipAi now always delivers the reply itself over the
	// authenticated Google Voice email address. These fields are retained only
	// so existing bridge.json files keep parsing; loadConfig forces them and
	// nothing reads them.
	SendReplyViaAgentBrowser bool `json:"sendReplyViaAgentBrowser"`
	GmailReplyFallback       bool `json:"gmailReplyFallback"`
}

type CodexConfig struct {
	AgentSettings

	ApprovalPolicy string `json:"approvalPolicy"`
}

type ClaudeConfig struct {
	AgentSettings

	// PermissionMode is passed to Claude Code as --permission-mode. It defaults
	// to full user access so a Claude SMS turn reaches as far as the Codex one
	// beside it; see claudeFullAccess for why a narrower mode silently breaks
	// Chrome and other MCP tools on an unattended turn.
	PermissionMode string `json:"permissionMode"`

	// UseChrome passes --chrome so Claude can drive the browser it already
	// drives at the desktop.
	UseChrome bool `json:"useChrome"`

	// SessionMode selects how FlipAi drives Claude Code.
	//
	// "print" is the original behaviour and stays the default: every SMS turn
	// is one `claude -p` subprocess that resumes the stored session id. It
	// needs no long-lived process, works with a `claude setup-token`, and is
	// the mode FlipAi falls back to whenever live mode cannot run.
	//
	// "live" keeps one Claude Code session running for the whole conversation
	// and delivers each SMS into it, so the same session can be opened in
	// Remote Control at claude.ai/code. It costs a supervised child process and
	// refuses a stored token; see claudelive.go for the full set of conditions.
	SessionMode string `json:"sessionMode,omitempty"`
}

// UpdateConfig is kept for compatibility with existing bridge.json files. The
// Settings page no longer exposes an update cadence or unattended installs:
// FlipAi checks in the background on the app default and installation remains a
// deliberate user action.
type UpdateConfig struct {
	CheckHours   int  `json:"checkHours,omitempty"`
	CheckMinutes int  `json:"checkMinutes,omitempty"`
	Automatic    bool `json:"automatic"`
}

const (
	updateCheckMinutesMin     = 5
	updateCheckMinutesMax     = 7 * 24 * 60
	updateCheckMinutesDefault = 50
)

// checkInterval is the validated background check period.
func (u UpdateConfig) checkInterval() time.Duration {
	m := u.normalizedCheckMinutes()
	return time.Duration(m) * time.Minute
}

// normalizedCheckMinutes resolves the effective cadence. Ten minutes was the
// old UI/default value; because that control has been retired, an install still
// carrying 10 is migrated to the new 50-minute app default.
func (u UpdateConfig) normalizedCheckMinutes() int {
	m := u.CheckMinutes
	if m == 10 {
		m = updateCheckMinutesDefault
	}
	if m == 0 && u.CheckHours > 0 {
		if u.CheckHours == retiredUpdateCheckHoursDefault {
			m = updateCheckMinutesDefault
		} else {
			m = u.CheckHours * 60
		}
	}
	if m < updateCheckMinutesMin || m > updateCheckMinutesMax {
		m = updateCheckMinutesDefault
	}
	return m
}

// retiredUpdateCheckHoursDefault is the value the hours-only setting shipped
// with. It is kept only so old files migrate cleanly.
const retiredUpdateCheckHoursDefault = 6

type SecurityConfig struct {
	// Deprecated: a security code belongs to the agent that enforces it. These
	// stay so an existing bridge.json still parses and can be migrated once.
	RequireCode bool   `json:"requireCode"`
	CodeSalt    string `json:"codeSalt,omitempty"`
	CodeHash    string `json:"codeHash,omitempty"`

	// AgentsMigrated records that the shared allowlist and code have already
	// been moved onto the agents, so a later change on one agent is never
	// undone by migrating the old fields again.
	AgentsMigrated bool `json:"agentsMigrated,omitempty"`

	// MachineScopeSecrets records that stored credentials are protected for
	// this PC rather than for the signed-in account. Starting before sign-in
	// requires it, because a task that runs with no interactive logon has no
	// account key to decrypt with.
	MachineScopeSecrets bool `json:"machineScopeSecrets,omitempty"`
}

type State struct {
	CodexThreadID   string `json:"codexThreadId,omitempty"`
	ClaudeSessionID string `json:"claudeSessionId,omitempty"`

	// ClaudeSessionName is the label minted when the current Claude
	// conversation started. It is stored beside the id so every resume reuses
	// the same name, and so the Agents page can show a resume handle that stays
	// unambiguous across however many new-session commands have been sent.
	ClaudeSessionName string `json:"claudeSessionName,omitempty"`

	// ClaudeLiveSessionID is the session id of the supervised live-mode session,
	// kept apart from ClaudeSessionID on purpose. The print path resumes its id
	// with `claude --resume`, which is not something to point at a session that
	// is currently open; keeping the two separate means a live turn that falls
	// back to per-message mode resumes the per-message conversation rather than
	// fighting the running one for the same transcript.
	ClaudeLiveSessionID string `json:"claudeLiveSessionId,omitempty"`

	GmailBaselineUnix   int64     `json:"gmailBaselineUnix,omitempty"`
	ProcessedMessageIDs []string  `json:"processedMessageIds,omitempty"`
	LastMessageID       string    `json:"lastMessageId,omitempty"`
	LastRunAt           time.Time `json:"lastRunAt,omitempty"`
	LastAgent           string    `json:"lastAgent,omitempty"`

	// Checks record the outcome of the last real connection test for each
	// dependency. The UI reports these instead of guessing: a tile says "Ready"
	// only because a test actually succeeded, and says when that happened.
	GmailCheck  Check `json:"gmailCheck,omitempty"`
	CodexCheck  Check `json:"codexCheck,omitempty"`
	ClaudeCheck Check `json:"claudeCheck,omitempty"`
}

// Check is the result of one dependency test, kept so the desktop UI can show
// verified state rather than an optimistic guess.
type Check struct {
	OK     bool      `json:"ok"`
	At     time.Time `json:"at,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

func (c Check) Known() bool { return !c.At.IsZero() }

// Ready reports a dependency that a test actually confirmed. OK on its own is
// not enough: a Check with no timestamp was never run, so a page that keyed a
// green badge off OK alone could call an untested agent ready.
func (c Check) Ready() bool { return c.Known() && c.OK }

func secureRandomToken(n int) (string, error) {
	if n < 16 {
		n = 16
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secure randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashSecurityCode(code, salt string) string {
	v := []byte(salt + "\x00" + strings.TrimSpace(code))
	sum := sha256.Sum256(v)
	b := sum[:]
	for i := 0; i < 120000; i++ {
		h := sha256.New()
		h.Write([]byte(salt))
		h.Write(b)
		b = h.Sum(nil)
	}
	return hex.EncodeToString(b)
}

func setSecurityCode(cfg *Config, code string) error {
	code = strings.TrimSpace(code)
	if len(code) < 6 || strings.ContainsAny(code, " \t\r\n") {
		return errors.New("SMS security code must be at least 6 characters with no spaces")
	}
	salt, err := secureRandomToken(18)
	if err != nil {
		return err
	}
	cfg.Security.CodeSalt = salt
	cfg.Security.CodeHash = hashSecurityCode(code, salt)
	return nil
}

func verifySecurityCode(cfg Config, code string) bool {
	if cfg.Security.CodeSalt == "" || cfg.Security.CodeHash == "" {
		return false
	}
	got := hashSecurityCode(code, cfg.Security.CodeSalt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(cfg.Security.CodeHash)) == 1
}

func appPaths() (dataDir, configFile, stateFile, tokenFile string, err error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return "", "", "", "", e
		}
		base = filepath.Join(home, ".local", "share")
	}
	dataDir = filepath.Join(base, "AISMSBridge")
	configFile = filepath.Join(dataDir, "bridge.json")
	stateFile = filepath.Join(dataDir, "state.json")
	tokenFile = filepath.Join(dataDir, "google-token.dat")
	return
}

func defaultConfig(dataDir string) Config {
	home, _ := os.UserHomeDir()
	tok, _ := secureRandomToken(24)
	return Config{
		CodexPath: "codex", ClaudePath: "claude", Cwd: home,
		Listen: "127.0.0.1:8765", LocalToken: tok, TurnTimeoutMinutes: 90,
		DefaultAgent: "C", CodexPrefix: defaultCodexPrefix, ClaudePrefix: defaultClaudePrefix, NewSessionCommand: defaultNewSessionCommand,
		Gmail: GmailConfig{CredentialsFile: filepath.Join(dataDir, "google-credentials.json"), PollSeconds: 1, SearchQuery: `subject:"new text message from" newer_than:2d`, SubjectPhrase: "new text message from"},
		GoogleVoice: GoogleVoiceConfig{
			RequiredSubjectPhrase:   "new text message from",
			ReplyMaxChars:           300,
			ReplyStyleHint:          defaultReplyStyleHint,
			MaxReplyParts:           4,
			ReplyAck:                true,
			ProgressUpdates:         true,
			ProgressIntervalSeconds: 120,
		},
		Updates:  UpdateConfig{Automatic: false},
		Codex:    CodexConfig{ApprovalPolicy: "never"},
		Claude:   ClaudeConfig{PermissionMode: claudeFullAccess, UseChrome: true, SessionMode: claudeSessionModePrint},
		Security: SecurityConfig{RequireCode: true},
		UI:       UIConfig{Theme: ThemeLight, Compact: false, Alerts: false, AlertSound: false, CloseToTray: true},
	}
}

const (
	ThemeLight  = "light"
	ThemeDark   = "dark"
	ThemeSystem = "system"
)

// codexWorkingDir and claudeWorkingDir resolve the folder an agent process
// starts in. An empty per-agent value means "use the shared folder", so an
// upgraded install behaves exactly as it did before per-agent folders existed.
func (c Config) codexWorkingDir() string {
	if v := strings.TrimSpace(c.CodexCwd); v != "" {
		return v
	}
	return c.Cwd
}

// progressIntervalFor resolves the heartbeat cadence for one agent: its own
// override when set, otherwise the shared value. A task that runs fifteen
// minutes should be able to report differently from one that runs one, and the
// two agents are used for different work.
//
// The same 30-second floor the shared setting has applies to an override, so a
// per-agent value cannot turn a long turn into a stream of texts.
func (c Config) progressIntervalFor(agent string) time.Duration {
	seconds := c.GoogleVoice.ProgressIntervalSeconds
	switch agent {
	case "A":
		if v := c.Claude.ProgressIntervalSeconds; v > 0 {
			seconds = v
		}
	case "C":
		if v := c.Codex.ProgressIntervalSeconds; v > 0 {
			seconds = v
		}
	}
	if seconds < 30 {
		seconds = 120
	}
	return time.Duration(seconds) * time.Second
}

// replyStyleHintFor resolves the one line of framing FlipAi puts after an SMS
// command for a given agent: that agent's own instruction when it has one,
// otherwise the shared default. An install that never opens the new editors
// keeps behaving exactly as it did, because both overrides start empty.
func (c Config) replyStyleHintFor(agent string) string {
	var own string
	switch agent {
	case "A":
		own = strings.TrimSpace(c.Claude.Instruction)
	case "C":
		own = strings.TrimSpace(c.Codex.Instruction)
	}
	if own != "" {
		return own
	}
	if shared := strings.TrimSpace(c.GoogleVoice.ReplyStyleHint); shared != "" {
		return shared
	}
	return defaultReplyStyleHint
}

// normalizeReplyStyleHint trims a hand-written instruction and folds it to a
// single block of plain text. Blank means "follow the shared default", which is
// what the Reset control on each agent posts.
func normalizeReplyStyleHint(v string) string {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n"))
	if len(v) > replyStyleHintMaxChars {
		v = strings.TrimSpace(v[:replyStyleHintMaxChars])
	}
	return v
}

func (c Config) claudeWorkingDir() string {
	if v := strings.TrimSpace(c.ClaudeCwd); v != "" {
		return v
	}
	return c.Cwd
}

func normalizeTheme(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ThemeDark:
		return ThemeDark
	case ThemeSystem:
		return ThemeSystem
	default:
		return ThemeLight
	}
}

func loadConfig(path, dataDir string) (Config, error) {
	cfg := defaultConfig(dataDir)
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8765"
	}
	if !strings.HasPrefix(cfg.Listen, "127.0.0.1:") && !strings.HasPrefix(cfg.Listen, "localhost:") {
		cfg.Listen = "127.0.0.1:8765"
	}
	if cfg.Gmail.PollSeconds < 1 {
		cfg.Gmail.PollSeconds = 1
	}
	if cfg.Gmail.SubjectPhrase == "" {
		cfg.Gmail.SubjectPhrase = "new text message from"
	}
	// There is intentionally no Gmail default for new installs. Migrate only
	// older v0.4.x installs that already have a Desktop OAuth JSON on disk.
	if cfg.Gmail.Method == "" {
		if _, statErr := os.Stat(cfg.Gmail.CredentialsFile); statErr == nil {
			cfg.Gmail.Method = GmailMethodOAuth
		}
	}
	if cfg.Gmail.Method != "" && cfg.Gmail.Method != GmailMethodOAuth && cfg.Gmail.Method != GmailMethodAppPassword {
		cfg.Gmail.Method = ""
	}
	if cfg.GoogleVoice.ReplyMaxChars < 80 {
		cfg.GoogleVoice.ReplyMaxChars = 300
	}
	if strings.TrimSpace(cfg.GoogleVoice.ReplyStyleHint) == "" {
		cfg.GoogleVoice.ReplyStyleHint = defaultReplyStyleHint
	}
	cfg.GoogleVoice.ReplyStyleHint = normalizeReplyStyleHint(cfg.GoogleVoice.ReplyStyleHint)
	// Per-agent overrides stay blank when nobody set one; blank means "follow
	// the shared line above".
	cfg.Codex.Instruction = normalizeReplyStyleHint(cfg.Codex.Instruction)
	cfg.Claude.Instruction = normalizeReplyStyleHint(cfg.Claude.Instruction)
	if cfg.GoogleVoice.MaxReplyParts < 1 {
		cfg.GoogleVoice.MaxReplyParts = 4
	}
	if cfg.GoogleVoice.MaxReplyParts > 10 {
		cfg.GoogleVoice.MaxReplyParts = 10
	}
	if cfg.GoogleVoice.ProgressIntervalSeconds < 30 {
		cfg.GoogleVoice.ProgressIntervalSeconds = 120
	}
	// FlipAi always delivers the reply itself now. Force the retired fields so
	// an upgraded install cannot resurrect the agent-driven browser reply.
	cfg.GoogleVoice.SendReplyViaAgentBrowser = false
	cfg.GoogleVoice.GmailReplyFallback = true
	// SMS turns intentionally use Codex full normal-user access. This removes
	// the Codex sandbox/approval layer but does not elevate the Windows process.
	cfg.Codex.ApprovalPolicy = "never"
	// Older configs predate RequireCode. Because loadConfig starts from
	// defaultConfig, they inherit RequireCode=true. If a manually edited config
	// disables the code without a stored hash, create an unguessable placeholder
	// so the older startup readiness check remains satisfied; parsing ignores it.
	if !cfg.Security.RequireCode && cfg.Security.CodeHash == "" {
		if placeholder, e := secureRandomToken(24); e == nil {
			_ = setSecurityCode(&cfg, placeholder)
		}
	}
	// Claude SMS turns get the same reach as Codex SMS turns. Older FlipAi
	// builds rewrote this field on every load — "", "auto", and even an
	// explicit "bypassPermissions" all became "acceptEdits" — so the stored
	// value records what that rewrite produced, not what the user chose: every
	// install on disk reads "acceptEdits" whether or not anybody picked it.
	// acceptEdits auto-approves file edits only, which left Chrome and every
	// other MCP tool needing an approval no unattended SMS turn can give.
	// Upgrading installs therefore move to full access once; the Agents page
	// can narrow it again.
	if strings.TrimSpace(cfg.Claude.PermissionMode) == "acceptEdits" {
		cfg.Claude.PermissionMode = claudeFullAccess
	}
	cfg.Claude.PermissionMode = normalizeClaudePermissionMode(cfg.Claude.PermissionMode)
	// Keep reading the old updates block for file compatibility, but the user
	// no longer has an automatic-install switch. Existing true values are
	// disabled on upgrade, and the former 10-minute default migrates to 50.
	cfg.Updates.CheckMinutes = cfg.Updates.normalizedCheckMinutes()
	cfg.Updates.CheckHours = 0
	cfg.Updates.Automatic = false
	if cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {
		cfg.DefaultAgent = "C"
	}
	cfg.CodexPrefix = normalizeCommandToken(cfg.CodexPrefix, defaultCodexPrefix)
	cfg.ClaudePrefix = normalizeCommandToken(cfg.ClaudePrefix, defaultClaudePrefix)
	cfg.NewSessionCommand = normalizeCommandToken(cfg.NewSessionCommand, defaultNewSessionCommand)
	if strings.EqualFold(cfg.CodexPrefix, cfg.ClaudePrefix) {
		cfg.CodexPrefix, cfg.ClaudePrefix = defaultCodexPrefix, defaultClaudePrefix
	}
	if cfg.LocalToken == "" {
		cfg.LocalToken, err = secureRandomToken(24)
		if err != nil {
			return cfg, err
		}
	}
	// Appearance, notification and close behavior are no longer settings. Keep
	// the desktop predictable across old and new installs: light, standard
	// spacing, no in-window alert preference, and closing the window leaves the
	// background bridge alive.
	cfg.UI.Theme = ThemeLight
	cfg.UI.Compact = false
	cfg.UI.Alerts = false
	cfg.UI.AlertSound = false
	cfg.UI.CloseToTray = true
	syncAllowedNumbers(&cfg.GoogleVoice)
	// Allowed numbers, security codes and reply behaviour now belong to the
	// agent they reach. Move a pre-agent configuration onto the agents, then
	// clean both together so a number can never be claimed twice.
	migrateAgentSettings(&cfg)
	if err := normalizeAgents(&cfg); err != nil {
		// A stored file that cannot satisfy the rules must not stop FlipAi from
		// starting: drop what does not fit rather than refusing to load.
		salvageAgents(&cfg)
	}
	// The Google Voice email parser still checks one list; it is now derived
	// from the agents so there is a single source of truth.
	cfg.GoogleVoice.AllowedFrom = smsAllowedFrom(cfg)
	return cfg, nil
}

func saveConfig(path string, cfg Config) error {
	if cfg.LocalToken == "" {
		var err error
		cfg.LocalToken, err = secureRandomToken(24)
		if err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func loadState(path string) State {
	var s State
	if b, e := os.ReadFile(path); e == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}
func saveState(path string, s State) error {
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, b, 0600)
}
func ensureDataDir(dir string) error {
	if dir == "" {
		return errors.New("empty data directory")
	}
	return os.MkdirAll(dir, 0700)
}
