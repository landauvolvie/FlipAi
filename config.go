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

const version = "0.5.2"

type Config struct {
	CodexPath          string            `json:"codexPath"`
	ClaudePath         string            `json:"claudePath"`
	Cwd                string            `json:"cwd"`
	Listen             string            `json:"listen"`
	LocalToken         string            `json:"localToken"`
	TurnTimeoutMinutes int               `json:"turnTimeoutMinutes"`
	DefaultAgent       string            `json:"defaultAgent"`
	Gmail              GmailConfig       `json:"gmail"`
	GoogleVoice        GoogleVoiceConfig `json:"googleVoice"`
	Codex              CodexConfig       `json:"codex"`
	Claude             ClaudeConfig      `json:"claude"`
	Security           SecurityConfig    `json:"security"`
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
	AllowedFrom              string `json:"allowedFrom"`
	RequiredSubjectPhrase    string `json:"requiredSubjectPhrase"`
	ReplyTo                  string `json:"replyTo"`
	ReplyMaxChars            int    `json:"replyMaxChars"`
	SendReplyViaAgentBrowser bool   `json:"sendReplyViaAgentBrowser"`
	GmailReplyFallback       bool   `json:"gmailReplyFallback"`
}

type CodexConfig struct {
	ApprovalPolicy string `json:"approvalPolicy"`
}

type ClaudeConfig struct {
	PermissionMode string `json:"permissionMode"`
	UseChrome      bool   `json:"useChrome"`
}

type SecurityConfig struct {
	CodeSalt string `json:"codeSalt,omitempty"`
	CodeHash string `json:"codeHash,omitempty"`
}

type State struct {
	CodexThreadID       string    `json:"codexThreadId,omitempty"`
	ClaudeSessionID     string    `json:"claudeSessionId,omitempty"`
	GmailBaselineUnix   int64     `json:"gmailBaselineUnix,omitempty"`
	ProcessedMessageIDs []string  `json:"processedMessageIds,omitempty"`
	LastMessageID       string    `json:"lastMessageId,omitempty"`
	LastRunAt           time.Time `json:"lastRunAt,omitempty"`
	LastAgent           string    `json:"lastAgent,omitempty"`
}

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
		DefaultAgent: "C",
		Gmail:        GmailConfig{CredentialsFile: filepath.Join(dataDir, "google-credentials.json"), PollSeconds: 1, SearchQuery: `subject:"new text message from" newer_than:2d`, SubjectPhrase: "new text message from"},
		GoogleVoice:  GoogleVoiceConfig{RequiredSubjectPhrase: "new text message from", ReplyMaxChars: 300, SendReplyViaAgentBrowser: true, GmailReplyFallback: true},
		Codex:        CodexConfig{ApprovalPolicy: "on-request"},
		Claude:       ClaudeConfig{PermissionMode: "acceptEdits", UseChrome: true},
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
	if cfg.Codex.ApprovalPolicy == "" || cfg.Codex.ApprovalPolicy == "unlessTrusted" {
		cfg.Codex.ApprovalPolicy = "on-request"
	}
	if cfg.Claude.PermissionMode == "" || cfg.Claude.PermissionMode == "auto" || cfg.Claude.PermissionMode == "bypassPermissions" {
		cfg.Claude.PermissionMode = "acceptEdits"
	}
	if cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {
		cfg.DefaultAgent = "C"
	}
	if cfg.LocalToken == "" {
		cfg.LocalToken, err = secureRandomToken(24)
		if err != nil {
			return cfg, err
		}
	}
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
