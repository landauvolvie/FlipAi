package main

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsResourceBuildStaysLeastPrivilegeAndPinned(t *testing.T) {
	raw, err := os.ReadFile("scripts/Generate-FlipAiIcon.ps1")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"github.com/tc-hib/go-winres@v0.2.3",
		"--manifest gui",
		"--file-description 'FlipAi AI bridge'",
		"--product-name 'FlipAi'",
		"--original-filename 'FlipAi.exe'",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Windows resource build lost %q", want)
		}
	}
	if strings.Contains(text, "--admin") {
		t.Fatal("the main FlipAi executable must stay asInvoker and must not embed an administrator manifest")
	}
}
