package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateProgressPersists(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	info := ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe", Downloading: true, DownloadBytes: 45, DownloadTotal: 100}
	saveUpdateState(statePath, info)
	got := loadUpdateState(statePath)
	if got.DownloadBytes != 45 || got.DownloadTotal != 100 || got.ProgressPercent() != 45 {
		t.Fatalf("persisted progress = %d/%d (%d%%), want 45/100 (45%%)", got.DownloadBytes, got.DownloadTotal, got.ProgressPercent())
	}
}

func TestRestartInstallsVerifiedStagedUpdateAndReopensWindow(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	installer := filepath.Join(dir, "FlipAi-Setup-v99.0.0.exe")
	if err := os.WriteFile(installer, []byte("verified"), 0600); err != nil {
		t.Fatal(err)
	}
	saveUpdateState(statePath, ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe", DownloadedPath: installer, DownloadBytes: 8, DownloadTotal: 8})
	called := false
	if !installStagedUpdateOnStartupWith(statePath, func(path string, reopen bool) error {
		called = true
		if path != installer || !reopen {
			t.Fatalf("launch = (%q,%v), want staged installer and reopen=true", path, reopen)
		}
		return nil
	}) {
		t.Fatal("verified staged update was not consumed on restart")
	}
	if !called {
		t.Fatal("installer launcher was not called")
	}
}

func TestRestartDoesNotInstallIncompleteUpdate(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	saveUpdateState(statePath, ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe", Downloading: true, DownloadBytes: 45, DownloadTotal: 100})
	if installStagedUpdateOnStartupWith(statePath, func(string, bool) error {
		t.Fatal("incomplete update must not launch")
		return nil
	}) {
		t.Fatal("incomplete update reported as installed")
	}
}

func TestDownloadResumesPartFileAndReportsProgress(t *testing.T) {
	oldMax := maxUpdateBytes
	_ = oldMax
	payload := []byte(strings.Repeat("abcdef", 4096))
	var sawRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRange = r.Header.Get("Range")
		start := 0
		if strings.HasPrefix(sawRange, "bytes=") {
			fmt.Sscanf(strings.TrimPrefix(sawRange, "bytes="), "%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(payload[start:])
	}))
	defer server.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "FlipAi-Setup-v99.0.0.exe")
	part := dest + ".part"
	prefix := payload[:5000]
	if err := os.WriteFile(part, prefix, 0600); err != nil {
		t.Fatal(err)
	}
	var lastDone, lastTotal int64
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := downloadWithProgress(ctx, server.URL, dest, func(done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatal(err)
	}
	if sawRange != "bytes=5000-" {
		t.Fatalf("Range = %q, want bytes=5000-", sawRange)
	}
	if lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Fatalf("final progress = %d/%d, want %d/%d", lastDone, lastTotal, len(payload), len(payload))
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatal("resumed file bytes differ from source")
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("successful download left the .part file behind")
	}
}
