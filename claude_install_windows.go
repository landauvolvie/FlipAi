//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// A clean FlipAi machine may not have Claude Code yet. Previous builds used
// Anthropic's documented PowerShell one-liner, which downloads a script and
// immediately pipes it into Invoke-Expression. That is convenient at a prompt,
// but it is also exactly the download-and-execute pattern endpoint protection
// is designed to distrust when launched by another application.
//
// Anthropic also publishes Claude Code through Microsoft's WinGet catalog. Use
// that package-manager path instead: Windows/WinGet owns the download and
// package verification, FlipAi never evaluates network-delivered script text,
// and the user still gets one visible setup/sign-in console.
const claudeWingetPackageID = "Anthropic.ClaudeCode"

func claudeInstallSignInArgs() string {
	return `/D /K "winget install --id ` + claudeWingetPackageID + ` --exact --source winget --accept-source-agreements --accept-package-agreements && claude auth login && echo. && echo Claude sign-in finished. FlipAi will detect it automatically. You may close this window."`
}

func startClaudeInstallAndSignIn(dir string) error {
	if capture := os.Getenv("FLIPAI_BROWSER_TEST_CAPTURE"); capture != "" {
		return os.WriteFile(capture, []byte(claudeInstallSignInArgs()), 0600)
	}
	if _, err := exec.LookPath("winget.exe"); err != nil {
		return fmt.Errorf("Windows Package Manager (winget) is required to install Claude Code securely; install App Installer from Microsoft, then try Connect Claude again")
	}

	// Keep the terminal visible because Claude's authentication is interactive.
	// cmd.exe is used only as a sequencer for two fixed commands; no user input
	// or remotely supplied text is interpolated into the command line.
	shell := strings.TrimSpace(os.Getenv("COMSPEC"))
	if shell == "" {
		shell = "cmd.exe"
	}
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(shell)
	if err != nil {
		return err
	}
	params, err := syscall.UTF16PtrFromString(claudeInstallSignInArgs())
	if err != nil {
		return err
	}
	var workdir *uint16
	if strings.TrimSpace(dir) != "" && existingDir(dir) {
		if workdir, err = syscall.UTF16PtrFromString(dir); err != nil {
			return err
		}
	}
	r, _, callErr := procPlatformShellOpen.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(workdir)),
		1, // SW_SHOWNORMAL
	)
	if r <= 32 {
		return fmt.Errorf("open Claude installer/sign-in console failed (ShellExecuteW=%d): %v", r, callErr)
	}
	return nil
}
