package main

import (
	"fmt"
	"os"
	"strings"
)

// Stored credentials are normally protected for the signed-in Windows account.
// A task that starts before anyone signs in has no such account context, so
// those credentials have to be protected for the PC instead. Switching the
// "start before sign-in" option flips the scope and rewrites what is already on
// disk; nothing else changes about where secrets live or who can reach them.

func (a *App) protectedSecretFiles() []string {
	return []string{
		appPasswordPath(a.dataDir),
		claudeTokenPath(a.dataDir),
		a.tokenPath,
	}
}

// applySecretScope re-protects every stored credential in the requested scope.
// A file that cannot be re-read is reported rather than silently dropped: the
// caller needs to know the credential must be entered again.
func (a *App) applySecretScope(machine bool) error {
	setSecretScope(machine)
	for _, path := range a.protectedSecretFiles() {
		if err := reprotectFile(path); err != nil {
			return err
		}
	}
	return nil
}

func reprotectFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	plain, err := unprotect(raw)
	if err != nil {
		return fmt.Errorf("could not re-read %s for re-protection: %w", secretLabel(path), err)
	}
	enc, err := protect(plain)
	if err != nil {
		return fmt.Errorf("could not re-protect %s: %w", secretLabel(path), err)
	}
	return os.WriteFile(path, enc, 0o600)
}

func secretLabel(path string) string {
	switch {
	case strings.Contains(path, "gmail-app-password"):
		return "the Gmail App Password"
	case strings.Contains(path, "claude-oauth-token"):
		return "the Claude token"
	case strings.Contains(path, "google-token"):
		return "the Google OAuth token"
	default:
		return "a stored credential"
	}
}
