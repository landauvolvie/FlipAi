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

// The background updater still validates a stored cadence for compatibility,
// but the retired 10-minute UI/default value migrates to the new 50-minute app
// default.
func TestUpdateCheckIntervalIsConfiguredAndClamped(t *testing.T) {
	if got := (UpdateConfig{CheckMinutes: 30}).checkInterval(); got != 30*time.Minute {
		t.Errorf("a compatible stored interval must still be understood, got %v", got)
	}
	if got := (UpdateConfig{CheckMinutes: 10}).checkInterval(); got != 50*time.Minute {
		t.Errorf("the retired 10-minute default must migrate to 50 minutes, got %v", got)
	}
	for _, bad := range []int{0, -5, 100000000} {
		got := (UpdateConfig{CheckMinutes: bad}).checkInterval()
		if got != updateCheckMinutesDefault*time.Minute {
			t.Errorf("CheckMinutes %d must fall back to the default, got %v", bad, got)
		}
	}
	if got := (UpdateConfig{CheckMinutes: 1}).checkInterval(); got != updateCheckMinutesDefault*time.Minute {
		t.Errorf("below the floor must fall back to the default, got %v", got)
	}
	if got := (UpdateConfig{CheckMinutes: updateCheckMinutesMin}).checkInterval(); got != updateCheckMinutesMin*time.Minute {
		t.Errorf("the floor itself must remain readable for old configs, got %v", got)
	}
}

func TestRetiredHoursSettingMigrates(t *testing.T) {
	if got := (UpdateConfig{CheckHours: 24}).checkInterval(); got != 24*time.Hour {
		t.Errorf("a chosen 24-hour cadence must still parse, got %v", got)
	}
	if got := (UpdateConfig{CheckHours: retiredUpdateCheckHoursDefault}).checkInterval(); got != updateCheckMinutesDefault*time.Minute {
		t.Errorf("the old default must move to the new default, got %v", got)
	}
	if got := (UpdateConfig{CheckHours: 24, CheckMinutes: 15}).checkInterval(); got != 15*time.Minute {
		t.Errorf("a non-retired minute value must still take precedence, got %v", got)
	}
}

// Update checks are automatic app behavior now; installation is never enabled
// unattended by a saved legacy flag.
func TestOlderConfigGetsSimplifiedUpdateDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.json")
	if err := os.WriteFile(path, []byte(`{"listen":"127.0.0.1:8765"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Automatic {
		t.Error("automatic installation must be off")
	}
	if cfg.Updates.CheckMinutes != 50 {
		t.Errorf("check interval = %d, want 50", cfg.Updates.CheckMinutes)
	}

	// An old config may still contain the retired switch. Loading it must not
	// silently re-enable unattended installation.
	if err := os.WriteFile(path, []byte(`{"updates":{"automatic":true,"checkHours":24}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Automatic {
		t.Error("a legacy automatic:true must be disabled")
	}
	if cfg.Updates.CheckMinutes != 24*60 {
		t.Errorf("a non-default legacy cadence should remain readable, got %d", cfg.Updates.CheckMinutes)
	}
}

// The old POST endpoint is kept so an older open FlipAi window cannot break
// during an upgrade, but loading the saved config normalizes the retired user
// controls back to the app defaults.
func TestRetiredUpdateSettingsCannotReenableAutomaticInstall(t *testing.T) {
	a := newTestApp(t)
	rr := a.do(t, http.MethodPost, "/settings/updates", url.Values{
		"autoUpdate":         {"1"},
		"updateCheckMinutes": {"10"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("save returned %d", rr.Code)
	}
	cfg, err := loadConfig(a.configPath, a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Automatic {
		t.Error("the retired endpoint re-enabled automatic installation")
	}
	if cfg.Updates.CheckMinutes != 50 {
		t.Errorf("retired 10-minute setting did not normalize to 50, got %d", cfg.Updates.CheckMinutes)
	}

	if rr := a.do(t, http.MethodPost, "/settings/updates", url.Values{"updateCheckMinutes": {"999999999"}}); rr.Code != 400 {
		t.Errorf("an out-of-range legacy interval should still be refused, got %d", rr.Code)
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

	saveUpdateState(a.statePath, ReleaseInfo{Version: version, CheckedAt: time.Now()})
	plain := a.do(t, http.MethodGet, "/", nil).Body.String()
	if strings.Contains(plain, "side-update") {
		t.Error("no update available should leave the plain version line")
	}
}

// Keep the busy-state protection even though unattended installation is off;
// the same helper protects any future maintenance path from restarting during a
// live agent turn.
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
		t.Error("a running turn must block a restart")
	}
}
