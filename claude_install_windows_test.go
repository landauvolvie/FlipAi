//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestClaudeInstallSignInUsesOfficialWindowsInstallerAndLogin(t *testing.T) {
	got := claudeInstallSignInArgs()
	for _, want := range []string{
		"https://claude.ai/install.ps1",
		"auth login",
		`.local\bin\claude.exe`,
		"-NoExit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Claude first-run PowerShell is missing %q: %s", want, got)
		}
	}
}
