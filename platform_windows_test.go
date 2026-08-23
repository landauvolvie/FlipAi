//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutostartUsesCurrentUserRunKey(t *testing.T) {
	name := "AISMSBridgeTest_" + strings.ReplaceAll(t.Name(), "/", "_")
	exe := filepath.Join(t.TempDir(), "AISMSBridge.exe")
	if err := os.WriteFile(exe, []byte("test"), 0600); err != nil { t.Fatal(err) }
	defer uninstallAutostartNamed(name)
	if err := installAutostartNamed(name, exe); err != nil { t.Fatal(err) }
	cmd := exec.Command("reg.exe", "QUERY", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", name)
	out, err := cmd.CombinedOutput()
	if err != nil { t.Fatalf("query startup value: %v: %s", err, out) }
	want := `"` + exe + `" --watchdog`
	if !strings.Contains(string(out), want) { t.Fatalf("startup value does not launch watchdog; want %q in %q", want, string(out)) }
}

func TestCopySelfInstallUsesLocalAppDataWithoutAdminLocation(t *testing.T) {
	root := t.TempDir(); t.Setenv("LOCALAPPDATA", root)
	dst, err := copySelfInstall()
	if err != nil { t.Fatal(err) }
	want := filepath.Join(root, "Programs", "AISMSBridge", "AISMSBridge.exe")
	if !strings.EqualFold(filepath.Clean(dst), filepath.Clean(want)) { t.Fatalf("installed path = %q, want %q", dst, want) }
	if _, err := os.Stat(dst); err != nil { t.Fatalf("installed EXE missing: %v", err) }
}

func TestResolveCodexExecutablePrefersDesktopUserRuntime(t *testing.T) {
	root := t.TempDir(); t.Setenv("LOCALAPPDATA", root)
	older := filepath.Join(root, "OpenAI", "Codex", "bin", "oldhash", "codex.exe")
	newer := filepath.Join(root, "OpenAI", "Codex", "bin", "newhash", "codex.exe")
	for _, p := range []string{older, newer} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil { t.Fatal(err) }
		if err := os.WriteFile(p, []byte("fake"), 0755); err != nil { t.Fatal(err) }
	}
	oldTime := time.Now().Add(-time.Hour); _ = os.Chtimes(older, oldTime, oldTime)
	if got := resolveCodexExecutable("codex"); !strings.EqualFold(filepath.Clean(got), filepath.Clean(newer)) {
		t.Fatalf("resolved Codex = %q, want newest Desktop runtime %q", got, newer)
	}
}

func TestResolveClaudeExecutableUsesNativeUserInstall(t *testing.T) {
	home := t.TempDir(); t.Setenv("USERPROFILE", home); t.Setenv("HOME", home)
	claude := filepath.Join(home, ".local", "bin", "claude.exe")
	if err := os.MkdirAll(filepath.Dir(claude), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(claude, []byte("fake"), 0755); err != nil { t.Fatal(err) }
	if got := resolveClaudeExecutable("claude"); !strings.EqualFold(filepath.Clean(got), filepath.Clean(claude)) {
		t.Fatalf("resolved Claude = %q, want native user install %q", got, claude)
	}
}

func TestExplicitAgentPathsAreRespected(t *testing.T) {
	customCodex := filepath.Join(t.TempDir(), "my-codex.exe")
	if got := resolveCodexExecutable(customCodex); got != customCodex { t.Fatalf("explicit Codex path changed: got %q want %q", got, customCodex) }
	customClaude := filepath.Join(t.TempDir(), "my-claude.exe")
	if got := resolveClaudeExecutable(customClaude); got != customClaude { t.Fatalf("explicit Claude path changed: got %q want %q", got, customClaude) }
}

func TestWatchdogSingleInstanceMutex(t *testing.T) {
	release1, owner1, err := acquireWatchdogInstance()
	if err != nil { t.Fatal(err) }
	if !owner1 { t.Fatal("first watchdog should own the mutex") }
	defer release1()
	release2, owner2, err := acquireWatchdogInstance()
	if err != nil { t.Fatal(err) }
	defer release2()
	if owner2 { t.Fatal("second watchdog must not own the same mutex") }
}
