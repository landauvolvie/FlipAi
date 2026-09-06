package main

import "net/http"

func (a *App) updateStatusJSON(w http.ResponseWriter, r *http.Request) {
	info := loadUpdateState(a.statePath)
	available := info.Newer()
	writeJSON(w, map[string]any{
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
