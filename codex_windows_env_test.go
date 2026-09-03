//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAugmentCodexEnvExposesDesktopHelpers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	bin := filepath.Join(root, "OpenAI", "Codex", "bin")
	hashDir := filepath.Join(bin, "abc123")
	if err := os.MkdirAll(hashDir, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(hashDir, "codex.exe")
	if err := os.WriteFile(exe, []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	env := augmentCodexEnv(exe, []string{"PATH=C:\\Windows\\System32", "TEMP=C:\\Temp"})
	var pathValue string
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			pathValue = e[5:]
		}
	}
	if pathValue == "" {
		t.Fatal("PATH missing")
	}
	parts := strings.Split(pathValue, string(os.PathListSeparator))
	contains := func(want string) bool {
		for _, p := range parts {
			if strings.EqualFold(filepath.Clean(p), filepath.Clean(want)) {
				return true
			}
		}
		return false
	}
	if !contains(hashDir) {
		t.Fatalf("Codex executable directory missing from PATH: %q", pathValue)
	}
	if !contains(bin) {
		t.Fatalf("Codex Desktop helper directory missing from PATH: %q", pathValue)
	}
}
