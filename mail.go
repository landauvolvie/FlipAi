package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
)

const (
	GmailMethodAppPassword = "app_password"
	GmailMethodOAuth       = "oauth"
)

// MailClient is the small contract the bridge needs from Gmail. Both the
// Google OAuth/Gmail API backend and the App Password IMAP/SMTP backend
// implement this interface.
type MailClient interface {
	Authorized() bool
	Test(context.Context) error
	List(context.Context) ([]string, error)
	Get(context.Context, string) (GmailMessage, error)
	SendText(context.Context, string, string) error
}

type appPasswordSecret struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func appPasswordPath(dataDir string) string {
	return filepath.Join(dataDir, "gmail-app-password.dat")
}

func normalizeGmailAddress(v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("invalid Gmail address")
	}
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address == "" || !strings.Contains(a.Address, "@") {
		return "", errors.New("enter a valid Gmail address")
	}
	return strings.ToLower(a.Address), nil
}

func normalizeAppPassword(v string) (string, error) {
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "\t", "")
	v = strings.TrimSpace(v)
	if len(v) != 16 {
		return "", errors.New("Google App Password must be the 16-character app password (spaces are okay when pasting)")
	}
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("invalid App Password")
	}
	return v, nil
}

func saveAppPasswordSecret(path, email, password string) error {
	email, err := normalizeGmailAddress(email)
	if err != nil {
		return err
	}
	password, err = normalizeAppPassword(password)
	if err != nil {
		return err
	}
	b, err := json.Marshal(appPasswordSecret{Email: email, Password: password})
	if err != nil {
		return err
	}
	enc, err := protect(b)
	if err != nil {
		return fmt.Errorf("protect Gmail App Password: %w", err)
	}
	return os.WriteFile(path, enc, 0600)
}

func loadAppPasswordSecret(path string) (appPasswordSecret, error) {
	var s appPasswordSecret
	enc, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	plain, err := unprotect(enc)
	if err != nil {
		return s, fmt.Errorf("unprotect Gmail App Password: %w", err)
	}
	if err := json.Unmarshal(plain, &s); err != nil {
		return s, err
	}
	s.Email, err = normalizeGmailAddress(s.Email)
	if err != nil {
		return s, err
	}
	s.Password, err = normalizeAppPassword(s.Password)
	if err != nil {
		return s, err
	}
	return s, nil
}

func hasAppPasswordSecret(path string) bool {
	_, err := loadAppPasswordSecret(path)
	return err == nil
}

// buildConfiguredMailClient constructs only the explicitly selected Gmail
// backend. There is intentionally no default connection method.
func buildConfiguredMailClient(cfg GmailConfig, dataDir, tokenFile string) (MailClient, *GmailClient, error) {
	switch cfg.Method {
	case GmailMethodOAuth:
		g, err := NewGmailClient(cfg, tokenFile)
		if err != nil {
			return nil, nil, err
		}
		return g, g, nil
	case GmailMethodAppPassword:
		g, err := NewIMAPMailClient(cfg, appPasswordPath(dataDir))
		if err != nil {
			return nil, nil, err
		}
		return g, nil, nil
	case "":
		return nil, nil, errors.New("choose a Gmail connection method")
	default:
		return nil, nil, fmt.Errorf("unsupported Gmail connection method %q", cfg.Method)
	}
}
