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

func stagedUpdateFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	installer := filepath.Join(dir, "setup.exe")
	if err := os.WriteFile(installer, []byte("verified installer"), 0600); err != nil {
		t.Fatal(err)
	}
	sum, err := sha256File(installer)
	if err != nil {
		t.Fatal(err)
	}
	saveUpdateState(state, ReleaseInfo{Version: "99.0.0", AssetURL: "https://offline.invalid/setup.exe", DownloadedPath: installer, DownloadedSHA256: sum})
	return state
}

func TestStagedUpdateInstallsOfflineOnceAndPreservesLaunchMode(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		state := stagedUpdateFixture(t)
		calls := 0
		launch := func(path string, window bool) error {
			calls++
			if window != reopen {
				t.Fatalf("reopen = %v, want %v", window, reopen)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
			return nil
		}
		for i := 0; i < 2; i++ {
			started, err := installStagedUpdate(state, reopen, launch)
			if !started || err != nil {
				t.Fatalf("install: started=%v err=%v", started, err)
			}
		}
		if calls != 1 {
			t.Fatalf("launched %d installers", calls)
		}
	}
}

func TestStagedUpdateFailedLaunchCanRetry(t *testing.T) {
	state := stagedUpdateFixture(t)
	if started, err := installStagedUpdate(state, true, func(string, bool) error { return os.ErrPermission }); started || err == nil {
		t.Fatalf("failed launch: started=%v err=%v", started, err)
	}
	if _, err := os.Stat(updateInstallingFlag(state)); !os.IsNotExist(err) {
		t.Fatal("failed launch left the install lock")
	}
	started, err := installStagedUpdate(state, true, func(string, bool) error { return nil })
	if !started || err != nil {
		t.Fatalf("retry: started=%v err=%v", started, err)
	}
}

func TestStagedUpdateRejectsTamperingAndMissingChecksum(t *testing.T) {
	for _, missing := range []bool{false, true} {
		state := stagedUpdateFixture(t)
		info := loadUpdateState(state)
		if missing {
			info.DownloadedSHA256 = ""
			saveUpdateState(state, info)
		} else {
			if err := os.WriteFile(info.DownloadedPath, []byte("modified"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		started, err := installStagedUpdate(state, true, func(string, bool) error { t.Fatal("launched unverified installer"); return nil })
		if started || err != nil {
			t.Fatalf("rejected file: started=%v err=%v", started, err)
		}
		if loadUpdateState(state).Ready() {
			t.Fatal("rejected file still marked ready")
		}
	}
}
