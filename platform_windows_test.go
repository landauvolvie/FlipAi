//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestAutostartUsesCurrentUserRunKey(t *testing.T) {
	name := "FlipAiTest_" + strings.ReplaceAll(t.Name(), "/", "_")
	exe := filepath.Join(t.TempDir(), "FlipAi.exe")
	if err := os.WriteFile(exe, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	defer uninstallAutostartNamed(name)
	if err := installAutostartNamed(name, exe); err != nil {
		t.Fatal(err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, flipAiRunKey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open startup Run key: %v", err)
	}
	defer key.Close()
	got, _, err := key.GetStringValue(name)
	if err != nil {
		t.Fatalf("read startup value: %v", err)
	}
	want := `"` + exe + `" --watchdog`
	if got != want {
		t.Fatalf("startup value = %q, want %q", got, want)
	}
}

func TestCopySelfInstallUsesFlipAiPerUserLocation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	dst, err := copySelfInstall()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "Programs", "FlipAi", "FlipAi.exe")
	if !strings.EqualFold(filepath.Clean(dst), filepath.Clean(want)) {
		t.Fatalf("installed path = %q, want %q", dst, want)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("installed EXE missing: %v", err)
	}
}

func TestOpenBrowserPreservesSettingsURL(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "opened.txt")
	t.Setenv("FLIPAI_BROWSER_TEST_CAPTURE", capture)
	want := "http://127.0.0.1:8765/?token=abc123"
	if err := openBrowser(want); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("browser target = %q, want %q", string(got), want)
	}
}

func TestResolveCodexExecutablePrefersDesktopUserRuntime(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	older := filepath.Join(root, "OpenAI", "Codex", "bin", "oldhash", "codex.exe")
	newer := filepath.Join(root, "OpenAI", "Codex", "bin", "newhash", "codex.exe")
	for _, p := range []string{older, newer} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("fake"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-time.Hour)
	_ = os.Chtimes(older, oldTime, oldTime)
	if got := resolveCodexExecutable("codex"); !strings.EqualFold(filepath.Clean(got), filepath.Clean(newer)) {
		t.Fatalf("resolved Codex = %q, want newest Desktop runtime %q", got, newer)
	}
}

func TestResolveClaudeExecutableUsesNativeUserInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	claude := filepath.Join(home, ".local", "bin", "claude.exe")
	if err := os.MkdirAll(filepath.Dir(claude), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := resolveClaudeExecutable("claude"); !strings.EqualFold(filepath.Clean(got), filepath.Clean(claude)) {
		t.Fatalf("resolved Claude = %q, want native user install %q", got, claude)
	}
}

func TestExplicitAgentPathsAreRespected(t *testing.T) {
	customCodex := filepath.Join(t.TempDir(), "my-codex.exe")
	if got := resolveCodexExecutable(customCodex); got != customCodex {
		t.Fatalf("explicit Codex path changed: got %q want %q", got, customCodex)
	}
	customClaude := filepath.Join(t.TempDir(), "my-claude.exe")
	if got := resolveClaudeExecutable(customClaude); got != customClaude {
		t.Fatalf("explicit Claude path changed: got %q want %q", got, customClaude)
	}
}

func TestWatchdogSingleInstanceMutex(t *testing.T) {
	release1, owner1, err := acquireWatchdogInstance()
	if err != nil {
		t.Fatal(err)
	}
	if !owner1 {
		t.Fatal("first watchdog should own the mutex")
	}
	defer release1()
	release2, owner2, err := acquireWatchdogInstance()
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	if owner2 {
		t.Fatal("second watchdog must not own the same mutex")
	}
}
