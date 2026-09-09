package main

import (
	"net/http"
	"os"
	"time"
)

func (a *App) updateStatusJSON(w http.ResponseWriter, r *http.Request) {
	info := loadUpdateState(a.statePath)
	available := info.Newer()
	flag, err := os.Stat(updateInstallingFlag(a.statePath))
	installing := available && err == nil && time.Since(flag.ModTime()) < 10*time.Minute
	writeJSON(w, map[string]any{
		"installing":      installing,
		"currentVersion":  version,
		"available":       available,
		"targetVersion":   info.Version,
		"downloading":     available && info.Downloading,
		"ready":           available && info.Ready(),
		"percent":         info.ProgressPercent(),
		"downloadedBytes": info.DownloadedBytes,
		"totalBytes":      info.TotalBytes,
	})
}
