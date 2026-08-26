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

const version = "0.27.0"

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

// UIConfig holds desktop-window preferences. Nothing here changes how SMS is
// routed; these are only the app-window behaviours the Settings page offers.
type UIConfig struct {
	Theme   string `json:"theme"`   // light, dark, or system
	Compact bool   `json:"compact"` // tighter spacing in the desktop window

	// Alerts shows an in-window banner when a new error reaches the activity
	// log while FlipAi is open; AlertSound adds a short tone to that banner.
	Alerts     bool `json:"alerts"`
	AlertSound bool `json:"alertSound"`
	CloseToTray bool `json:"closeToTray"`
}
