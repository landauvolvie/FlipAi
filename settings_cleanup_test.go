package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsPageKeepsOnlyAppLevelControls(t *testing.T) {
	a := newTestApp(t)
	body := a.do(t, http.MethodGet, "/settings", nil).Body.String()

	for _, want := range []string{
		"Check for new version",
		"Start FlipAi with Windows",
		"Start before sign-in",
		"Call status & diagnostics",
		"Desktop voice apps",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("clean Settings page is missing %q", want)
		}
	}

	for _, retired := range []string{
		`name="autoUpdate"`,
		`name="updateCheckMinutes"`,
		`name="theme"`,
		`name="compact"`,
		`name="alerts"`,
		`name="alertSound"`,
		`name="closeToTray"`,
		`action="/settings/reset"`,
		`action="/restart"`,
		`action="/quit"`,
		`name="newSessionCommand"`,
		`name="turnTimeout"`,
		`name="replyMaxChars"`,
		`name="cwd"`,
		`<div class="tiles">`,
	} {
		if strings.Contains(body, retired) {
			t.Errorf("clean Settings page still exposes retired control %s", retired)
		}
	}
}

func TestSimplifiedDefaultsDisableAutomaticInstallAndMigrateTenMinutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.json")
	if err := os.WriteFile(path, []byte(`{"updates":{"automatic":true,"checkMinutes":10}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.Automatic {
		t.Fatal("legacy automatic install remained enabled")
	}
	if cfg.Updates.CheckMinutes != 50 {
		t.Fatalf("legacy 10-minute cadence migrated to %d, want 50", cfg.Updates.CheckMinutes)
	}
}
