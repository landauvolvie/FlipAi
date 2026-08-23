//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

func openBrowser(u string) error { return exec.Command("explorer.exe", u).Start() }
func hideWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} }
func spawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	hideWindow(cmd)
	return cmd.Start()
}
func installAutostart(exe string) error { return installAutostartNamed("AISMSBridge", exe) }
func installAutostartNamed(name, exe string) error {
	value := fmt.Sprintf("\"%s\" --watchdog", exe)
	cmd := exec.Command("reg.exe", "ADD", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", name, "/t", "REG_SZ", "/d", value, "/f")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil { return fmt.Errorf("enable startup: %v: %s", err, strings.TrimSpace(string(out))) }
	return nil
}
func uninstallAutostart() error { return uninstallAutostartNamed("AISMSBridge") }
func uninstallAutostartNamed(name string) error {
	cmd := exec.Command("reg.exe", "DELETE", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", name, "/f")
	hideWindow(cmd); _ = cmd.Run(); return nil
}
func installedExePath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" { return "", fmt.Errorf("LOCALAPPDATA not set") }
	dir := filepath.Join(base, "Programs", "AISMSBridge")
	if err := os.MkdirAll(dir, 0755); err != nil { return "", err }
	return filepath.Join(dir, "AISMSBridge.exe"), nil
}
func copySelfInstall() (string, error) {
	src, err := os.Executable(); if err != nil { return "", err }
	dst, err := installedExePath(); if err != nil { return "", err }
	if strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) { return dst, nil }
	b, err := os.ReadFile(src); if err != nil { return "", err }
	if err := os.WriteFile(dst, b, 0755); err != nil { return "", err }
	return dst, nil
}

func regularExecutable(path string) bool { st, err := os.Stat(path); return err == nil && !st.IsDir() }
func existingDir(path string) bool { st, err := os.Stat(path); return err == nil && st.IsDir() }

func resolveCodexExecutable(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" && !strings.EqualFold(configured, "codex") && !strings.EqualFold(configured, "codex.exe") { return configured }
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		root := filepath.Join(base, "OpenAI", "Codex", "bin")
		direct := filepath.Join(root, "codex.exe")
		if regularExecutable(direct) { return direct }
		matches, _ := filepath.Glob(filepath.Join(root, "*", "codex.exe"))
		sort.Slice(matches, func(i, j int) bool {
			si, ei := os.Stat(matches[i]); sj, ej := os.Stat(matches[j])
			if ei == nil && ej == nil && !si.ModTime().Equal(sj.ModTime()) { return si.ModTime().After(sj.ModTime()) }
			return strings.ToLower(matches[i]) > strings.ToLower(matches[j])
		})
		for _, p := range matches { if regularExecutable(p) { return p } }
	}
	if p, err := exec.LookPath("codex"); err == nil { return p }
	if configured != "" { return configured }
	return "codex"
}

// augmentCodexEnv makes the helper executables that belong to the selected
// Codex runtime discoverable. This is important on Windows where codex.exe can
// be staged in a per-user cache while sandbox/command-runner helpers live in a
// sibling runtime directory.
func augmentCodexEnv(exe string, env []string) []string {
	var dirs []string
	add := func(p string) {
		if p == "" || !existingDir(p) { return }
		for _, d := range dirs { if strings.EqualFold(d, p) { return } }
		dirs = append(dirs, p)
	}
	exeDir := filepath.Dir(exe)
	add(exeDir)
	// Standalone layout: <release>\bin\codex.exe + <release>\codex-resources\...
	add(filepath.Join(filepath.Dir(exeDir), "codex-resources"))
	// Desktop layout: %LOCALAPPDATA%\OpenAI\Codex\bin may contain helpers.
	if base := os.Getenv("LOCALAPPDATA"); base != "" { add(filepath.Join(base, "OpenAI", "Codex", "bin")) }
	if len(dirs) == 0 { return env }
	prefix := strings.Join(dirs, string(os.PathListSeparator))
	out := append([]string(nil), env...)
	for i, e := range out {
		if eq := strings.IndexByte(e, '='); eq > 0 && strings.EqualFold(e[:eq], "PATH") {
			out[i] = e[:eq+1] + prefix + string(os.PathListSeparator) + e[eq+1:]
			return out
		}
	}
	return append(out, "PATH="+prefix)
}

func resolveClaudeExecutable(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" && !strings.EqualFold(configured, "claude") && !strings.EqualFold(configured, "claude.exe") { return configured }
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"claude.exe", "claude.cmd"} {
			p := filepath.Join(home, ".local", "bin", name)
			if regularExecutable(p) { return p }
		}
	}
	if p, err := exec.LookPath("claude"); err == nil { return p }
	if configured != "" { return configured }
	return "claude"
}
