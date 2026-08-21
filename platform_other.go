//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func openBrowser(u string) error {
	if runtime.GOOS == "darwin" { return exec.Command("open", u).Start() }
	return exec.Command("xdg-open", u).Start()
}
func hideWindow(cmd *exec.Cmd)                       {}
func spawnDetached(exe string, args ...string) error { return exec.Command(exe, args...).Start() }
func installAutostart(exe string) error              { return nil }
func uninstallAutostart() error                      { return nil }
func copySelfInstall() (string, error)               { return os.Executable() }
func resolveCodexExecutable(configured string) string { return resolvePathExecutable(configured, "codex") }
func resolveClaudeExecutable(configured string) string { return resolvePathExecutable(configured, "claude") }
func augmentCodexEnv(exe string, env []string) []string { return env }
func resolvePathExecutable(configured, fallback string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" { configured = fallback }
	if p, err := exec.LookPath(configured); err == nil { return p }
	return configured
}
