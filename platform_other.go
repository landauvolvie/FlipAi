//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func openBrowser(u string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", u).Start()
	}
	return exec.Command("xdg-open", u).Start()
}
func hideWindow(cmd *exec.Cmd)                       {}
func spawnDetached(exe string, args ...string) error { return exec.Command(exe, args...).Start() }
func installAutostart(exe string) error              { return nil }
func uninstallAutostart() error                      { return nil }

// autostartEnabled and openFolder exist on every platform so the desktop UI
// compiles and tests everywhere. Only the Windows implementations do real work;
// sign-in startup is a Windows feature.
func autostartEnabled() bool { return false }

// The published installer is a Windows Setup EXE; there is nothing to run
// elsewhere.
func runUpdateInstaller(path string, reopenWindow bool) error {
	return errors.New("the FlipAi installer only runs on Windows")
}
func openFolder(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

// startClaudeSignIn opens the interactive Claude Code sign-in in a terminal.
// Only the Windows implementation is the supported one; this keeps the connect
// flow buildable and testable everywhere, and is genuinely useful when FlipAi
// is run from a developer machine.
func startClaudeSignIn(exe, dir string) error {
	cmd := exec.Command(exe, "/login")
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
}

func copySelfInstall() (string, error) { return os.Executable() }
func resolveCodexExecutable(configured string) string {
	return resolvePathExecutable(configured, "codex")
}
func resolveClaudeExecutable(configured string) string {
	return resolvePathExecutable(configured, "claude")
}
func augmentCodexEnv(exe string, env []string) []string { return env }
func resolvePathExecutable(configured, fallback string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = fallback
	}
	if p, err := exec.LookPath(configured); err == nil {
		return p
	}
	return configured
}
