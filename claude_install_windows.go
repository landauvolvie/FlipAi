//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// claudeInstallSignInPowerShell is intentionally a visible, first-party setup
// command. A clean FlipAi machine may not have Claude Code yet; Connect should
// get that machine to the same place as one where the user installed Claude by
// hand, rather than dead-ending on a red "not found" page.
//
// Anthropic's native installer currently places claude.exe under
// %USERPROFILE%\.local\bin, which resolveClaudeExecutable already searches.
const claudeInstallSignInPowerShell = `$ErrorActionPreference='Stop'; Write-Host 'FlipAi: installing Claude Code from Anthropic...'; irm 'https://claude.ai/install.ps1' | iex; $claude=Join-Path $HOME '.local\bin\claude.exe'; if(-not (Test-Path $claude)){ $cmd=Get-Command claude -ErrorAction Stop; $claude=$cmd.Source }; Write-Host ''; Write-Host 'Claude Code is installed. Starting Claude sign-in...'; & $claude auth login; Write-Host ''; Write-Host 'Claude sign-in finished. FlipAi will detect it automatically. You may close this window.'`

func claudeInstallSignInArgs() string {
	return `-NoLogo -NoProfile -NoExit -Command "` + claudeInstallSignInPowerShell + `"`
}

func startClaudeInstallAndSignIn(dir string) error {
	if capture := os.Getenv("FLIPAI_BROWSER_TEST_CAPTURE"); capture != "" {
		return os.WriteFile(capture, []byte(claudeInstallSignInArgs()), 0600)
	}
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString("powershell.exe")
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
		return fmt.Errorf("open Claude installer/sign-in PowerShell failed (ShellExecuteW=%d): %v", r, callErr)
	}
	return nil
}
