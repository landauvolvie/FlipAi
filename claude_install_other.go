//go:build !windows

package main

import "errors"

func startClaudeInstallAndSignIn(dir string) error {
	return errors.New("automatic Claude Code installation is available in the Windows FlipAi app")
}
