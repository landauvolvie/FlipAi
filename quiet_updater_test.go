package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppUpdateCheckIsAlwaysThirtySeconds(t *testing.T) {
	a := newTestApp(t)
	a.mu.Lock()
	a.cfg.Updates.CheckMinutes = 24 * 60
	a.cfg.Updates.Automatic = true
	a.mu.Unlock()
	if got := a.updateInterval(); got != 30*time.Second {
		t.Fatalf("update interval = %v, want 30s", got)
	}
	if a.autoUpdateEnabled() {
		t.Fatal("the retired config flag must not install an update immediately while FlipAi is running")
	}
}

func TestReleaseProgressPercentPersistsBytes(t *testing.T) {
	info := ReleaseInfo{DownloadedBytes: 45, TotalBytes: 100}
	if got := info.ProgressPercent(); got != 45 {
		t.Fatalf("progress = %d, want 45", got)
	}
	info.DownloadedBytes = 150
	if got := info.ProgressPercent(); got != 100 {
		t.Fatalf("clamped progress = %d, want 100", got)
	}
}

func TestSidebarShowsPercentageThenOffersInstall(t *testing.T) {
	a := newTestApp(t)
	waiting := ReleaseInfo{
		Version:         "99.0.0",
		AssetURL:        "https://example.invalid/FlipAi-Setup-v99.0.0.exe",
		CheckedAt:       time.Now(),
		Downloading:     true,
		DownloadedBytes: 45,
		TotalBytes:      100,
	}
	saveUpdateState(a.statePath, waiting)
	body := a.do(t, http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, `title="Downloading FlipAi 99.0.0"`) {
		t.Fatal("available update should show the quiet download indicator")
	}
	if !strings.Contains(body, `side-update-percent">45%`) {
		t.Fatal("sidebar should show live download percentage")
	}
	if strings.Contains(body, `id="flipai-update-install"`) {
		t.Fatal("install button appeared before the installer was staged")
	}
	if strings.Contains(body, `class="banner update"`) || strings.Contains(body, "Details</a>") {
		t.Fatal("page-wide updater banner must be removed")
	}

	installer := filepath.Join(t.TempDir(), "FlipAi-Setup-v99.0.0.exe")
	if err := os.WriteFile(installer, []byte("verified-test-installer"), 0600); err != nil {
		t.Fatal(err)
	}
	waiting.Downloading = false
	waiting.DownloadedPath = installer
	waiting.DownloadedBytes = int64(len("verified-test-installer"))
	waiting.TotalBytes = waiting.DownloadedBytes
	waiting.DownloadedAt = time.Now()
	saveUpdateState(a.statePath, waiting)
	body = a.do(t, http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, `id="flipai-update-install"`) || !strings.Contains(body, "side-update-ready") || !strings.Contains(body, ">Install update</button>") {
		t.Fatal("staged update should become the compact Install update button")
	}
	if strings.Contains(body, `action="/update/install"`) {
		t.Fatal("install control should use the quiet fetch path instead of navigating to a result page")
	}
}

func TestUpdateStatusEndpointReportsProgress(t *testing.T) {
	a := newTestApp(t)
	saveUpdateState(a.statePath, ReleaseInfo{
		Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe",
		Downloading: true, DownloadedBytes: 15, TotalBytes: 100,
	})
	rr := a.do(t, http.MethodGet, "/update/status.json", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"percent":15`) || !strings.Contains(rr.Body.String(), `"available":true`) {
		t.Fatalf("unexpected update status: code=%d body=%s", rr.Code, rr.Body.String())
	}
}
