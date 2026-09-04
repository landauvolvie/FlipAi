package main

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsBuildPipelineKeepsAnalyzableBinariesAndDefenderGates(t *testing.T) {
	for _, path := range []string{".github/workflows/build.yml", ".github/workflows/release.yml"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, `-H=windowsgui -s -w`) || strings.Contains(text, `-s -w -H=windowsgui`) {
			t.Fatalf("%s strips Go metadata from the Windows executable", path)
		}
		for _, want := range []string{"Update-MpSignature", "Start-MpScan", "FlipAi.exe"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s lost Windows security gate %q", path, want)
			}
		}
	}
}

func TestSecurityAutomationRemainsEnabled(t *testing.T) {
	for _, path := range []string{
		".github/workflows/security.yml",
		".github/workflows/dependency-review.yml",
		".github/workflows/sbom.yml",
		".github/dependabot.yml",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("required security automation %s is missing: %v", path, err)
		}
	}
}
