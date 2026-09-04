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
	// Dependency Review is intentionally omitted: this repository does not have
	// GitHub's Dependency Graph feature enabled. govulncheck, CodeQL, Dependabot,
	// the standalone SBOM check, and release-time SBOM publication remain the
	// repository-supported gates.
	for _, path := range []string{
		".github/workflows/security.yml",
		".github/workflows/sbom.yml",
		".github/workflows/release.yml",
		".github/dependabot.yml",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("required security automation %s is missing: %v", path, err)
		}
	}

	security, err := os.ReadFile(".github/workflows/security.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"codeql-action", "govulncheck"} {
		if !strings.Contains(string(security), want) {
			t.Fatalf("security workflow lost %q", want)
		}
	}

	release, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	releaseText := string(release)
	for _, want := range []string{
		"Generate CycloneDX SBOM",
		"FlipAi-SBOM.cdx.json",
		"Attest release build provenance",
		"SBOM is missing; refusing to publish release without it",
	} {
		if !strings.Contains(releaseText, want) {
			t.Fatalf("release workflow lost inline SBOM control %q", want)
		}
	}
}
