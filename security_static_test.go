package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceAvoidsMalwareStyleWindowsTechniques(t *testing.T) {
	// Keep these assembled so this test does not match its own source. Scan
	// production Go only: tests intentionally name forbidden primitives when
	// asserting that they stay absent.
	forbidden := []string{
		"Virtual" + "AllocEx",
		"Write" + "ProcessMemory",
		"Create" + "RemoteThread",
		"NtCreate" + "ThreadEx",
		"SetWindows" + "HookEx",
		"MiniDump" + "WriteDump",
		"browser" + "_password",
		"Login" + " Data",
		"Invoke-" + "Expression",
		"Encoded" + "Command",
		"DisableRealtime" + "Monitoring",
		"Add-Mp" + "Preference",
		"Set-Mp" + "Preference",
		"test" + "signing",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
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

func TestSecurityHardeningDoesNotRegressToShellPersistenceOrRemoteScriptInstall(t *testing.T) {
	platform, err := os.ReadFile("platform_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	platformText := strings.ToLower(string(platform))
	if strings.Contains(platformText, strings.ToLower("reg"+".exe")) {
		t.Fatal("per-user startup must use the Windows registry API, not a hidden reg.exe child process")
	}
	if !strings.Contains(platformText, "windows/registry") {
		t.Fatal("platform startup code is expected to use golang.org/x/sys/windows/registry")
	}

	claude, err := os.ReadFile("claude_install_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	claudeText := strings.ToLower(string(claude))
	for _, needle := range []string{"install" + ".ps1", "| " + "iex", "invoke-" + "expression"} {
		if strings.Contains(claudeText, strings.ToLower(needle)) {
			t.Fatalf("Claude setup regressed to download-and-execute script behavior: %q", needle)
		}
	}
	if !strings.Contains(claudeText, strings.ToLower("Anthropic."+"ClaudeCode")) || !strings.Contains(claudeText, "winget") {
		t.Fatal("Claude setup must go through Anthropic's WinGet package")
	}

	boot, err := os.ReadFile("boot_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(boot), "<RunLevel>LeastPrivilege</RunLevel>") {
		t.Fatal("the optional boot watchdog must run with the normal user token")
	}
	if strings.Contains(string(boot), "<RunLevel>HighestAvailable</RunLevel>") {
		t.Fatal("the long-running boot watchdog must not request elevated privileges")
	}
}
