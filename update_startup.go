package main

import "log"

// startStagedUpdateInstaller is replaceable in tests so restart behavior can be
// verified without launching a Windows Setup EXE.
var startStagedUpdateInstaller = runUpdateInstaller

// installStagedUpdateOnStartup runs only for a real app launch or the watchdog
// Windows starts after sign-in. Helper/browser/host subprocesses must never
// independently start an installer.
func installStagedUpdateOnStartup(mode, statePath string) bool {
	if mode != "ui" && mode != "--watchdog" {
		return false
	}
	info := loadUpdateState(statePath)
	if !info.Ready() {
		return false
	}
	if err := startStagedUpdateInstaller(info.DownloadedPath, true); err != nil {
		log.Printf("staged FlipAi %s update could not start after restart: %v", info.Version, err)
		activityLogForStatePath(statePath).Add("error", "host", "Staged update could not start after restart: "+truncate(err.Error(), 200), "", "", "")
		return false
	}
	activityLogForStatePath(statePath).Add("info", "host", "Installing staged FlipAi "+info.Version+" after restart", "", "", "")
	return true
}
