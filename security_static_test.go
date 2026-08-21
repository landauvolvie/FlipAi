package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceAvoidsMalwareStyleWindowsTechniques(t *testing.T) {
	// Keep these assembled so this test does not match its own source.
	forbidden := []string{
		"Virtual" + "AllocEx",
		"Write" + "ProcessMemory",
		"Create" + "RemoteThread",
		"NtCreate" + "ThreadEx",
		"SetWindows" + "HookEx",
		"MiniDump" + "WriteDump",
		"browser" + "_password",
		"Login" + " Data",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Base(f) == "security_static_test.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(b))
		for _, needle := range forbidden {
			if strings.Contains(text, strings.ToLower(needle)) {
				t.Fatalf("%s contains forbidden malware-style technique %q", f, needle)
			}
		}
	}
}
