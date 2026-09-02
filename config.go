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

const version = "0.46.8"

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
	Paused             bool   `json:"paused,omitempty"`

	Updates     UpdateConfig      `json:"updates"`
	Gmail       GmailConfig       `json:"gmail"`
	GoogleVoice GoogleVoiceConfig `json:"googleVoice"`
	Codex       CodexConfig       `json:"codex"`
	Claude      ClaudeConfig      `json:"claude"`
	Security    SecurityConfig    `json:"security"`
	UI          UIConfig          `json:"ui"`
}

type UIConfig struct {
	Theme       string `json:"theme"`
	Compact     bool   `json:"compact"`
	Alerts      bool   `json:"alerts"`
	AlertSound  bool   `json:"alertSound"`
	CloseToTray bool   `json:"closeToTray"`
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
	AllowedFrom           string          `json:"allowedFrom"`
	AllowedNumbers        []AllowedNumber `json:"allowedNumbers,omitempty"`
	RequiredSubjectPhrase string          `json:"requiredSubjectPhrase"`
	ReplyTo               string          `json:"replyTo"`
	ReplyMaxChars         int             `json:"replyMaxChars"`
	ReplyStyleHint        string          `json:"replyStyleHint"`
	MaxReplyParts         int             `json:"maxReplyParts"`
	ReplyAck                bool `json:"replyAck"`
	ProgressUpdates         bool `json:"progressUpdates"`
	ProgressIntervalSeconds int  `json:"progressIntervalSeconds"`
	SendReplyViaAgentBrowser bool `json:"sendReplyViaAgentBrowser"`
	GmailReplyFallback       bool `json:"gmailReplyFallback"`
}

type CodexConfig struct {
	AgentSettings
	ApprovalPolicy string `json:"approvalPolicy"`
}

type ClaudeConfig struct {
	AgentSettings
	PermissionMode string `json:"permissionMode"`
	UseChrome      bool   `json:"useChrome"`
	SessionMode    string `json:"sessionMode,omitempty"`
}

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

func (u UpdateConfig) checkInterval() time.Duration {
	m := u.normalizedCheckMinutes()
	return time.Duration(m) * time.Minute
}

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

const retiredUpdateCheckHoursDefault = 6

type SecurityConfig struct {
	RequireCode bool   `json:"requireCode"`
	CodeSalt    string `json:"codeSalt,omitempty"`
	CodeHash    string `json:"codeHash,omitempty"`
	AgentsMigrated bool `json:"agentsMigrated,omitempty"`
	MachineScopeSecrets bool `json:"machineScopeSecrets,omitempty"`
}

type State struct {
	CodexThreadID   string `json:"codexThreadId,omitempty"`
	ClaudeSessionID string `json:"claudeSessionId,omitempty"`
	ClaudeSessionName string `json:"claudeSessionName,omitempty"`
	ClaudeLiveSessionID string `json:"claudeLiveSessionId,omitempty"`
	GmailBaselineUnix   int64     `json:"gmailBaselineUnix,omitempty"`
	ProcessedMessageIDs []string  `json:"processedMessageIds,omitempty"`
	LastMessageID       string    `json:"lastMessageId,omitempty"`
	LastRunAt           time.Time `json:"lastRunAt,omitempty"`
	LastAgent           string    `json:"lastAgent,omitempty"`
	GmailCheck  Check `json:"gmailCheck,omitempty"`
	CodexCheck  Check `json:"codexCheck,omitempty"`
	ClaudeCheck Check `json:"claudeCheck,omitempty"`
}

type Check struct {
	OK     bool      `json:"ok"`
	At     time.Time `json:"at,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

func (c Check) Known() bool { return !c.At.IsZero() }
func (c Check) Ready() bool { return c.Known() && c.OK }

func secureRandomToken(n int) (string, error) {
	if n < 16 { n = 16 }
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil { return "", fmt.Errorf("secure randomness: %w", err) }
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashSecurityCode(code, salt string) string {
	v := []byte(salt + "\x00" + strings.TrimSpace(code))
	sum := sha256.Sum256(v)
	b := sum[:]
	for i := 0; i < 120000; i++ {
		h := sha256.New(); h.Write([]byte(salt)); h.Write(b); b = h.Sum(nil)
	}
	return hex.EncodeToString(b)
}

func setSecurityCode(cfg *Config, code string) error {
	code = strings.TrimSpace(code)
	if len(code) < 6 || strings.ContainsAny(code, " \t\r\n") { return errors.New("SMS security code must be at least 6 characters with no spaces") }
	salt, err := secureRandomToken(18); if err != nil { return err }
	cfg.Security.CodeSalt = salt; cfg.Security.CodeHash = hashSecurityCode(code, salt); return nil
}

func verifySecurityCode(cfg Config, code string) bool {
	if cfg.Security.CodeSalt == "" || cfg.Security.CodeHash == "" { return false }
	got := hashSecurityCode(code, cfg.Security.CodeSalt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(cfg.Security.CodeHash)) == 1
}

func appPaths() (dataDir, configFile, stateFile, tokenFile string, err error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, e := os.UserHomeDir(); if e != nil { return "", "", "", "", e }
		base = filepath.Join(home, ".local", "share")
	}
	dataDir = filepath.Join(base, "AISMSBridge")
	configFile = filepath.Join(dataDir, "bridge.json")
	stateFile = filepath.Join(dataDir, "state.json")
	tokenFile = filepath.Join(dataDir, "google-token.dat")
	return
}

func defaultConfig(dataDir string) Config {
	home, _ := os.UserHomeDir(); tok, _ := secureRandomToken(24)
	return Config{
		CodexPath: "codex", ClaudePath: "claude", Cwd: home,
		Listen: "127.0.0.1:8765", LocalToken: tok, TurnTimeoutMinutes: 90,
		DefaultAgent: "C", CodexPrefix: defaultCodexPrefix, ClaudePrefix: defaultClaudePrefix, NewSessionCommand: defaultNewSessionCommand,
		Gmail: GmailConfig{CredentialsFile: filepath.Join(dataDir, "google-credentials.json"), PollSeconds: 1, SearchQuery: `subject:"new text message from" newer_than:2d`, SubjectPhrase: "new text message from"},
		GoogleVoice: GoogleVoiceConfig{RequiredSubjectPhrase: "new text message from", ReplyMaxChars: 300, ReplyStyleHint: defaultReplyStyleHint, MaxReplyParts: 4, ReplyAck: true, ProgressUpdates: true, ProgressIntervalSeconds: 120},
		Updates: UpdateConfig{Automatic: false},
		Codex: CodexConfig{ApprovalPolicy: "never"},
		Claude: ClaudeConfig{PermissionMode: claudeFullAccess, UseChrome: true, SessionMode: claudeSessionModePrint},
		Security: SecurityConfig{RequireCode: true},
		UI: UIConfig{Theme: ThemeLight, Alerts: true, CloseToTray: true},
	}
}

const (
	ThemeLight = "light"
	ThemeDark = "dark"
	ThemeSystem = "system"
)

func (c Config) codexWorkingDir() string { if v := strings.TrimSpace(c.CodexCwd); v != "" { return v }; return c.Cwd }
func (c Config) progressIntervalFor(agent string) time.Duration {
	seconds := c.GoogleVoice.ProgressIntervalSeconds
	switch agent { case "A": if v := c.Claude.ProgressIntervalSeconds; v > 0 { seconds = v }; case "C": if v := c.Codex.ProgressIntervalSeconds; v > 0 { seconds = v } }
	if seconds < 30 { seconds = 120 }
	return time.Duration(seconds) * time.Second
}
func (c Config) replyStyleHintFor(agent string) string {
	var own string
	switch agent { case "A": own = strings.TrimSpace(c.Claude.Instruction); case "C": own = strings.TrimSpace(c.Codex.Instruction) }
	if own != "" { return own }
	if shared := strings.TrimSpace(c.GoogleVoice.ReplyStyleHint); shared != "" { return shared }
	return defaultReplyStyleHint
}
func normalizeReplyStyleHint(v string) string {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n")); if len(v) > replyStyleHintMaxChars { v = strings.TrimSpace(v[:replyStyleHintMaxChars]) }; return v
}
func (c Config) claudeWorkingDir() string { if v := strings.TrimSpace(c.ClaudeCwd); v != "" { return v }; return c.Cwd }
func normalizeTheme(v string) string { switch strings.ToLower(strings.TrimSpace(v)) { case ThemeDark: return ThemeDark; case ThemeSystem: return ThemeSystem; default: return ThemeLight } }

func loadConfig(path, dataDir string) (Config, error) {
	cfg := defaultConfig(dataDir)
	b, err := os.ReadFile(path); if err != nil { return cfg, err }
	if err := json.Unmarshal(b, &cfg); err != nil { return cfg, err }
	if cfg.Listen == "" { cfg.Listen = "127.0.0.1:8765" }
	if !strings.HasPrefix(cfg.Listen, "127.0.0.1:") && !strings.HasPrefix(cfg.Listen, "localhost:") { cfg.Listen = "127.0.0.1:8765" }
	if cfg.Gmail.PollSeconds < 1 { cfg.Gmail.PollSeconds = 1 }
	if cfg.Gmail.SubjectPhrase == "" { cfg.Gmail.SubjectPhrase = "new text message from" }
	if cfg.Gmail.Method == "" { if _, statErr := os.Stat(cfg.Gmail.CredentialsFile); statErr == nil { cfg.Gmail.Method = GmailMethodOAuth } }
	if cfg.Gmail.Method != "" && cfg.Gmail.Method != GmailMethodOAuth && cfg.Gmail.Method != GmailMethodAppPassword { cfg.Gmail.Method = "" }
	if cfg.GoogleVoice.ReplyMaxChars < 80 { cfg.GoogleVoice.ReplyMaxChars = 300 }
	if strings.TrimSpace(cfg.GoogleVoice.ReplyStyleHint) == "" { cfg.GoogleVoice.ReplyStyleHint = defaultReplyStyleHint }
	cfg.GoogleVoice.ReplyStyleHint = normalizeReplyStyleHint(cfg.GoogleVoice.ReplyStyleHint)
	cfg.Codex.Instruction = normalizeReplyStyleHint(cfg.Codex.Instruction)
	cfg.Claude.Instruction = normalizeReplyStyleHint(cfg.Claude.Instruction)
	if cfg.GoogleVoice.MaxReplyParts < 1 { cfg.GoogleVoice.MaxReplyParts = 4 }
	if cfg.GoogleVoice.MaxReplyParts > 10 { cfg.GoogleVoice.MaxReplyParts = 10 }
	if cfg.GoogleVoice.ProgressIntervalSeconds < 30 { cfg.GoogleVoice.ProgressIntervalSeconds = 120 }
	cfg.GoogleVoice.SendReplyViaAgentBrowser = false
	cfg.GoogleVoice.GmailReplyFallback = true
	cfg.Codex.ApprovalPolicy = "never"
	if !cfg.Security.RequireCode && cfg.Security.CodeHash == "" { if placeholder, e := secureRandomToken(24); e == nil { _ = setSecurityCode(&cfg, placeholder) } }
	if strings.TrimSpace(cfg.Claude.PermissionMode) == "acceptEdits" { cfg.Claude.PermissionMode = claudeFullAccess }
	cfg.Claude.PermissionMode = normalizeClaudePermissionMode(cfg.Claude.PermissionMode)
	cfg.Updates.CheckMinutes = cfg.Updates.normalizedCheckMinutes(); cfg.Updates.CheckHours = 0; cfg.Updates.Automatic = false
	if cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" { cfg.DefaultAgent = "C" }
	cfg.CodexPrefix = normalizeCommandToken(cfg.CodexPrefix, defaultCodexPrefix)
	cfg.ClaudePrefix = normalizeCommandToken(cfg.ClaudePrefix, defaultClaudePrefix)
	cfg.NewSessionCommand = normalizeCommandToken(cfg.NewSessionCommand, defaultNewSessionCommand)
	if strings.EqualFold(cfg.CodexPrefix, cfg.ClaudePrefix) { cfg.CodexPrefix, cfg.ClaudePrefix = defaultCodexPrefix, defaultClaudePrefix }
	if cfg.LocalToken == "" { cfg.LocalToken, err = secureRandomToken(24); if err != nil { return cfg, err } }
	var probe struct { UI *UIConfig `json:"ui"` }
	if json.Unmarshal(b, &probe) == nil && probe.UI == nil { cfg.UI = defaultConfig(dataDir).UI }
	cfg.UI.Theme = normalizeTheme(cfg.UI.Theme)
	syncAllowedNumbers(&cfg.GoogleVoice)
	migrateAgentSettings(&cfg)
	if err := normalizeAgents(&cfg); err != nil { salvageAgents(&cfg) }
	cfg.GoogleVoice.AllowedFrom = smsAllowedFrom(cfg)
	return cfg, nil
}

func saveConfig(path string, cfg Config) error {
	if cfg.LocalToken == "" { var err error; cfg.LocalToken, err = secureRandomToken(24); if err != nil { return err } }
	b, err := json.MarshalIndent(cfg, "", "  "); if err != nil { return err }
	return os.WriteFile(path, b, 0600)
}
func loadState(path string) State { var s State; if b, e := os.ReadFile(path); e == nil { _ = json.Unmarshal(b, &s) }; return s }
func saveState(path string, s State) error { b, e := json.MarshalIndent(s, "", "  "); if e != nil { return e }; return os.WriteFile(path, b, 0600) }
func ensureDataDir(dir string) error { if dir == "" { return errors.New("empty data directory") }; return os.MkdirAll(dir, 0700) }
