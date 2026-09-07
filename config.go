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

const version = "0.46.60"

// defaultReplyStyleHint is the only behavioural framing FlipAi adds to a phone
// command. It deliberately says nothing about SMS or plain text so providers
// remain free to use their normal tools, including image/file generation.
const defaultReplyStyleHint = "Please keep your reply short and to the point."

const (
	legacyReplyStyleHintSMSV1 = "Reply for SMS. Keep it brief and plain text."
	legacyReplyStyleHintSMSV0 = "Your answer is delivered to the user as an SMS text message, so keep it brief and in plain text."
)

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
	CopilotChatPrefix  string `json:"copilotChatPrefix,omitempty"`
	NewSessionCommand  string `json:"newSessionCommand,omitempty"`

	// Paused stops the bridge from picking up new texts without shutting the
	// control plane down. It is intentionally persisted in config.json.
	Paused bool `json:"paused"`

	PollSeconds      int    `json:"pollSeconds"`
	GmailQuery       string `json:"gmailQuery"`
	GmailLabel       string `json:"gmailLabel"`
	GmailCredsPath   string `json:"gmailCredsPath"`
	GmailTokenPath   string `json:"gmailTokenPath"`
	EmailRegex       string `json:"emailRegex"`
	GoogleVoiceRegex string `json:"googleVoiceRegex"`

	Agents map[string]AgentSettings `json:"agents"`

	Phonebook map[string]string `json:"phonebook"`

	SMTP SMTPConfig `json:"smtp"`

	GoogleVoice GoogleVoiceConfig `json:"googleVoice"`
}

type AgentSettings struct {
	Enabled      bool   `json:"enabled"`
	RequireCode  bool   `json:"requireCode"`
	CodeHash     string `json:"codeHash,omitempty"`
	ReplyStyle   string `json:"replyStyle,omitempty"`
	SharedNumber string `json:"sharedNumber,omitempty"`
}

type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type GoogleVoiceConfig struct {
	Enabled           bool   `json:"enabled"`
	ProfileDir        string `json:"profileDir,omitempty"`
	ReceiverURL       string `json:"receiverUrl,omitempty"`
	ReceiverConnected bool   `json:"receiverConnected,omitempty"`
}

func defaultConfig(dataDir string) Config {
	return Config{
		Listen:             "127.0.0.1:4141",
		TurnTimeoutMinutes: 25,
		DefaultAgent:       "C",
		CodexPrefix:        "C",
		ClaudePrefix:       "A",
		ChatGPTPrefix:      chatGPTSMSPrefix,
		ClaudeChatPrefix:   "H",
		GeminiChatPrefix:   "M",
		GrokChatPrefix:     "X",
		CopilotChatPrefix:  "P",
		NewSessionCommand:  "NEW",
		PollSeconds:        2,
		GmailQuery:         `from:(txt.voice.google.com) newer_than:2d`,
		GmailLabel:         "FlipAiProcessed",
		GmailCredsPath:     filepath.Join(dataDir, "credentials.json"),
		GmailTokenPath:     filepath.Join(dataDir, "gmail-token.json"),
		EmailRegex:         `(?i)^\s*(?:from:\s*)?([^\n\r<]+)?\s*<?([+\d(). -]{7,})>?\s*$`,
		GoogleVoiceRegex:   `(?i)(?:from|via)\s+google\s+voice`,
		Agents: map[string]AgentSettings{
			"C": {Enabled: true, ReplyStyle: defaultReplyStyleHint},
			"A": {Enabled: true, ReplyStyle: defaultReplyStyleHint},
			"G": {Enabled: true, ReplyStyle: defaultReplyStyleHint},
			"H": {Enabled: true, ReplyStyle: defaultReplyStyleHint},
			"M": {Enabled: true, ReplyStyle: defaultReplyStyleHint},
			"X": {Enabled: true, ReplyStyle: defaultReplyStyleHint},
			"P": {Enabled: true, ReplyStyle: defaultReplyStyleHint},
		},
		Phonebook: map[string]string{},
		SMTP: SMTPConfig{
			Port: 587,
		},
	}
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:4141"
	}
	if c.TurnTimeoutMinutes <= 0 {
		c.TurnTimeoutMinutes = 25
	}
	if c.DefaultAgent == "" {
		c.DefaultAgent = "C"
	}
	if c.CodexPrefix == "" {
		c.CodexPrefix = "C"
	}
	if c.ClaudePrefix == "" {
		c.ClaudePrefix = "A"
	}
	if c.ChatGPTPrefix == "" {
		c.ChatGPTPrefix = chatGPTSMSPrefix
	}
	if c.ClaudeChatPrefix == "" {
		c.ClaudeChatPrefix = "H"
	}
	if c.GeminiChatPrefix == "" {
		c.GeminiChatPrefix = "M"
	}
	if c.GrokChatPrefix == "" {
		c.GrokChatPrefix = "X"
	}
	if c.CopilotChatPrefix == "" {
		c.CopilotChatPrefix = "P"
	}
	if c.NewSessionCommand == "" {
		c.NewSessionCommand = "NEW"
	}
	if c.PollSeconds <= 0 {
		c.PollSeconds = 2
	}
	if c.GmailLabel == "" {
		c.GmailLabel = "FlipAiProcessed"
	}
	if c.GmailCredsPath == "" {
		c.GmailCredsPath = filepath.Join(filepath.Dir(path), "credentials.json")
	}
	if c.GmailTokenPath == "" {
		c.GmailTokenPath = filepath.Join(filepath.Dir(path), "gmail-token.json")
	}
	if c.Agents == nil {
		c.Agents = map[string]AgentSettings{}
	}
	for _, a := range []string{"C", "A", "G", "H", "M", "X", "P"} {
		s := c.Agents[a]
		if strings.TrimSpace(s.ReplyStyle) == "" || isLegacyReplyStyleHint(s.ReplyStyle) {
			s.ReplyStyle = defaultReplyStyleHint
			c.Agents[a] = s
		}
	}
	if c.Phonebook == nil {
		c.Phonebook = map[string]string{}
	}
	if c.SMTP.Port == 0 {
		c.SMTP.Port = 587
	}
	return c, nil
}

func saveConfig(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, b)
}

func isLegacyReplyStyleHint(v string) bool {
	v = strings.TrimSpace(v)
	return v == legacyReplyStyleHintSMSV1 || v == legacyReplyStyleHintSMSV0
}

func (c Config) replyStyleHintFor(agent string) string {
	s := agentSettings(c, agent)
	if strings.TrimSpace(s.ReplyStyle) == "" || isLegacyReplyStyleHint(s.ReplyStyle) {
		return defaultReplyStyleHint
	}
	return strings.TrimSpace(s.ReplyStyle)
}

func randomToken(n int) (string, error) {
	if n < 1 {
		return "", errors.New("token length must be positive")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashCode(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}

func verifyAgentCode(s AgentSettings, code string) bool {
	if !s.RequireCode {
		return true
	}
	if s.CodeHash == "" {
		return false
	}
	got := hashCode(code)
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(strings.ToLower(s.CodeHash))) == 1
}

func setAgentCode(c *Config, agent, code string) error {
	agent = strings.ToUpper(strings.TrimSpace(agent))
	if agent == "" {
		return errors.New("agent is required")
	}
	if strings.TrimSpace(code) == "" {
		return errors.New("code cannot be empty")
	}
	if c.Agents == nil {
		c.Agents = map[string]AgentSettings{}
	}
	s := c.Agents[agent]
	s.RequireCode = true
	s.CodeHash = hashCode(code)
	c.Agents[agent] = s
	return nil
}

func clearAgentCode(c *Config, agent string) {
	agent = strings.ToUpper(strings.TrimSpace(agent))
	if c.Agents == nil {
		return
	}
	s := c.Agents[agent]
	s.RequireCode = false
	s.CodeHash = ""
	c.Agents[agent] = s
}

func agentSettings(c Config, agent string) AgentSettings {
	if c.Agents == nil {
		return AgentSettings{Enabled: true, ReplyStyle: defaultReplyStyleHint}
	}
	s, ok := c.Agents[strings.ToUpper(strings.TrimSpace(agent))]
	if !ok {
		return AgentSettings{Enabled: true, ReplyStyle: defaultReplyStyleHint}
	}
	return s
}

func validateConfig(c Config) error {
	if strings.TrimSpace(c.Listen) == "" {
		return errors.New("listen address cannot be empty")
	}
	if c.TurnTimeoutMinutes < 1 || c.TurnTimeoutMinutes > 180 {
		return fmt.Errorf("turn timeout must be between 1 and 180 minutes")
	}
	return nil
}

func configPathForDataDir(dataDir string) string {
	return filepath.Join(dataDir, "config.json")
}

func ensureConfig(dataDir string) (Config, string, error) {
	path := configPathForDataDir(dataDir)
	c, err := loadConfig(path)
	if err == nil {
		return c, path, nil
	}
	if !os.IsNotExist(err) {
		return Config{}, path, err
	}
	c = defaultConfig(dataDir)
	if err := saveConfig(path, c); err != nil {
		return Config{}, path, err
	}
	return c, path, nil
}

func configMTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}
