package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrokenStagedUpdateIsClearedBeforeStartupInstall(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	installer := filepath.Join(dir, "FlipAi-Setup-v99.0.0.exe")
	if err := os.WriteFile(installer, []byte("installer"), 0600); err != nil {
		t.Fatal(err)
	}
	info := ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/x.exe", DownloadedPath: installer, DownloadedSHA256: strings.Repeat("0", 64)}
	saveUpdateState(statePath, info)
	if maybeInstallStagedUpdateAtStartup(statePath) {
		t.Fatal("a checksum-mismatched staged installer must not be launched")
	}
	got := loadUpdateState(statePath)
	if got.DownloadedPath != "" || got.DownloadedBytes != 0 || got.TotalBytes != 0 {
		t.Fatalf("broken staged state was not cleared: %+v", got)
	}
}
