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

const version = "0.46.22"

// defaultReplyStyleHint is the only behavioural framing FlipAi adds to an SMS
// command. FlipAi delivers the reply itself, so the agent is never told how or
// where to send anything — only that its answer travels as a text message.
const defaultReplyStyleHint = "Reply for SMS. Keep it brief and plain text."

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
	ChatGPTPrefix      string `json:"chatgptPrefix,omitempty"`
	ClaudeChatPrefix   string `json:"claudeChatPrefix,omitempty"`
	GeminiChatPrefix   string `json:"geminiChatPrefix,omitempty"`
	GrokChatPrefix     string `json:"grokChatPrefix,omitempty"`
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
	ChatGPT     ChatGPTConfig     `json:"chatgpt"`
	ClaudeChat  ClaudeChatConfig  `json:"claudeChat"`
	GeminiChat  GeminiChatConfig  `json:"geminiChat"`
	GrokChat    GrokChatConfig    `json:"grokChat"`
	Security    SecurityConfig    `json:"security"`
	UI          UIConfig          `json:"ui"`
}

// UIConfig is retained for compatibility with existing bridge.json files. The
// normal Settings page no longer exposes appearance, notifications or
// close-to-tray controls; fresh installs use the light/default presentation.
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

// ChatGPTConfig gives regular ChatGPT Chat the same SMS-facing shape as the
// CLI agents. Its browser connection remains separate because the underlying
// connection mechanism is different.
type ChatGPTConfig struct{ AgentSettings }

// ClaudeChatConfig is intentionally separate from ClaudeConfig. Claude is the
// local Claude Code CLI; Claude Chat is the user's regular claude.ai account in
// FlipAi's dedicated WebView2 profile.
type ClaudeChatConfig struct{ AgentSettings }

// GeminiChatConfig is the user's regular gemini.google.com account in its own
// dedicated WebView2 profile. It is intentionally independent from every CLI/API.
type GeminiChatConfig struct{ AgentSettings }

type GrokChatConfig struct{ AgentSettings }

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
	// Deprecated: a security code belongs to the agent that enforces it. These
	// stay so an existing bridge.json still parses and can be migrated once.
	RequireCode bool   `json:"requireCode"`
	CodeSalt    string `json:"codeSalt,omitempty"`
	CodeHash    string `json:"codeHash,omitempty"`

	AgentsMigrated          bool `json:"agentsMigrated,omitempty"`
	ChatGPTAgentMigrated    bool `json:"chatgptAgentMigrated,omitempty"`
	ClaudeChatAgentMigrated bool `json:"claudeChatAgentMigrated,omitempty"`
	GeminiChatAgentMigrated bool `json:"geminiChatAgentMigrated,omitempty"`
	GrokChatAgentMigrated   bool `json:"grokChatAgentMigrated,omitempty"`

	// MachineScopeSecrets records that stored credentials are protected for
	// this PC rather than for the signed-in account. Starting before sign-in
	// requires it, because a task that runs with no interactive logon has no
	// account key to decrypt with.
	MachineScopeSecrets bool `json:"machineScopeSecrets,omitempty"`
}

type State struct {
	CodexThreadID   string `json:"codexThreadId,omitempty"`
	ClaudeSessionID string `json:"claudeSessionId,omitempty"`

	ClaudeSessionName   string `json:"claudeSessionName,omitempty"`
	ClaudeLiveSessionID string `json:"claudeLiveSessionId,omitempty"`

	GmailBaselineUnix   int64     `json:"gmailBaselineUnix,omitempty"`
	ProcessedMessageIDs []string  `json:"processedMessageIds,omitempty"`
	LastMessageID       string    `json:"lastMessageId,omitempty"`
	LastRunAt           time.Time `json:"lastRunAt,omitempty"`
	LastAgent           string    `json:"lastAgent,omitempty"`
	// LastAgentBySender remembers the most recently selected SMS destination
	// for each allowed phone. Explicit C:, A:, G:, H:, M:, or X: changes it.
	LastAgentBySender map[string]string `json:"lastAgentBySender,omitempty"`

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
	browserDefaults := AgentSettings{ReplyAck: boolPtr(true), ProgressUpdates: boolPtr(true), ProgressIntervalSeconds: 120, AckDelaySeconds: 30}
	return Config{
		CodexPath: "codex", ClaudePath: "claude", Cwd: home,
		Listen: "127.0.0.1:8765", LocalToken: tok, TurnTimeoutMinutes: 90,
		DefaultAgent: "C", CodexPrefix: defaultCodexPrefix, ClaudePrefix: defaultClaudePrefix, ChatGPTPrefix: defaultChatGPTPrefix, ClaudeChatPrefix: defaultClaudeChatPrefix, GeminiChatPrefix: defaultGeminiChatPrefix, GrokChatPrefix: defaultGrokChatPrefix, NewSessionCommand: defaultNewSessionCommand,
		Gmail:       GmailConfig{CredentialsFile: filepath.Join(dataDir, "google-credentials.json"), PollSeconds: 1, SearchQuery: `subject:"new text message from" newer_than:2d`, SubjectPhrase: "new text message from"},
		GoogleVoice: GoogleVoiceConfig{RequiredSubjectPhrase: "new text message from", ReplyMaxChars: 300, ReplyStyleHint: defaultReplyStyleHint, MaxReplyParts: 4, ReplyAck: true, ProgressUpdates: true, ProgressIntervalSeconds: 120},
		Updates:     UpdateConfig{Automatic: false},
		Codex:       CodexConfig{ApprovalPolicy: "never"},
		Claude:      ClaudeConfig{PermissionMode: claudeFullAccess, UseChrome: true, SessionMode: claudeSessionModePrint},
		ChatGPT:     ChatGPTConfig{AgentSettings: browserDefaults},
		ClaudeChat:  ClaudeChatConfig{AgentSettings: browserDefaults},
		GeminiChat:  GeminiChatConfig{AgentSettings: browserDefaults},
		GrokChat:    GrokChatConfig{AgentSettings: browserDefaults},
		Security:    SecurityConfig{RequireCode: false},
		UI:          UIConfig{Theme: ThemeLight, Alerts: true, CloseToTray: true},
	}
}

const (
	ThemeLight  = "light"
	ThemeDark   = "dark"
	ThemeSystem = "system"
)

func (c Config) codexWorkingDir() string {
	if v := strings.TrimSpace(c.CodexCwd); v != "" {
		return v
	}
	return c.Cwd
}

func (c Config) progressIntervalFor(agent string) time.Duration {
	seconds := agentSettings(c, agent).ProgressIntervalSeconds
	if seconds <= 0 {
		seconds = c.GoogleVoice.ProgressIntervalSeconds
	}
	if seconds < 30 {
		seconds = 120
	}
	return time.Duration(seconds) * time.Second
}

func (c Config) replyStyleHintFor(agent string) string {
	_ = agent
	if shared := strings.TrimSpace(c.GoogleVoice.ReplyStyleHint); shared != "" {
		return shared
	}
	return defaultReplyStyleHint
}

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
	cfg.Codex.Instruction = normalizeReplyStyleHint(cfg.Codex.Instruction)
	cfg.Claude.Instruction = normalizeReplyStyleHint(cfg.Claude.Instruction)
	cfg.ChatGPT.Instruction = normalizeReplyStyleHint(cfg.ChatGPT.Instruction)
	cfg.ClaudeChat.Instruction = normalizeReplyStyleHint(cfg.ClaudeChat.Instruction)
	cfg.GeminiChat.Instruction = normalizeReplyStyleHint(cfg.GeminiChat.Instruction)
	cfg.GrokChat.Instruction = normalizeReplyStyleHint(cfg.GrokChat.Instruction)
	if cfg.GoogleVoice.MaxReplyParts < 1 {
		cfg.GoogleVoice.MaxReplyParts = 4
	}
	if cfg.GoogleVoice.MaxReplyParts > 10 {
		cfg.GoogleVoice.MaxReplyParts = 10
	}
	if cfg.GoogleVoice.ProgressIntervalSeconds < 30 {
		cfg.GoogleVoice.ProgressIntervalSeconds = 120
	}
	cfg.GoogleVoice.SendReplyViaAgentBrowser = false
	cfg.GoogleVoice.GmailReplyFallback = true
	cfg.Codex.ApprovalPolicy = "never"
	if !cfg.Security.RequireCode && cfg.Security.CodeHash == "" {
		if placeholder, e := secureRandomToken(24); e == nil {
			_ = setSecurityCode(&cfg, placeholder)
		}
	}
	if strings.TrimSpace(cfg.Claude.PermissionMode) == "acceptEdits" {
		cfg.Claude.PermissionMode = claudeFullAccess
	}
	cfg.Claude.PermissionMode = normalizeClaudePermissionMode(cfg.Claude.PermissionMode)
	cfg.Updates.CheckMinutes = cfg.Updates.normalizedCheckMinutes()
	cfg.Updates.CheckHours = 0
	cfg.Updates.Automatic = false
	if cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {
		cfg.DefaultAgent = "C"
	}
	cfg.CodexPrefix = normalizeCommandToken(cfg.CodexPrefix, defaultCodexPrefix)
	cfg.ClaudePrefix = normalizeCommandToken(cfg.ClaudePrefix, defaultClaudePrefix)
	cfg.ChatGPTPrefix = normalizeCommandToken(cfg.ChatGPTPrefix, defaultChatGPTPrefix)
	cfg.ClaudeChatPrefix = normalizeCommandToken(cfg.ClaudeChatPrefix, defaultClaudeChatPrefix)
	cfg.GeminiChatPrefix = normalizeCommandToken(cfg.GeminiChatPrefix, defaultGeminiChatPrefix)
	cfg.GrokChatPrefix = normalizeCommandToken(cfg.GrokChatPrefix, defaultGrokChatPrefix)
	cfg.NewSessionCommand = normalizeCommandToken(cfg.NewSessionCommand, defaultNewSessionCommand)
	prefixes := []string{cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix}
	dup := false
	for i := range prefixes {
		for j := i + 1; j < len(prefixes); j++ {
			if strings.EqualFold(prefixes[i], prefixes[j]) {
				dup = true
			}
		}
	}
	if dup {
		cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix = defaultCodexPrefix, defaultClaudePrefix, defaultChatGPTPrefix, defaultClaudeChatPrefix, defaultGeminiChatPrefix, defaultGrokChatPrefix
	}
	if cfg.LocalToken == "" {
		cfg.LocalToken, err = secureRandomToken(24)
		if err != nil {
			return cfg, err
		}
	}
	var probe struct {
		UI *UIConfig `json:"ui"`
	}
	if json.Unmarshal(b, &probe) == nil && probe.UI == nil {
		cfg.UI = defaultConfig(dataDir).UI
	}
	cfg.UI.Theme = normalizeTheme(cfg.UI.Theme)
	syncAllowedNumbers(&cfg.GoogleVoice)
	migrateAgentSettings(&cfg)
	if err := normalizeAgents(&cfg); err != nil {
		salvageAgents(&cfg)
	}
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
