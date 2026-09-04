//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestClaudeInstallSignInUsesWinGetAndLoginWithoutRemoteScriptExecution(t *testing.T) {
	got := claudeInstallSignInArgs()
	for _, want := range []string{
		"winget install",
		claudeWingetPackageID,
		"--exact",
		"auth login",
		"/K",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Claude first-run command is missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"install.ps1", "Invoke-Expression", "| iex", "irm http"} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(forbidden)) {
			t.Errorf("Claude first-run command must not download-and-execute script text; found %q in %s", forbidden, got)
		}
	}
}
