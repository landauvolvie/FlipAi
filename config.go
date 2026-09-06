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

const version = "0.46.54"

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
	// ClaudeCwd let either one override it without changing the other.
	Cwd       string `json:"cwd"`
	CodexCwd  string `json:"codexCwd,omitempty"`
	ClaudeCwd string `json:"claudeCwd,omitempty"`

	// Public SMS prefixes are stored for backward compatibility with existing
	// installations. The provider-grouped public shortcuts are resolved in
	// sms_routes.go; these values remain the internal/legacy parser aliases.
	CodexPrefix       string `json:"codexPrefix"`
	ClaudePrefix      string `json:"claudePrefix"`
	ChatGPTPrefix     string `json:"chatgptPrefix,omitempty"`
	ClaudeChatPrefix  string `json:"claudeChatPrefix,omitempty"`
	GeminiChatPrefix  string `json:"geminiChatPrefix,omitempty"`
	GrokChatPrefix    string `json:"grokChatPrefix,omitempty"`
	CopilotChatPrefix string `json:"copilotChatPrefix,omitempty"`
	NewSessionCommand string `json:"newSessionCommand"`

	DefaultAgent string `json:"defaultAgent"`

	TurnTimeoutMinutes int `json:"turnTimeoutMinutes"`

	Codex       AgentSettings `json:"codex"`
	Claude      AgentSettings `json:"claude"`
	ChatGPT     AgentSettings `json:"chatgpt,omitempty"`
	ClaudeChat  AgentSettings `json:"claudeChat,omitempty"`
	GeminiChat  AgentSettings `json:"geminiChat,omitempty"`
	GrokChat    AgentSettings `json:"grokChat,omitempty"`
	CopilotChat AgentSettings `json:"copilotChat,omitempty"`

	GoogleVoice GoogleVoiceConfig `json:"googleVoice"`
	Gmail       GmailConfig       `json:"gmail"`

	Paused bool `json:"paused"`
}

type AgentSettings struct {
	Phones            []AgentPhone `json:"phones,omitempty"`
	CallerNames       string       `json:"callerNames,omitempty"`
	RequireCode       bool         `json:"requireCode,omitempty"`
	CodeHash          string       `json:"codeHash,omitempty"`
	ReplyStyleHint    string       `json:"replyStyleHint,omitempty"`
	Ack               *bool        `json:"ack,omitempty"`
	Progress          *bool        `json:"progress,omitempty"`
	ProgressInterval  int          `json:"progressInterval,omitempty"`
	AckDelaySeconds   int          `json:"ackDelaySeconds,omitempty"`
	Voice             bool         `json:"voice,omitempty"`
	Mode              string       `json:"mode,omitempty"`
	ApprovalPolicy    string       `json:"approvalPolicy,omitempty"`
}

type AgentPhone struct {
	Number string `json:"number"`
	Label  string `json:"label,omitempty"`
	Access string `json:"access,omitempty"`
}

const (
	AccessAll   = "all"
	AccessSMS   = "sms"
	AccessVoice = "voice"
)

func (p AgentPhone) AllowsSMS() bool {
	return p.Access == "" || p.Access == AccessAll || p.Access == AccessSMS
}
func (p AgentPhone) AllowsVoice() bool {
	return p.Access == "" || p.Access == AccessAll || p.Access == AccessVoice
}

type GoogleVoiceConfig struct {
	AllowedFrom           string `json:"allowedFrom"`
	RequiredSubjectPhrase string `json:"requiredSubjectPhrase"`
	ReplyMaxChars         int    `json:"replyMaxChars"`
	Ack                   bool   `json:"ack"`
	Progress              bool   `json:"progress"`
	ProgressInterval      int    `json:"progressInterval"`
}

type GmailConfig struct {
	Mode        string `json:"mode"`
	Address     string `json:"address"`
	AppPassword string `json:"appPassword"`
	PollSeconds int    `json:"pollSeconds"`
}

type State struct {
	CodexThreadID       string            `json:"codexThreadId,omitempty"`
	ClaudeSessionID     string            `json:"claudeSessionId,omitempty"`
	ClaudeSessionName   string            `json:"claudeSessionName,omitempty"`
	ClaudeLiveSessionID string            `json:"claudeLiveSessionId,omitempty"`
	LastMessageID       string            `json:"lastMessageId,omitempty"`
	ProcessedMessageIDs []string          `json:"processedMessageIds,omitempty"`
	GmailBaselineUnix   int64             `json:"gmailBaselineUnix,omitempty"`
	LastAgent            string            `json:"lastAgent,omitempty"`
	LastAgentBySender    map[string]string `json:"lastAgentBySender,omitempty"`
	LastRunAt            time.Time         `json:"lastRunAt,omitempty"`
}

func defaultConfig(dataDir string) Config {
	return Config{
		CodexPath:            "codex",
		ClaudePath:           "claude",
		CodexPrefix:          defaultCodexPrefix,
		ClaudePrefix:         defaultClaudePrefix,
		ChatGPTPrefix:        defaultChatGPTPrefix,
		ClaudeChatPrefix:     defaultClaudeChatPrefix,
		GeminiChatPrefix:     defaultGeminiChatPrefix,
		GrokChatPrefix:       defaultGrokChatPrefix,
		CopilotChatPrefix:    defaultCopilotChatPrefix,
		NewSessionCommand:    defaultNewSessionCommand,
		DefaultAgent:         "C",
		TurnTimeoutMinutes:   90,
		Codex:                defaultAgentSettings(true, 0),
		Claude:               defaultAgentSettings(true, 0),
		ChatGPT:              defaultAgentSettings(false, 30),
		ClaudeChat:           defaultAgentSettings(false, 30),
		GeminiChat:           defaultAgentSettings(false, 30),
		GrokChat:             defaultAgentSettings(false, 30),
		CopilotChat:          defaultAgentSettings(false, 30),
		GoogleVoice:          defaultGoogleVoiceConfig(),
		Gmail:                defaultGmailConfig(),
	}
}

func defaultAgentSettings(progress bool, ackDelay int) AgentSettings {
	ack := true
	return AgentSettings{
		Ack:              &ack,
		Progress:         boolPtr(progress),
		ProgressInterval: 120,
		AckDelaySeconds:  ackDelay,
	}
}

func defaultGoogleVoiceConfig() GoogleVoiceConfig {
	return GoogleVoiceConfig{
		RequiredSubjectPhrase: "New text message from",
		ReplyMaxChars:         1550,
		Ack:                   true,
		Progress:              true,
		ProgressInterval:      120,
	}
}

func defaultGmailConfig() GmailConfig {
	return GmailConfig{Mode: "api", PollSeconds: 2}
}

func boolPtr(v bool) *bool { return &v }

func normalizeAgentSettings(s AgentSettings, fallbackProgress bool, fallbackAckDelay int) AgentSettings {
	if s.Ack == nil {
		s.Ack = boolPtr(true)
	}
	if s.Progress == nil {
		s.Progress = boolPtr(fallbackProgress)
	}
	if s.ProgressInterval <= 0 {
		s.ProgressInterval = 120
	}
	if s.AckDelaySeconds < 0 {
		s.AckDelaySeconds = fallbackAckDelay
	}
	return s
}

func normalizeConfig(cfg Config, dataDir string) Config {
	def := defaultConfig(dataDir)
	if strings.TrimSpace(cfg.CodexPath) == "" {
		cfg.CodexPath = def.CodexPath
	}
	if strings.TrimSpace(cfg.ClaudePath) == "" {
		cfg.ClaudePath = def.ClaudePath
	}
	if strings.TrimSpace(cfg.CodexPrefix) == "" {
		cfg.CodexPrefix = def.CodexPrefix
	}
	if strings.TrimSpace(cfg.ClaudePrefix) == "" {
		cfg.ClaudePrefix = def.ClaudePrefix
	}
	if strings.TrimSpace(cfg.ChatGPTPrefix) == "" {
		cfg.ChatGPTPrefix = def.ChatGPTPrefix
	}
	if strings.TrimSpace(cfg.ClaudeChatPrefix) == "" {
		cfg.ClaudeChatPrefix = def.ClaudeChatPrefix
	}
	if strings.TrimSpace(cfg.GeminiChatPrefix) == "" {
		cfg.GeminiChatPrefix = def.GeminiChatPrefix
	}
	if strings.TrimSpace(cfg.GrokChatPrefix) == "" {
		cfg.GrokChatPrefix = def.GrokChatPrefix
	}
	if strings.TrimSpace(cfg.CopilotChatPrefix) == "" {
		cfg.CopilotChatPrefix = def.CopilotChatPrefix
	}
	if strings.TrimSpace(cfg.NewSessionCommand) == "" {
		cfg.NewSessionCommand = def.NewSessionCommand
	}
	if cfg.TurnTimeoutMinutes <= 0 {
		cfg.TurnTimeoutMinutes = def.TurnTimeoutMinutes
	}
	cfg.Codex = normalizeAgentSettings(cfg.Codex, true, 0)
	cfg.Claude = normalizeAgentSettings(cfg.Claude, true, 0)
	cfg.ChatGPT = normalizeAgentSettings(cfg.ChatGPT, false, 30)
	cfg.ClaudeChat = normalizeAgentSettings(cfg.ClaudeChat, false, 30)
	cfg.GeminiChat = normalizeAgentSettings(cfg.GeminiChat, false, 30)
	cfg.GrokChat = normalizeAgentSettings(cfg.GrokChat, false, 30)
	cfg.CopilotChat = normalizeAgentSettings(cfg.CopilotChat, false, 30)
	cfg.GoogleVoice = normalizeGoogleVoiceConfig(cfg.GoogleVoice)
	cfg.Gmail = normalizeGmailConfig(cfg.Gmail)
	return cfg
}

func normalizeGoogleVoiceConfig(cfg GoogleVoiceConfig) GoogleVoiceConfig {
	if cfg.ReplyMaxChars <= 0 {
		cfg.ReplyMaxChars = 1550
	}
	if cfg.ProgressInterval <= 0 {
		cfg.ProgressInterval = 120
	}
	return cfg
}

func normalizeGmailConfig(cfg GmailConfig) GmailConfig {
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = "api"
	}
	if cfg.PollSeconds < 1 {
		cfg.PollSeconds = 1
	}
	return cfg
}

func loadConfig(path, dataDir string) Config {
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultConfig(dataDir)
	}
	var cfg Config
	if json.Unmarshal(b, &cfg) != nil {
		return defaultConfig(dataDir)
	}
	return normalizeConfig(cfg, dataDir)
}

func saveConfig(path string, cfg Config) error {
	cfg = normalizeConfig(cfg, filepath.Dir(path))
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func newSecurityCode() (string, string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	plain := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(plain))
	return plain, hex.EncodeToString(h[:]), nil
}

func hashSecurityCode(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func verifySecurityCode(hash, supplied string) bool {
	want, err := hex.DecodeString(strings.TrimSpace(hash))
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(supplied))
	return subtle.ConstantTimeCompare(want, got[:]) == 1
}
