package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateDownloadProgressReservesOneHundredForVerifiedInstaller(t *testing.T) {
	cases := []struct {
		done, total int64
		want        int
	}{{0, 100, 0}, {1, 100, 1}, {45, 100, 45}, {99, 100, 99}, {100, 100, 99}, {200, 100, 99}, {1, 0, 0}}
	for _, tc := range cases {
		if got := updateDownloadProgress(tc.done, tc.total); got != tc.want {
			t.Fatalf("progress(%d,%d)=%d, want %d", tc.done, tc.total, got, tc.want)
		}
	}
}

func TestDownloadReportsByteProgress(t *testing.T) {
	payload := strings.Repeat("FlipAi-update-progress-", 8192)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "FlipAi-Setup-v99.0.0.exe")
	var lastDone, lastTotal int64
	_, err := downloadWithProgress(context.Background(), server.URL, dest, func(done, total int64) {
		if done < lastDone {
			t.Fatalf("download progress moved backwards: %d -> %d", lastDone, done)
		}
		lastDone, lastTotal = done, total
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Fatalf("final progress=(%d,%d), want (%d,%d)", lastDone, lastTotal, len(payload), len(payload))
	}
}

func TestUpdateStatusJSONShowsLivePercentageThenReady(t *testing.T) {
	a := newTestApp(t)
	info := ReleaseInfo{
		Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe",
		Downloading: true, DownloadBytes: 45, DownloadTotal: 100, DownloadPercent: 45,
		CheckedAt: time.Now(),
	}
	saveUpdateState(a.statePath, info)
	rr := a.do(t, http.MethodGet, "/update/status.json", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status endpoint returned %d", rr.Code)
	}
	var status map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["state"] != "downloading" || status["percent"] != float64(45) {
		t.Fatalf("unexpected download status: %#v", status)
	}

	installer := filepath.Join(t.TempDir(), "FlipAi-Setup-v99.0.0.exe")
	if err := os.WriteFile(installer, []byte("verified"), 0600); err != nil {
		t.Fatal(err)
	}
	info.Downloading = false
	info.DownloadedPath = installer
	info.DownloadPercent = 100
	saveUpdateState(a.statePath, info)
	rr = a.do(t, http.MethodGet, "/update/status.json", nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["state"] != "ready" || status["percent"] != float64(100) || status["ready"] != true {
		t.Fatalf("unexpected ready status: %#v", status)
	}
}

func TestRestartInstallsStagedUpdateAndAlwaysReopensFlipAi(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	installer := filepath.Join(dir, "FlipAi-Setup-v99.0.0.exe")
	if err := os.WriteFile(installer, []byte("installer"), 0600); err != nil {
		t.Fatal(err)
	}
	saveUpdateState(statePath, ReleaseInfo{
		Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe",
		DownloadedPath: installer, DownloadPercent: 100, CheckedAt: time.Now(),
	})

	old := startStagedUpdateInstaller
	defer func() { startStagedUpdateInstaller = old }()
	var gotPath string
	var gotReopen bool
	startStagedUpdateInstaller = func(path string, reopen bool) error {
		gotPath, gotReopen = path, reopen
		return nil
	}
	if !installStagedUpdateOnStartup("ui", statePath) {
		t.Fatal("normal app restart should start the staged update")
	}
	if gotPath != installer || !gotReopen {
		t.Fatalf("installer launch=(%q,%v), want staged path and reopen=true", gotPath, gotReopen)
	}
}

func TestWindowsStartupWatchdogInstallsStagedUpdate(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	installer := filepath.Join(dir, "FlipAi-Setup-v99.0.0.exe")
	_ = os.WriteFile(installer, []byte("installer"), 0600)
	saveUpdateState(statePath, ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/a.exe", DownloadedPath: installer, DownloadPercent: 100})
	old := startStagedUpdateInstaller
	defer func() { startStagedUpdateInstaller = old }()
	called := false
	startStagedUpdateInstaller = func(string, bool) error { called = true; return nil }
	if !installStagedUpdateOnStartup("--watchdog", statePath) || !called {
		t.Fatal("Windows sign-in/watchdog restart should install the staged update")
	}
}

func TestFailedRestartInstallLeavesFlipAiAbleToStart(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	installer := filepath.Join(dir, "FlipAi-Setup-v99.0.0.exe")
	_ = os.WriteFile(installer, []byte("installer"), 0600)
	saveUpdateState(statePath, ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/a.exe", DownloadedPath: installer, DownloadPercent: 100})
	old := startStagedUpdateInstaller
	defer func() { startStagedUpdateInstaller = old }()
	startStagedUpdateInstaller = func(string, bool) error { return errors.New("blocked") }
	if installStagedUpdateOnStartup("ui", statePath) {
		t.Fatal("a failed installer launch must not prevent FlipAi from starting normally")
	}
	if installStagedUpdateOnStartup("--host", statePath) {
		t.Fatal("a helper host process must never independently start the installer")
	}
}
