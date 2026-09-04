package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// goCodeText returns identifiers and string literals from executable Go syntax
// while deliberately ignoring comments. Security documentation should be free
// to explain a forbidden technique without making the guardrail flag itself.
func goCodeText(t *testing.T, path string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var parts []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			parts = append(parts, x.Name)
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if s, err := strconv.Unquote(x.Value); err == nil {
					parts = append(parts, s)
				}
			}
		}
		return true
	})
	return strings.ToLower(strings.Join(parts, "\n"))
}

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
		text := goCodeText(t, f)
		for _, needle := range forbidden {
			if strings.Contains(text, strings.ToLower(needle)) {
				t.Fatalf("%s contains forbidden malware-style technique %q", f, needle)
			}
		}
	}
}

func TestSecurityHardeningDoesNotRegressToShellPersistenceOrRemoteScriptInstall(t *testing.T) {
	platformText := goCodeText(t, "platform_windows.go")
	if strings.Contains(platformText, strings.ToLower("reg"+".exe")) {
		t.Fatal("per-user startup must use the Windows registry API, not a hidden registry command child process")
	}
	if !strings.Contains(platformText, "golang.org/x/sys/windows/registry") {
		t.Fatal("platform startup code is expected to use golang.org/x/sys/windows/registry")
	}

	claudeText := goCodeText(t, "claude_install_windows.go")
	for _, needle := range []string{"install" + ".ps1", "| " + "iex", "invoke-" + "expression"} {
		if strings.Contains(claudeText, strings.ToLower(needle)) {
			t.Fatalf("Claude setup regressed to download-and-execute script behavior: %q", needle)
		}
	}
	if !strings.Contains(claudeText, strings.ToLower("Anthropic."+"ClaudeCode")) || !strings.Contains(claudeText, "winget") {
		t.Fatal("Claude setup must go through Anthropic's WinGet package")
	}

	bootText := goCodeText(t, "boot_windows.go")
	if !strings.Contains(bootText, strings.ToLower("<RunLevel>LeastPrivilege</RunLevel>")) {
		t.Fatal("the optional boot watchdog must run with the normal user token")
	}
	if strings.Contains(bootText, strings.ToLower("<RunLevel>HighestAvailable</RunLevel>")) {
		t.Fatal("the long-running boot watchdog must not request elevated privileges")
	}
}
