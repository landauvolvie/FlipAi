package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Claude Code CLI keeps its own sign-in, separate from the Claude desktop app.
// That browser session expires and its refresh can fail outright, which strands
// an unattended bridge until someone runs `claude /login` at the machine.
//
// `claude setup-token` mints a long-lived token instead. It is not permanent —
// the CLI reports lifetimes up to a year and can still ask for a fresh one — but
// it turns a session that lapses in hours or days into one measured in months.
//
// The token is optional. With none stored, FlipAi behaves exactly as before and
// the normal CLI login is used, so a token that ever misbehaves can simply be
// cleared.
func claudeTokenPath(dataDir string) string {
	return filepath.Join(dataDir, "claude-oauth-token.dat")
}

// normalizeClaudeToken rejects obviously malformed input early. The token is
// passed to a child process through the environment, so an embedded newline
// could otherwise smuggle a second variable in.
func normalizeClaudeToken(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("paste the token printed by `claude setup-token`")
	}
	if strings.ContainsAny(v, "\r\n\x00") {
		return "", errors.New("the Claude token must be a single line with no spaces or line breaks")
	}
	if strings.ContainsAny(v, " \t") {
		return "", errors.New("the Claude token must not contain spaces; copy only the token itself")
	}
	if len(v) < 20 {
		return "", errors.New("that does not look like a Claude token; run `claude setup-token` and paste the whole value")
	}
	return v, nil
}

// saveClaudeToken stores the token encrypted with Windows DPAPI, mirroring how
// the Gmail App Password is kept. It is never written into bridge.json and never
// rendered back into the Settings page.
func saveClaudeToken(path, token string) error {
	token, err := normalizeClaudeToken(token)
	if err != nil {
		return err
	}
	enc, err := protect([]byte(token))
	if err != nil {
		return err
	}
	return os.WriteFile(path, enc, 0600)
}

func loadClaudeToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dec, err := unprotect(b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(dec)), nil
}

func hasClaudeToken(path string) bool {
	tok, err := loadClaudeToken(path)
	return err == nil && tok != ""
}

func clearClaudeToken(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// newClaudeClient builds a Claude client for this install, wiring in the stored
// long-lived token when the user has set one.
func (a *App) newClaudeClient(cfg Config) *ClaudeClient {
	token, _ := loadClaudeToken(claudeTokenPath(a.dataDir))
	return NewClaudeClientWithToken(cfg.ClaudePath, cfg.Cwd, cfg.Claude, token)
}

// secretishRE matches credential shapes that must never reach an SMS, the
// Settings page, or a log file. Claude Code echoes CLAUDE_CODE_OAUTH_TOKEN in
// some of its own error text, and FlipAi puts raw CLI output into the errors it
// reports, so without this a token could ride out in a text message.
var secretishRE = regexp.MustCompile(`(?i)(sk-ant-[A-Za-z0-9_-]{8,}|ya29\.[A-Za-z0-9._-]{8,}|CLAUDE_CODE_OAUTH_TOKEN=[^\s"']+)`)

// redactSecrets removes the configured token and any credential-shaped text.
// The exact token is replaced first so it is caught even if it does not match
// the generic pattern.
func redactSecrets(s, token string) string {
	if s == "" {
		return s
	}
	if t := strings.TrimSpace(token); len(t) >= 8 {
		s = strings.ReplaceAll(s, t, "[redacted]")
	}
	return secretishRE.ReplaceAllString(s, "[redacted]")
}

// redact scrubs anything credential-shaped out of text about to be surfaced.
func (c *ClaudeClient) redact(s string) string {
	if c == nil {
		return redactSecrets(s, "")
	}
	return redactSecrets(s, c.token)
}
