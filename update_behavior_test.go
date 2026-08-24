package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The background check has to be configurable, and a hand-edited config must
// not be able to make FlipAi poll GitHub in a tight loop.
func TestUpdateCheckIntervalIsConfiguredAndClamped(t *testing.T) {
	if got := (UpdateConfig{CheckHours: 24}).checkInterval(); got != 24*time.Hour {
		t.Errorf("a configured interval must be used, got %v", got)
	}
	for _, bad := range []int{0, -5, 100000} {
		got := (UpdateConfig{CheckHours: bad}).checkInterval()
		if got != updateCheckHoursDefault*time.Hour {
			t.Errorf("CheckHours %d must fall back to the default, got %v", bad, got)
		}
	}
}

// A config written before automatic updates existed has no updates block, and
// decoding leaves the booleans false. It must take the defaults rather than
// silently landing on "never check, never install".
func TestOlderConfigGetsUpdateDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.json")
	if err := os.WriteFile(path, []byte(`{"listen":"127.0.0.1:8765"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Updates.Automatic {
		t.Error("an upgraded install should get automatic updates, not silence")
	}
	if cfg.Updates.CheckHours != updateCheckHoursDefault {
		t.Errorf("check interval = %d, want %d", cfg.Updates.CheckHours, updateCheckHoursDefault)
	}

	// An explicit block must be respected, including turning automation off.
	if err := os.WriteFile(path, []byte(`{"updates":{"automatic":false,"checkHours":24}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Automatic {
		t.Error("an explicit automatic:false must be honoured")
	}
	if cfg.Updates.CheckHours != 24 {
		t.Errorf("an explicit interval must be honoured, got %d", cfg.Updates.CheckHours)
	}
}

// The Settings form must round-trip both controls.
func TestUpdateSettingsSaveRoundTrip(t *testing.T) {
	a := newTestApp(t)
	rr := a.do(t, http.MethodPost, "/settings/updates", url.Values{
		"autoUpdate":       {"0"},
		"updateCheckHours": {"24"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("save returned %d", rr.Code)
	}
	cfg, err := loadConfig(a.configPath, a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Automatic {
		t.Error("turning automatic updates off did not stick")
	}
	if cfg.Updates.CheckHours != 24 {
		t.Errorf("interval = %d, want 24", cfg.Updates.CheckHours)
	}

	// And an out-of-range interval is rejected rather than stored.
	if rr := a.do(t, http.MethodPost, "/settings/updates", url.Values{"updateCheckHours": {"999999"}}); rr.Code != 400 {
		t.Errorf("an out-of-range interval should be refused, got %d", rr.Code)
	}
}

// A new release must be visible from any page without opening Settings.
func TestSidebarShowsAnAvailableUpdateNextToTheVersion(t *testing.T) {
	a := newTestApp(t)
	saveUpdateState(a.statePath, ReleaseInfo{
		Version:   "99.0.0",
		AssetURL:  "https://example.invalid/FlipAi-Setup-v99.0.0.exe",
		CheckedAt: time.Now(),
	})
	body := a.do(t, http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, "side-update") {
		t.Error("the sidebar version line should become an update indicator")
	}
	if !strings.Contains(body, "99.0.0") {
		t.Error("the sidebar should name the available version")
	}

	// With no newer release the sidebar stays a plain version line.
	saveUpdateState(a.statePath, ReleaseInfo{Version: version, CheckedAt: time.Now()})
	plain := a.do(t, http.MethodGet, "/", nil).Body.String()
	if strings.Contains(plain, "side-update") {
		t.Error("no update available should leave the plain version line")
	}
}

// Automatic installs must never kill a turn that is already running.
func TestAutomaticUpdateWaitsForAnAgentTurn(t *testing.T) {
	a := newTestApp(t)
	if a.bridgeBusy() {
		t.Fatal("a bridge-less app cannot be busy")
	}
	dir := t.TempDir()
	cfg := defaultConfig(dir)
	b := NewBridge(cfg, filepath.Join(dir, "state.json"), State{}, nil, nil, nil)
	a.mu.Lock()
	a.bridge = b
	a.mu.Unlock()

	if a.bridgeBusy() {
		t.Error("an idle bridge must not report busy")
	}
	b.mu.Lock()
	b.busy = true
	b.mu.Unlock()
	if !a.bridgeBusy() {
		t.Error("a running turn must block an automatic restart")
	}
}
