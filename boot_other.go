//go:build !windows

package main

import "errors"

// Starting before sign-in is a Windows Task Scheduler feature. The stubs keep
// the settings page and its tests building everywhere else.

func bootStartupEnabled() bool { return false }

func enableBootStartup(dataDir string) error {
	return errors.New("starting before sign-in is a Windows feature")
}

func disableBootStartup(dataDir string) error {
	return errors.New("starting before sign-in is a Windows feature")
}

func runBootTaskCommand(dataDir string, args []string) int { return 1 }
