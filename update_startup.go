package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

func updateInstallingFlag(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "update-installing.flag")
}

func clearBrokenStagedUpdate(statePath string, info ReleaseInfo, detail string) {
	if info.DownloadedPath != "" {
		_ = os.Remove(info.DownloadedPath)
	}
	info.Downloading = false
	info.DownloadedPath = ""
	info.DownloadedSHA256 = ""
	info.DownloadedAt = time.Time{}
	info.DownloadedBytes = 0
	info.TotalBytes = 0
	info.Error = truncate(detail, 200)
	saveUpdateState(statePath, info)
}

// maybeInstallStagedUpdateAtStartup is called only at a real app/watchdog
// startup. Merely reaching 100% while FlipAi is running does not interrupt the
// user: the sidebar becomes an Install update button. If the app or computer is
// restarted first, this consumes the already-verified installer automatically.
func maybeInstallStagedUpdateAtStartup(statePath string) bool {
	info := loadUpdateState(statePath)
	flag := updateInstallingFlag(statePath)
	if !info.Newer() {
		_ = os.Remove(flag)
		return false
	}
	if !info.Ready() {
		return false
	}
	if strings.TrimSpace(info.DownloadedSHA256) == "" {
		clearBrokenStagedUpdate(statePath, info, "staged update checksum is missing; downloading it again")
		return false
	}
	sum, err := sha256File(info.DownloadedPath)
	if err != nil || !strings.EqualFold(sum, info.DownloadedSHA256) {
		clearBrokenStagedUpdate(statePath, info, "staged update no longer matches its verified checksum; downloading it again")
		return false
	}

	// A launcher and the Windows sign-in watchdog can overlap. This tiny flag
	// prevents both from starting the same installer. A stale flag is recoverable.
	tryFlag := func() (*os.File, error) {
		return os.OpenFile(flag, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	}
	lock, lockErr := tryFlag()
	if os.IsExist(lockErr) {
		if st, statErr := os.Stat(flag); statErr == nil && time.Since(st.ModTime()) > 10*time.Minute {
			_ = os.Remove(flag)
			lock, lockErr = tryFlag()
		} else {
			// Another startup already handed off to Setup. Do not start the old
			// build underneath it.
			return true
		}
	}
	if lockErr != nil {
		return false
	}
	_, _ = lock.WriteString(info.Version + "\n")
	_ = lock.Close()

	if err := runUpdateInstaller(info.DownloadedPath, true); err != nil {
		_ = os.Remove(flag)
		info.Error = truncate("could not start staged update: "+err.Error(), 200)
		saveUpdateState(statePath, info)
		return false
	}
	activityLogForStatePath(statePath).Add("info", "host", "Installing staged FlipAi "+info.Version+" after restart", "", "", "")
	return true
}
