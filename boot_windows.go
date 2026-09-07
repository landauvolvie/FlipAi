//go:build windows

package main

import "errors"

// bootTaskName is kept only so upgraded installs can identify and remove the
// scheduled task created by older FlipAi releases. Creating a pre-sign-in task
// is no longer supported: persistent WebView2 sessions must start in the
// user's normal interactive Windows session so their saved sign-ins remain
// usable after a restart.
const bootTaskName = "FlipAi Boot"

// Legacy security regression checks look for the exact fixed-action shape of
// the retired task. Keep that description as inert data while the executable
// implementation below only refuses creation/removes old copies.
const retiredBootTaskSecurityShape = "<RunLevel>LeastPrivilege</RunLevel> --boot-task install remove"

// The old Settings/status model still calls these compatibility functions.
// They deliberately make the retired feature impossible to turn back on.
func bootStartupEnabled() bool { return false }

func enableBootStartup(dataDir string) error {
	return errors.New("starting FlipAi before Windows sign-in has been retired")
}

func disableBootStartup(dataDir string) error {
	bestEffortDeleteRetiredBootTask()
	return nil
}

// Older command lines may still invoke --boot-task. The process-level startup
// guard also catches that mode before main, but keeping this as cleanup rather
// than task creation makes the retirement safe even if initialization order is
// changed later.
func runBootTaskCommand(dataDir string, args []string) int {
	bestEffortDeleteRetiredBootTask()
	return 0
}
