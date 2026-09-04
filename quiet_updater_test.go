package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppUpdateCheckIsAlwaysFiveMinutes(t *testing.T) {
	a := newTestApp(t)
	a.mu.Lock()
	a.cfg.Updates.CheckMinutes = 24 * 60
	a.cfg.Updates.Automatic = true
	a.mu.Unlock()
	if got := a.updateInterval(); got != 5*time.Minute {
		t.Fatalf("update interval = %v, want 5m", got)
	}
	if a.autoUpdateEnabled() {
		t.Fatal("installation must never become automatic")
	}
}

func TestSidebarDownloadsQuietlyThenOffersInstall(t *testing.T) {
	a := newTestApp(t)
	waiting := ReleaseInfo{
		Version:     "99.0.0",
		AssetURL:    "https://example.invalid/FlipAi-Setup-v99.0.0.exe",
		CheckedAt:   time.Now(),
		Downloading: true,
	}
	saveUpdateState(a.statePath, waiting)
	body := a.do(t, http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, "side-update-downloading") {
		t.Fatal("available update should show the quiet download indicator")
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
	waiting.DownloadedAt = time.Now()
	saveUpdateState(a.statePath, waiting)
	body = a.do(t, http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, `id="flipai-update-install"`) || !strings.Contains(body, "side-update-ready") {
		t.Fatal("staged update should become the compact install button")
	}
	if strings.Contains(body, `action="/update/install"`) {
		t.Fatal("install control should use the quiet fetch path instead of navigating to a result page")
	}
}
