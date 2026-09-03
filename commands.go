package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultCodexPrefix       = "C"
	defaultClaudePrefix      = "A"
	defaultChatGPTPrefix     = "G"
	defaultClaudeChatPrefix  = "H"
	defaultNewSessionCommand = "NEW"
)

func cleanCommandToken(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimSpace(strings.TrimSuffix(v, ":"))
	return v
}

func validateCommandToken(v, label string) (string, error) {
	v = cleanCommandToken(v)
	if v == "" {
		return "", fmt.Errorf("%s cannot be empty", label)
	}
	if utf8.RuneCountInString(v) > 24 {
		return "", fmt.Errorf("%s must be 24 characters or fewer", label)
	}
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-.", r) {
			continue
		}
		return "", fmt.Errorf("%s may contain only letters, numbers, underscore, dash, or period", label)
	}
	return v, nil
}

func normalizeCommandToken(v, fallback string) string {
	if token, err := validateCommandToken(v, "command"); err == nil {
		return token
	}
	return fallback
}

func configuredCodexPrefix(cfg Config) string {
	return normalizeCommandToken(cfg.CodexPrefix, defaultCodexPrefix)
}

func configuredClaudePrefix(cfg Config) string {
	return normalizeCommandToken(cfg.ClaudePrefix, defaultClaudePrefix)
}

func configuredChatGPTPrefix(cfg Config) string {
	return normalizeCommandToken(cfg.ChatGPTPrefix, defaultChatGPTPrefix)
}

func configuredClaudeChatPrefix(cfg Config) string {
	return normalizeCommandToken(cfg.ClaudeChatPrefix, defaultClaudeChatPrefix)
}

func configuredNewSessionCommand(cfg Config) string {
	return normalizeCommandToken(cfg.NewSessionCommand, defaultNewSessionCommand)
}

// stripAgentCommandPrefix recognizes the normal "PREFIX: text" routing form.
func stripAgentCommandPrefix(raw, prefix string) (string, bool) {
	raw = strings.TrimSpace(raw)
	i := strings.Index(raw, ":")
	if i <= 0 || !strings.EqualFold(strings.TrimSpace(raw[:i]), prefix) {
		return "", false
	}
	return strings.TrimSpace(raw[i+1:]), true
}

// isAgentNewSession keeps the historical "C NEW" and "C: NEW" forms while
// allowing both the prefix and NEW word to be customized.
func isAgentNewSession(raw, prefix, newSession string) bool {
	if tail, ok := stripAgentCommandPrefix(raw, prefix); ok {
		return strings.EqualFold(strings.TrimSpace(tail), newSession)
	}
	fields := strings.Fields(strings.TrimSpace(raw))
	return len(fields) == 2 && strings.EqualFold(fields[0], prefix) && strings.EqualFold(fields[1], newSession)
}
