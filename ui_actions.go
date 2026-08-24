package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

// updateConfig applies one change to the saved configuration. Every page action
// funnels through here so a partial form can never drop the settings it does not
// show: the mutation starts from the current config and only touches its own
// fields.
func (a *App) updateConfig(mutate func(cfg *Config) error) error {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if err := mutate(&cfg); err != nil {
		return err
	}
	syncAllowedNumbers(&cfg.GoogleVoice)
	if err := saveConfig(a.configPath, cfg); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	b := a.bridge
	a.mu.Unlock()
	if b != nil {
		b.SetPaused(cfg.Paused)
	}
	return nil
}

// formFlag reads a checkbox that is paired with a hidden "0" field, so an
// unchecked box arrives as an explicit false instead of a missing key. The
// second return reports whether the form carried the field at all.
func formFlag(r *http.Request, name string) (bool, bool) {
	vals, ok := r.Form[name]
	if !ok || len(vals) == 0 {
		return false, false
	}
	v := strings.TrimSpace(vals[len(vals)-1])
	return v == "1" || v == "on" || v == "true", true
}

func formInt(r *http.Request, name string, min, max int) (int, bool, error) {
	if !r.Form.Has(name) {
		return 0, false, nil
	}
	raw := strings.TrimSpace(r.FormValue(name))
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, false, fmt.Errorf("enter a whole number between %d and %d", min, max)
	}
	return n, true, nil
}

// ---------------------------------------------------------------------------
// Bridge control
// ---------------------------------------------------------------------------

func (a *App) pauseBridge(w http.ResponseWriter, r *http.Request) {
	if err := a.updateConfig(func(cfg *Config) error { cfg.Paused = true; return nil }); err != nil {
		renderResult(w, r, 500, false, "Could not pause FlipAi", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("warn", "bridge", "FlipAi paused from the desktop app; new texts stay unread", "", "", "")
	redirectTo(w, r, "/", "paused")
}

func (a *App) resumeBridge(w http.ResponseWriter, r *http.Request) {
	if err := a.updateConfig(func(cfg *Config) error { cfg.Paused = false; return nil }); err != nil {
		renderResult(w, r, 500, false, "Could not resume FlipAi", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "bridge", "FlipAi resumed from the desktop app", "", "", "")
	redirectTo(w, r, "/", "resumed")
}

func (a *App) restartBridge(w http.ResponseWriter, r *http.Request) {
	activityLogForStatePath(a.statePath).Add("info", "bridge", "Restart requested from the desktop app", "", "", "")
	redirectTo(w, r, "/advanced", "restarting")
	go a.restartSoon()
}

// ---------------------------------------------------------------------------
// Connections
// ---------------------------------------------------------------------------

func (a *App) saveConnections(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		renderResult(w, r, 400, false, "Could not read the connection form", err.Error())
		return
	}
	a.mu.Lock()
	oldMethod, oldEmail := a.cfg.Gmail.Method, a.cfg.Gmail.Email
	credentialsFile := a.cfg.Gmail.CredentialsFile
	a.mu.Unlock()

	method := strings.TrimSpace(r.FormValue("gmailMethod"))
	if method != "" && method != GmailMethodAppPassword && method != GmailMethodOAuth {
		renderResult(w, r, 400, false, "Choose a Gmail method", "Select the App Password method or your own Google API project.")
		return
	}
	credentialsChanged := false
	if method == GmailMethodOAuth {
		if f, h, err := r.FormFile("credentials"); err == nil {
			defer f.Close()
			if !strings.HasSuffix(strings.ToLower(h.Filename), ".json") {
				renderResult(w, r, 400, false, "Wrong OAuth file", "Upload the JSON file created for a Google OAuth Desktop app.")
				return
			}
			raw, err := io.ReadAll(io.LimitReader(f, 2<<20))
			if err != nil {
				renderResult(w, r, 400, false, "Could not read the OAuth file", err.Error())
				return
			}
			var cr googleCredentials
			if json.Unmarshal(raw, &cr) != nil || cr.Installed.ClientID == "" {
				renderResult(w, r, 400, false, "OAuth file is not a Desktop app", "Create OAuth credentials with application type Desktop app and upload that JSON.")
				return
			}
			if err := os.WriteFile(credentialsFile, raw, 0600); err != nil {
				renderResult(w, r, 500, false, "Could not store the OAuth file", err.Error())
				return
			}
			_ = os.Remove(a.tokenPath)
			credentialsChanged = true
		}
		if !credentialsChanged && !fileExists(credentialsFile) {
			renderResult(w, r, 400, false, "OAuth credentials required", "Upload your Google OAuth Desktop credentials JSON.")
			return
		}
	}

	var email string
	if method == GmailMethodAppPassword {
		var err error
		email, err = normalizeGmailAddress(r.FormValue("gmailEmail"))
		if err != nil {
			renderResult(w, r, 400, false, "Gmail address is invalid", err.Error())
			return
		}
		pass := strings.TrimSpace(r.FormValue("appPassword"))
		secretFile := appPasswordPath(a.dataDir)
		if pass != "" {
			if err := saveAppPasswordSecret(secretFile, email, pass); err != nil {
				renderResult(w, r, 400, false, "App Password was not saved", err.Error())
				return
			}
		} else {
			existing, err := loadAppPasswordSecret(secretFile)
			if err != nil {
				renderResult(w, r, 400, false, "App Password required", "Enter the Google App Password the first time you choose the App Password method.")
				return
			}
			if !strings.EqualFold(existing.Email, email) {
				renderResult(w, r, 400, false, "Gmail account changed", "Enter a new App Password for the new Gmail address.")
				return
			}
		}
	}

	err := a.updateConfig(func(cfg *Config) error {
		cfg.Gmail.Method = method
		switch method {
		case GmailMethodAppPassword:
			cfg.Gmail.Email = email
		case GmailMethodOAuth:
			cfg.Gmail.Email = ""
			if oldMethod != GmailMethodOAuth {
				_ = os.Remove(a.tokenPath)
			}
		}
		if r.Form.Has("subjectPhrase") {
			phrase := strings.TrimSpace(r.FormValue("subjectPhrase"))
			if phrase == "" {
				phrase = "new text message from"
			}
			cfg.Gmail.SubjectPhrase = phrase
			cfg.GoogleVoice.RequiredSubjectPhrase = phrase
		}
		return nil
	})
	if err != nil {
		renderResult(w, r, 500, false, "Could not save the connection", err.Error())
		return
	}
	// A different mailbox or credential means the old read checkpoint no longer
	// describes this account, so start from a fresh baseline.
	if oldMethod != method || credentialsChanged || (method == GmailMethodAppPassword && !strings.EqualFold(oldEmail, email)) {
		a.resetMailCheckpoint()
	}
	redirectTo(w, r, "/connections", "saved-restart")
	go a.restartSoon()
}

// flowTest checks the whole inbound path a text has to travel — mailbox access,
// an allowed sender, a usable security code — and reports the first gap.
func (a *App) flowTest(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	if s.GmailMethod == "" {
		renderResult(w, r, 400, false, "Gmail is not connected", "Choose the App Password method or your own Google API project first.")
		return
	}
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	mc, _, err := buildConfiguredMailClient(cfg.Gmail, a.dataDir, a.tokenPath)
	if err != nil {
		a.recordCheck("gmail", false, err.Error())
		renderResult(w, r, 400, false, "Gmail setup is incomplete", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := mc.Test(ctx); err != nil {
		a.recordCheck("gmail", false, err.Error())
		activityLogForStatePath(a.statePath).Add("error", "gmail", "Message-flow test failed: "+truncate(err.Error(), 200), "", "", "")
		renderResult(w, r, 500, false, "Gmail test failed", err.Error())
		return
	}
	a.recordCheck("gmail", true, gmailMethodLabel(cfg.Gmail.Method)+" reachable")
	if s.AllowedCount == 0 {
		renderResult(w, r, 400, false, "No allowed numbers", "Gmail is reachable, but no phone number is allowed yet, so every incoming text would be ignored. Add your mobile number on the Phone page.")
		return
	}
	if s.RequireCode && !s.HasCode {
		renderResult(w, r, 400, false, "Security code missing", "Gmail is reachable and numbers are allowed, but code protection is on with no code set. Set a code on the Phone page or turn the requirement off.")
		return
	}
	activityLogForStatePath(a.statePath).Add("success", "gmail", "Message-flow test passed", "", "", "")
	detail := fmt.Sprintf("Gmail is reachable through %s and %s can reach your agents.", gmailMethodLabel(cfg.Gmail.Method), plural(s.AllowedCount, "number"))
	if s.RequireCode {
		detail += " Texts must start with your security code."
	}
	detail += "\n\nSend a text to your Google Voice number to run the whole path end to end."
	renderResult(w, r, 200, true, "Message flow looks ready", detail)
}

// ---------------------------------------------------------------------------
// Agents
// ---------------------------------------------------------------------------

func (a *App) saveAgents(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the agent form", err.Error())
		return
	}
	if r.Form.Has("clearClaudeToken") {
		if clear, _ := formFlag(r, "clearClaudeToken"); clear {
			if err := clearClaudeToken(claudeTokenPath(a.dataDir)); err != nil {
				renderResult(w, r, 500, false, "Could not remove the Claude token", err.Error())
				return
			}
		}
	}
	if token := strings.TrimSpace(r.FormValue("claudeToken")); token != "" {
		if err := saveClaudeToken(claudeTokenPath(a.dataDir), token); err != nil {
			renderResult(w, r, 400, false, "Claude token is invalid", err.Error())
			return
		}
	}
	err := a.updateConfig(func(cfg *Config) error {
		for field, target := range map[string]*string{
			"codexPath":  &cfg.CodexPath,
			"claudePath": &cfg.ClaudePath,
			"cwd":        &cfg.Cwd,
			"codexCwd":   &cfg.CodexCwd,
			"claudeCwd":  &cfg.ClaudeCwd,
		} {
			if r.Form.Has(field) {
				*target = strings.TrimSpace(r.FormValue(field))
			}
		}
		// A per-agent folder equal to the shared one is stored as empty so the
		// agent keeps following the shared folder when that changes.
		if cfg.CodexCwd == cfg.Cwd {
			cfg.CodexCwd = ""
		}
		if cfg.ClaudeCwd == cfg.Cwd {
			cfg.ClaudeCwd = ""
		}
		if v, ok := formFlag(r, "claudeUseChrome"); ok {
			cfg.Claude.UseChrome = v
		}
		if r.Form.Has("defaultAgent") {
			if v := strings.ToUpper(strings.TrimSpace(r.FormValue("defaultAgent"))); v == "A" || v == "C" {
				cfg.DefaultAgent = v
			}
		}
		if r.Form.Has("codexPrefix") || r.Form.Has("claudePrefix") || r.Form.Has("newSessionCommand") {
			codexPrefix, claudePrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredNewSessionCommand(*cfg)
			var err error
			if r.Form.Has("codexPrefix") {
				codexPrefix, err = validateCommandToken(r.FormValue("codexPrefix"), "Codex prefix")
				if err != nil {
					return err
				}
			}
			if r.Form.Has("claudePrefix") {
				claudePrefix, err = validateCommandToken(r.FormValue("claudePrefix"), "Claude prefix")
				if err != nil {
					return err
				}
			}
			if strings.EqualFold(codexPrefix, claudePrefix) {
				return fmt.Errorf("Codex and Claude prefixes must be different")
			}
			if r.Form.Has("newSessionCommand") {
				newSession, err = validateCommandToken(r.FormValue("newSessionCommand"), "new-session command")
				if err != nil {
					return err
				}
			}
			cfg.CodexPrefix, cfg.ClaudePrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, newSession
		}
		if n, ok, err := formInt(r, "turnTimeout", 1, 600); err != nil {
			return fmt.Errorf("turn timeout: %w", err)
		} else if ok {
			cfg.TurnTimeoutMinutes = n
		}
		if r.Form.Has("claudeSessionMode") {
			// Anything unrecognised normalises to per-message, so a stale form
			// post can never leave the bridge in a mode it does not implement.
			cfg.Claude.SessionMode = normalizeClaudeSessionMode(r.FormValue("claudeSessionMode"))
		}
		if r.Form.Has("permissionMode") {
			switch v := strings.TrimSpace(r.FormValue("permissionMode")); v {
			case "bypassPermissions", "acceptEdits", "dontAsk", "plan", "default":
				cfg.Claude.PermissionMode = v
			}
		}
		return nil
	})
	if err != nil {
		renderResult(w, r, 400, false, "Agent settings were not saved", err.Error())
		return
	}
	redirectTo(w, r, "/agents", "saved-restart")
	go a.restartSoon()
}

// ---------------------------------------------------------------------------
// Phone
// ---------------------------------------------------------------------------

func (a *App) savePhone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the reply settings", err.Error())
		return
	}
	err := a.updateConfig(func(cfg *Config) error {
		if n, ok, err := formInt(r, "replyMaxChars", 80, 1000); err != nil {
			return fmt.Errorf("reply length: %w", err)
		} else if ok {
			cfg.GoogleVoice.ReplyMaxChars = n
		}
		if n, ok, err := formInt(r, "maxReplyParts", 1, 10); err != nil {
			return fmt.Errorf("reply parts: %w", err)
		} else if ok {
			cfg.GoogleVoice.MaxReplyParts = n
		}
		if n, ok, err := formInt(r, "progressInterval", 30, 3600); err != nil {
			return fmt.Errorf("progress interval: %w", err)
		} else if ok {
			cfg.GoogleVoice.ProgressIntervalSeconds = n
		}
		if v, ok := formFlag(r, "replyAck"); ok {
			cfg.GoogleVoice.ReplyAck = v
		}
		if v, ok := formFlag(r, "progressUpdates"); ok {
			cfg.GoogleVoice.ProgressUpdates = v
		}
		return nil
	})
	if err != nil {
		renderResult(w, r, 400, false, "Reply settings were not saved", err.Error())
		return
	}
	redirectTo(w, r, "/phone", "saved-restart")
	go a.restartSoon()
}

func (a *App) savePhoneSecurity(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the security settings", err.Error())
		return
	}
	code := strings.TrimSpace(r.FormValue("securityCode"))
	err := a.updateConfig(func(cfg *Config) error {
		if code != "" {
			if err := setSecurityCode(cfg, code); err != nil {
				return err
			}
		}
		require, ok := formFlag(r, "requireCode")
		if !ok {
			return nil
		}
		if require && cfg.Security.CodeHash == "" {
			return fmt.Errorf("set a security code first — enter one under Change code, then turn the requirement on")
		}
		if !require && cfg.Security.CodeHash == "" {
			// Routing still checks a hash internally; an unguessable placeholder
			// keeps that path intact while the requirement is off.
			placeholder, e := secureRandomToken(24)
			if e != nil {
				return e
			}
			if e := setSecurityCode(cfg, placeholder); e != nil {
				return e
			}
		}
		cfg.Security.RequireCode = require
		return nil
	})
	if err != nil {
		renderResult(w, r, 400, false, "Security settings were not saved", err.Error())
		return
	}
	redirectTo(w, r, "/phone", "saved-restart")
	go a.restartSoon()
}

func (a *App) addPhoneNumber(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the number", err.Error())
		return
	}
	number := r.FormValue("number")
	err := a.updateConfig(func(cfg *Config) error {
		return addAllowedNumber(&cfg.GoogleVoice, number, r.FormValue("label"))
	})
	if err != nil {
		renderResult(w, r, 400, false, "Number was not added", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "security", "Allowed phone number added from the desktop app", number, "", "")
	redirectTo(w, r, "/phone", "number-added")
	go a.restartSoon()
}

func (a *App) removePhoneNumber(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the number", err.Error())
		return
	}
	number := r.FormValue("number")
	err := a.updateConfig(func(cfg *Config) error {
		return removeAllowedNumber(&cfg.GoogleVoice, number)
	})
	if err != nil {
		renderResult(w, r, 400, false, "Number was not removed", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "security", "Allowed phone number removed from the desktop app", number, "", "")
	redirectTo(w, r, "/phone", "number-removed")
	go a.restartSoon()
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the settings form", err.Error())
		return
	}
	err := a.updateConfig(func(cfg *Config) error {
		if r.Form.Has("theme") {
			cfg.UI.Theme = normalizeTheme(r.FormValue("theme"))
		}
		for field, target := range map[string]*bool{
			"compact":     &cfg.UI.Compact,
			"alerts":      &cfg.UI.Alerts,
			"alertSound":  &cfg.UI.AlertSound,
			"closeToTray": &cfg.UI.CloseToTray,
		} {
			if v, ok := formFlag(r, field); ok {
				*target = v
			}
		}
		return nil
	})
	if err != nil {
		renderResult(w, r, 500, false, "Settings were not saved", err.Error())
		return
	}
	// Window preferences take effect on the next render, with no restart.
	redirectTo(w, r, "/settings", "saved")
}

// saveUpdates stores the automatic-update choice and the background check
// interval. Both take effect on the next check without a restart, because
// watchForUpdates reads them from the live config each time round.
func (a *App) saveUpdates(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the update settings", err.Error())
		return
	}
	err := a.updateConfig(func(cfg *Config) error {
		if v, ok := formFlag(r, "autoUpdate"); ok {
			cfg.Updates.Automatic = v
		}
		if n, ok, err := formInt(r, "updateCheckMinutes", updateCheckMinutesMin, updateCheckMinutesMax); err != nil {
			return fmt.Errorf("update check interval: %w", err)
		} else if ok {
			cfg.Updates.CheckMinutes = n
			// Clear the retired value so the migration cannot later override
			// the cadence the user just chose.
			cfg.Updates.CheckHours = 0
		}
		return nil
	})
	if err != nil {
		renderResult(w, r, 400, false, "Update settings were not saved", err.Error())
		return
	}
	redirectTo(w, r, "/settings", "saved")
}

func (a *App) saveStartup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the startup setting", err.Error())
		return
	}
	enable, ok := formFlag(r, "startup")
	if !ok {
		redirectTo(w, r, "/settings", "")
		return
	}
	if enable {
		exe, err := os.Executable()
		if err == nil {
			err = installAutostart(exe)
		}
		if err != nil {
			renderResult(w, r, 500, false, "Could not enable startup", err.Error()+
				"\n\nFlipAi never requests administrator rights. On a managed PC, policy may block user startup entries.")
			return
		}
		activityLogForStatePath(a.statePath).Add("success", "startup", "Start with Windows enabled", "", "", "")
		redirectTo(w, r, "/settings", "startup-on")
		return
	}
	if err := uninstallAutostart(); err != nil {
		renderResult(w, r, 500, false, "Could not disable startup", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "startup", "Start with Windows disabled", "", "", "")
	redirectTo(w, r, "/settings", "startup-off")
}

// resetSetup returns this install to its defaults. The loopback listen address
// and session token are preserved so the open window keeps working; everything
// the user configured, including stored secrets, is removed.
func (a *App) resetSetup(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	old := a.cfg
	a.mu.Unlock()

	fresh := defaultConfig(a.dataDir)
	fresh.Listen = old.Listen
	fresh.LocalToken = old.LocalToken
	if err := saveConfig(a.configPath, fresh); err != nil {
		renderResult(w, r, 500, false, "Could not reset FlipAi", err.Error())
		return
	}
	for _, path := range []string{
		old.Gmail.CredentialsFile,
		a.tokenPath,
		appPasswordPath(a.dataDir),
		claudeTokenPath(a.dataDir),
		a.statePath,
	} {
		if path != "" {
			_ = os.Remove(path)
		}
	}
	a.mu.Lock()
	a.cfg = fresh
	a.mu.Unlock()
	activityLogForStatePath(a.statePath).Add("warn", "host", "Setup was reset from the desktop app", "", "", "")
	redirectTo(w, r, "/", "reset")
	go a.restartSoon()
}

// saveBootStartup turns the pre-sign-in startup task on or off. Enabling it is
// the one place FlipAi asks Windows for administrator approval, and it happens
// here — never during installation.
func (a *App) saveBootStartup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the startup setting", err.Error())
		return
	}
	enable, ok := formFlag(r, "bootStartup")
	if !ok {
		redirectTo(w, r, "/settings", "")
		return
	}
	if enable {
		// The task runs without an interactive logon, so stored credentials
		// have to be readable without one before the task is created.
		if err := a.applySecretScope(true); err != nil {
			_ = a.applySecretScope(false)
			renderResult(w, r, 500, false, "Credentials could not be re-protected", err.Error()+
				"\n\nFlipAi left them protected for your account and did not change startup.")
			return
		}
		if err := enableBootStartup(a.dataDir); err != nil {
			_ = a.applySecretScope(false)
			renderResult(w, r, 500, false, "Start before sign-in was not enabled", err.Error())
			return
		}
		if err := a.updateConfig(func(cfg *Config) error {
			cfg.Security.MachineScopeSecrets = true
			return nil
		}); err != nil {
			renderResult(w, r, 500, false, "Startup task created, but the setting was not saved", err.Error())
			return
		}
		activityLogForStatePath(a.statePath).Add("success", "startup", "Start before sign-in enabled (scheduled task created)", "", "", "")
		redirectTo(w, r, "/settings", "boot-on")
		return
	}
	if err := disableBootStartup(a.dataDir); err != nil {
		renderResult(w, r, 500, false, "Start before sign-in was not turned off", err.Error())
		return
	}
	if err := a.applySecretScope(false); err != nil {
		renderResult(w, r, 500, false, "Credentials could not be re-protected", err.Error())
		return
	}
	if err := a.updateConfig(func(cfg *Config) error {
		cfg.Security.MachineScopeSecrets = false
		return nil
	}); err != nil {
		renderResult(w, r, 500, false, "Startup task removed, but the setting was not saved", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "startup", "Start before sign-in disabled", "", "", "")
	redirectTo(w, r, "/settings", "boot-off")
}

// ---------------------------------------------------------------------------
// Updates
// ---------------------------------------------------------------------------

func (a *App) updateCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	info := a.checkForUpdate(ctx, true)
	if info.Error != "" {
		renderResult(w, r, 200, false, "Could not check for updates", info.Error+
			"\n\nFlipAi keeps working; it only needs a connection to github.com to see whether a newer release exists.")
		return
	}
	if info.Newer() {
		redirectTo(w, r, "/settings", "update-found")
		return
	}
	redirectTo(w, r, "/settings", "update-current")
}

// updateInstall downloads the published installer, verifies it against the
// checksum published with the release, and runs it in place. The installer
// recognises the existing install and updates it without asking setup
// questions again.
func (a *App) updateInstall(w http.ResponseWriter, r *http.Request) {
	info := loadUpdateState(a.statePath)
	if !info.Newer() {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		info = a.checkForUpdate(ctx, true)
		cancel()
	}
	if !info.Newer() {
		renderResult(w, r, 200, true, "FlipAi is up to date", "No newer release is published right now.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	path, err := downloadUpdate(ctx, info)
	if err != nil {
		activityLogForStatePath(a.statePath).Add("error", "host", "Update download failed: "+truncate(err.Error(), 200), "", "", "")
		renderResult(w, r, 500, false, "Update could not be downloaded", err.Error())
		return
	}
	// The user pressed Install in the app, so bring the window back with it.
	if err := runUpdateInstaller(path, true); err != nil {
		renderResult(w, r, 500, false, "Update could not be started", err.Error()+
			"\n\nThe verified installer is saved at "+path+" if you want to run it yourself.")
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "host", "Installing FlipAi "+info.Version, "", "", "")
	renderResult(w, r, 200, true, "Installing FlipAi "+info.Version,
		"The verified installer is running now. FlipAi stops for a few seconds and starts again on the new version with your settings intact.")
}

// ---------------------------------------------------------------------------
// Diagnostics
// ---------------------------------------------------------------------------

// exportLogs streams a zip of the local log files. Only FlipAi's own logs are
// included, and they hold statuses rather than message content.
func (a *App) exportLogs(w http.ResponseWriter, r *http.Request) {
	name := "flipai-logs-" + time.Now().Format("20060102-1504") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	z := zip.NewWriter(w)
	defer z.Close()
	for _, file := range []string{"activity.jsonl", "bridge.log"} {
		path := filepath.Join(a.dataDir, file)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		f, err := z.Create(file)
		if err != nil {
			return
		}
		if _, err := f.Write(raw); err != nil {
			return
		}
	}
	summary, err := z.Create("flipai-status.txt")
	if err != nil {
		return
	}
	s := a.status()
	fmt.Fprintf(summary, "FlipAi %s\nExported %s\n\nGmail: %s (%s)\nBridge running: %v\nPaused: %v\nAllowed numbers: %d\nSecurity code required: %v\nCodex found: %v\nClaude found: %v\nLast agent: %s\n",
		s.Version, time.Now().Format(time.RFC3339), s.GmailMethodLabel, readyText(s.GmailReady, "connected", "not connected"),
		s.Running, s.Paused, s.AllowedCount, s.RequireCode, s.CodexFound, s.ClaudeFound, s.LastAgent)
}

// openLocalFolder shows one of FlipAi's own folders in the file manager. The
// choice is a fixed keyword, never a path from the request.
func (a *App) openLocalFolder(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	var target string
	switch r.URL.Query().Get("which") {
	case "data", "logs":
		target = a.dataDir
	case "codex":
		target = cfg.codexWorkingDir()
	case "claude":
		target = cfg.claudeWorkingDir()
	default:
		renderResult(w, r, 400, false, "Unknown folder", "That folder is not one FlipAi can open.")
		return
	}
	if !directoryExists(target) {
		renderResult(w, r, 400, false, "Folder not found", target+" does not exist on this PC.")
		return
	}
	if err := openFolder(target); err != nil {
		renderResult(w, r, 500, false, "Could not open the folder", err.Error())
		return
	}
	renderResult(w, r, 200, true, "Folder opened", target+" was opened in a file manager window.")
}

// healthCheck re-runs the checks the Advanced page reports and states the
// result plainly, including what is not yet verified.
func (a *App) healthCheck(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	var problems []string
	if !s.GmailReady {
		problems = append(problems, "Gmail is not connected.")
	}
	if s.AllowedCount == 0 {
		problems = append(problems, "No phone number is allowed to send commands.")
	}
	if s.RequireCode && !s.HasCode {
		problems = append(problems, "Code protection is on but no security code is set.")
	}
	if !s.CodexFound && !s.ClaudeFound {
		problems = append(problems, "Neither the Codex nor the Claude executable was found.")
	}
	if !s.Running {
		problems = append(problems, "The SMS bridge is not running yet.")
	}
	if s.Paused {
		problems = append(problems, "FlipAi is paused, so new texts are not being read.")
	}
	if len(problems) > 0 {
		renderResult(w, r, 200, false, "Health check found issues", strings.Join(problems, "\n"))
		return
	}
	renderResult(w, r, 200, true, "All systems operational",
		fmt.Sprintf("Gmail is connected through %s, %s can send commands, and the bridge is watching the mailbox.\n\nHost uptime: %s",
			s.GmailMethodLabel, plural(s.AllowedCount, "number"), humanDuration(s.Uptime)))
}

// ---------------------------------------------------------------------------
// JSON endpoints
// ---------------------------------------------------------------------------

func (a *App) statusJSON(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	runningLabel := "FlipAi is idle"
	switch {
	case s.Paused:
		runningLabel = "FlipAi is paused"
	case s.Running:
		runningLabel = "FlipAi is running"
	}
	statusLabel := "Setup needed"
	switch {
	case s.Paused:
		statusLabel = "Paused"
	case s.Running:
		statusLabel = "Running"
	}
	lastSync := "Not checked yet"
	if !s.LastPollAt.IsZero() {
		lastSync = humanSince(s.LastPollAt)
	}
	writeJSON(w, map[string]any{
		"version":      s.Version,
		"running":      s.Running,
		"paused":       s.Paused,
		"busy":         s.Busy,
		"gmailReady":   s.GmailReady,
		"allowedCount": s.AllowedCount,
		"runningLabel": runningLabel,
		"statusLabel":  statusLabel,
		"lastSync":     lastSync,
	})
}

type folderEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// foldersJSON powers the in-app folder picker. It lists directory names only,
// never file contents, and it starts from the user's home folder when no path
// is given.
func (a *App) foldersJSON(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = home
		}
	}
	path = filepath.Clean(path)
	out := map[string]any{"path": path, "folders": []folderEntry{}}
	if parent := filepath.Dir(path); parent != path {
		out["parent"] = parent
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, out)
		return
	}
	folders := make([]folderEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		folders = append(folders, folderEntry{Name: e.Name(), Path: filepath.Join(path, e.Name())})
		if len(folders) >= 500 {
			break
		}
	}
	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name)
	})
	out["folders"] = folders
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
