package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// newTestApp builds an App backed by a temporary data directory, configured far
// enough that pages render but with no real Gmail or agent connection.
func newTestApp(t *testing.T) *App {
	t.Helper()
	tmp := t.TempDir()
	cfg := defaultConfig(tmp)
	cfg.GoogleVoice.AllowedFrom = "8455551212"
	syncAllowedNumbers(&cfg.GoogleVoice)
	if err := setSecurityCode(&cfg, "482913"); err != nil {
		t.Fatal(err)
	}
	a := &App{
		dataDir:    tmp,
		configPath: tmp + "/bridge.json",
		statePath:  tmp + "/state.json",
		tokenPath:  tmp + "/token.dat",
		cfg:        cfg,
	}
	if err := saveConfig(a.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	return a
}

func (a *App) do(t *testing.T, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequest(method, "http://127.0.0.1:8765"+target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, "http://127.0.0.1:8765"+target, nil)
	}
	req.AddCookie(&http.Cookie{Name: "aisms_session", Value: a.cfg.LocalToken})
	rr := httptest.NewRecorder()
	a.handler().ServeHTTP(rr, req)
	return rr
}

func (a *App) reloadConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := loadConfig(a.configPath, a.dataDir)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	return cfg
}

func TestDesktopPagesRender(t *testing.T) {
	a := newTestApp(t)
	pages := map[string][]string{
		"/":            {"Bridge Google Voice SMS commands", "Recent activity", "Pause FlipAi"},
		"/connections": {"Gmail / Google Voice", "Inbound sender settings", "Test message flow"},
		"/agents":      {"Codex", "Claude", "Behavior", "Executable path"},
		"/phone":       {"Allowed numbers", "Reply behavior", "Security code"},
		"/activity":    {"All stages", "Search activity", "Privacy"},
		"/settings":    {"Startup", "Appearance", "Notifications", "Diagnostics"},
		"/advanced":    {"Executable paths", "Local service", "Advanced tools"},
	}
	for path, want := range pages {
		rr := a.do(t, http.MethodGet, path, nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		for _, phrase := range want {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s is missing %q", path, phrase)
			}
		}
		// Every page carries the same shell.
		for _, item := range []string{`href="/connections"`, `href="/agents"`, `href="/phone"`, `href="/activity"`, `href="/settings"`, `href="/advanced"`} {
			if !strings.Contains(body, item) {
				t.Errorf("%s is missing nav entry %s", path, item)
			}
		}
		if strings.Contains(body, "//fonts.googleapis.com") || strings.Contains(body, "https://cdn") {
			t.Fatalf("%s must not depend on external fonts or CDNs", path)
		}
	}
}

func TestAssetsAreServedLocally(t *testing.T) {
	a := newTestApp(t)
	for path, want := range map[string]string{
		"/assets/flipai.css": ".sidebar",
		"/assets/flipai.js":  "activity.json",
	} {
		rr := a.do(t, http.MethodGet, path, nil)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), want) {
			t.Fatalf("%s status=%d missing %q", path, rr.Code, want)
		}
	}
}

func TestPagesRequireTheLocalSession(t *testing.T) {
	a := newTestApp(t)
	for _, path := range []string{"/", "/connections", "/agents", "/phone", "/activity", "/settings", "/advanced", "/status.json", "/activity.json"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
		rr := httptest.NewRecorder()
		a.handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s without a session cookie returned %d, want 403", path, rr.Code)
		}
	}
}

func TestTokenLinkStartsASession(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/?token="+a.cfg.LocalToken, nil)
	rr := httptest.NewRecorder()
	a.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("token link returned %d, want 303", rr.Code)
	}
	cookie := rr.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, a.cfg.LocalToken) || !strings.Contains(cookie, "SameSite=Strict") {
		t.Fatalf("session cookie is missing or not SameSite=Strict: %q", cookie)
	}
}

func TestActionsRefuseGet(t *testing.T) {
	a := newTestApp(t)
	for _, path := range []string{"/bridge/pause", "/phone/numbers/add", "/settings/reset", "/quit", "/activity/clear"} {
		rr := a.do(t, http.MethodGet, path, nil)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s returned %d, want 405", path, rr.Code)
		}
	}
}

func TestPauseAndResume(t *testing.T) {
	a := newTestApp(t)
	a.bridge = NewBridge(a.cfg, a.statePath, State{}, nil, nil, nil)

	if rr := a.do(t, http.MethodPost, "/bridge/pause", nil); rr.Code != http.StatusSeeOther {
		t.Fatalf("pause returned %d: %s", rr.Code, rr.Body.String())
	}
	if !a.reloadConfig(t).Paused {
		t.Fatal("pause did not persist to the config file")
	}
	if !a.bridge.Paused() {
		t.Fatal("pause did not reach the running bridge")
	}

	if rr := a.do(t, http.MethodPost, "/bridge/resume", nil); rr.Code != http.StatusSeeOther {
		t.Fatalf("resume returned %d", rr.Code)
	}
	if a.reloadConfig(t).Paused || a.bridge.Paused() {
		t.Fatal("resume did not clear the paused state")
	}

	body := a.do(t, http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, "/bridge/pause") {
		t.Fatal("Home should offer Pause once the bridge is running again")
	}
}

// A paused bridge must not claim new mail. This is the behaviour the Home
// button promises, so it is checked at the poll loop rather than in the UI.
func TestPausedBridgeSkipsPolling(t *testing.T) {
	tmp := t.TempDir()
	cfg := defaultConfig(tmp)
	cfg.Paused = true
	mail := &countingMailClient{}
	b := NewBridge(cfg, tmp+"/state.json", State{}, mail, nil, nil)
	b.poll(context.Background())
	if mail.listCalls != 0 {
		t.Fatalf("a paused bridge checked the mailbox %d time(s)", mail.listCalls)
	}
	b.SetPaused(false)
	b.poll(context.Background())
	if mail.listCalls != 1 {
		t.Fatalf("a resumed bridge made %d mailbox checks, want 1", mail.listCalls)
	}
}

func TestAllowedNumberLifecycle(t *testing.T) {
	a := newTestApp(t)

	rr := a.do(t, http.MethodPost, "/phone/numbers/add", url.Values{"number": {"(212) 555-0147"}, "label": {"Work"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("add returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	if len(cfg.GoogleVoice.AllowedNumbers) != 2 {
		t.Fatalf("expected 2 allowed numbers, got %d", len(cfg.GoogleVoice.AllowedNumbers))
	}
	if !strings.Contains(cfg.GoogleVoice.AllowedFrom, "2125550147") {
		t.Fatalf("the routing allowlist was not updated: %q", cfg.GoogleVoice.AllowedFrom)
	}
	var added AllowedNumber
	for _, n := range cfg.GoogleVoice.AllowedNumbers {
		if n.Number == "2125550147" {
			added = n
		}
	}
	if added.Label != "Work" || added.Added.IsZero() {
		t.Fatalf("label and added date were not recorded: %#v", added)
	}

	// html/template escapes the leading "+" as an entity, so match the part
	// of the formatted number that survives escaping.
	page := a.do(t, http.MethodGet, "/phone", nil).Body.String()
	if !strings.Contains(page, "(212) 555-0147") || !strings.Contains(page, "Work") {
		t.Fatal("the Phone page does not list the new number with its label")
	}

	if rr := a.do(t, http.MethodPost, "/phone/numbers/remove", url.Values{"number": {"2125550147"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("remove returned %d: %s", rr.Code, rr.Body.String())
	}
	if got := len(a.reloadConfig(t).GoogleVoice.AllowedNumbers); got != 1 {
		t.Fatalf("after removal expected 1 number, got %d", got)
	}

	// Removing the last number would silently stop the bridge, so it is refused.
	rr = a.do(t, http.MethodPost, "/phone/numbers/remove", url.Values{"number": {"8455551212"}})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "at least one allowed number") {
		t.Fatalf("removing the last number returned %d: %s", rr.Code, rr.Body.String())
	}

	rr = a.do(t, http.MethodPost, "/phone/numbers/add", url.Values{"number": {"nonsense"}})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("a malformed number returned %d, want 400", rr.Code)
	}
}

func TestReplyBehaviourValidationAndSave(t *testing.T) {
	a := newTestApp(t)
	rr := a.do(t, http.MethodPost, "/phone/save", url.Values{"replyMaxChars": {"50000"}})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "reply length") {
		t.Fatalf("out-of-range reply length returned %d: %s", rr.Code, rr.Body.String())
	}
	if a.reloadConfig(t).GoogleVoice.ReplyMaxChars == 50000 {
		t.Fatal("an invalid value was saved anyway")
	}

	rr = a.do(t, http.MethodPost, "/phone/save", url.Values{
		"replyMaxChars":    {"420"},
		"maxReplyParts":    {"6"},
		"progressInterval": {"90"},
		"replyAck":         {"0"},
		"progressUpdates":  {"0", "1"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("save returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	if cfg.GoogleVoice.ReplyMaxChars != 420 || cfg.GoogleVoice.MaxReplyParts != 6 || cfg.GoogleVoice.ProgressIntervalSeconds != 90 {
		t.Fatalf("reply settings were not stored: %#v", cfg.GoogleVoice)
	}
	if cfg.GoogleVoice.ReplyAck {
		t.Fatal("an unchecked toggle must turn the setting off")
	}
	if !cfg.GoogleVoice.ProgressUpdates {
		t.Fatal("a checked toggle must turn the setting on")
	}
}

func TestSecurityCodeToggle(t *testing.T) {
	a := newTestApp(t)

	// Turning protection off keeps routing working and records the choice.
	rr := a.do(t, http.MethodPost, "/phone/security", url.Values{"requireCode": {"0"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("disable returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	if cfg.Security.RequireCode || cfg.Security.CodeHash == "" {
		t.Fatalf("expected code protection off with an internal hash retained: %#v", cfg.Security)
	}

	// A brand new install has no code, so turning protection on must ask for one.
	fresh := newTestApp(t)
	fresh.cfg.Security.CodeHash, fresh.cfg.Security.CodeSalt = "", ""
	rr = fresh.do(t, http.MethodPost, "/phone/security", url.Values{"requireCode": {"0", "1"}})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "set a security code first") {
		t.Fatalf("enabling without a code returned %d: %s", rr.Code, rr.Body.String())
	}

	rr = fresh.do(t, http.MethodPost, "/phone/security", url.Values{"requireCode": {"0", "1"}, "securityCode": {"hunter42"}})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("setting a code returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg = fresh.reloadConfig(t)
	if !cfg.Security.RequireCode || !verifySecurityCode(cfg, "hunter42") {
		t.Fatal("the new security code was not stored")
	}
	if strings.Contains(fresh.do(t, http.MethodGet, "/phone", nil).Body.String(), "hunter42") {
		t.Fatal("the security code must never be rendered back to the page")
	}
}

func TestAgentSettingsSave(t *testing.T) {
	a := newTestApp(t)
	rr := a.do(t, http.MethodPost, "/agents/save", url.Values{
		"codexPath":      {`C:\Tools\codex.exe`},
		"claudePath":     {`C:\Tools\claude.exe`},
		"cwd":            {`C:\Users\User`},
		"codexCwd":       {`C:\Users\User\Projects`},
		"claudeCwd":      {`C:\Users\User`},
		"defaultAgent":   {"A"},
		"turnTimeout":    {"45"},
		"permissionMode": {"plan"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("agent save returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	if cfg.CodexPath != `C:\Tools\codex.exe` || cfg.ClaudePath != `C:\Tools\claude.exe` {
		t.Fatalf("executable paths were not stored: %#v", cfg)
	}
	if cfg.DefaultAgent != "A" || cfg.TurnTimeoutMinutes != 45 || cfg.Claude.PermissionMode != "plan" {
		t.Fatalf("behaviour settings were not stored: %#v", cfg)
	}
	if cfg.CodexCwd != `C:\Users\User\Projects` {
		t.Fatalf("per-agent folder was not stored: %q", cfg.CodexCwd)
	}
	// A per-agent folder equal to the shared one is stored as "follow the
	// shared folder" rather than as a copy that would stop tracking it.
	if cfg.ClaudeCwd != "" || cfg.claudeWorkingDir() != `C:\Users\User` {
		t.Fatalf("claude folder should follow the shared folder, got %q", cfg.ClaudeCwd)
	}
	if cfg.codexWorkingDir() != `C:\Users\User\Projects` {
		t.Fatalf("codex working folder resolved to %q", cfg.codexWorkingDir())
	}

	rr = a.do(t, http.MethodPost, "/agents/save", url.Values{"turnTimeout": {"9000"}})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("an out-of-range turn timeout returned %d, want 400", rr.Code)
	}
	if a.reloadConfig(t).TurnTimeoutMinutes != 45 {
		t.Fatal("a rejected form must not change stored settings")
	}
}

// A form that only shows some fields must never blank the ones it omits.
func TestPartialFormKeepsUntouchedSettings(t *testing.T) {
	a := newTestApp(t)
	if rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"defaultAgent": {"A"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("partial save returned %d", rr.Code)
	}
	cfg := a.reloadConfig(t)
	if cfg.DefaultAgent != "A" {
		t.Fatal("the submitted field was not applied")
	}
	if cfg.CodexPath == "" || cfg.Cwd == "" || cfg.GoogleVoice.AllowedFrom == "" {
		t.Fatalf("omitted settings were cleared: %#v", cfg)
	}
}

func TestConnectionsSaveRequiresCredentials(t *testing.T) {
	a := newTestApp(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("gmailMethod", GmailMethodAppPassword)
	_ = mw.WriteField("gmailEmail", "you@gmail.com")
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/connections/save", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "aisms_session", Value: a.cfg.LocalToken})
	rr := httptest.NewRecorder()
	a.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "App Password required") {
		t.Fatalf("missing App Password returned %d: %s", rr.Code, rr.Body.String())
	}
	if a.reloadConfig(t).Gmail.Method != "" {
		t.Fatal("a rejected connection form must not change the stored method")
	}
}

func TestWindowPreferencesApplyToTheShell(t *testing.T) {
	a := newTestApp(t)
	rr := a.do(t, http.MethodPost, "/settings/save", url.Values{
		"theme":      {"dark"},
		"compact":    {"0", "1"},
		"alerts":     {"0"},
		"alertSound": {"0", "1"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("settings save returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	if cfg.UI.Theme != ThemeDark || !cfg.UI.Compact || cfg.UI.Alerts || !cfg.UI.AlertSound {
		t.Fatalf("window preferences were not stored: %#v", cfg.UI)
	}
	body := a.do(t, http.MethodGet, "/settings", nil).Body.String()
	if !strings.Contains(body, `data-theme="dark"`) || !strings.Contains(body, `data-compact="1"`) {
		t.Fatal("the shell does not reflect the saved theme and density")
	}
	if strings.Contains(body, `data-alerts="1"`) {
		t.Fatal("error alerts were turned off but the page still enables them")
	}
}

// A settings change that only affects the window must not restart the bridge.
func TestWindowPreferencesDoNotRestartTheHost(t *testing.T) {
	a := newTestApp(t)
	stopped := make(chan struct{}, 1)
	a.stop = func() { stopped <- struct{}{} }
	if rr := a.do(t, http.MethodPost, "/settings/save", url.Values{"theme": {"dark"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("settings save returned %d", rr.Code)
	}
	select {
	case <-stopped:
		t.Fatal("changing the theme restarted the background host")
	case <-time.After(2 * time.Second):
	}
}

func TestExportLogsReturnsZip(t *testing.T) {
	a := newTestApp(t)
	activityLogForStatePath(a.statePath).Add("success", "reply", "Reply sent through Google Voice", "8455551212", "C", "m1")
	rr := a.do(t, http.MethodGet, "/logs/export", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("export returned %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Disposition"), ".zip") {
		t.Fatalf("export is not offered as a download: %q", rr.Header().Get("Content-Disposition"))
	}
	z, err := zip.NewReader(bytes.NewReader(rr.Body.Bytes()), int64(rr.Body.Len()))
	if err != nil {
		t.Fatalf("export is not a readable zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
	}
	if !names["activity.jsonl"] || !names["flipai-status.txt"] {
		t.Fatalf("export is missing expected members: %v", names)
	}
}

func TestFoldersJSONListsDirectories(t *testing.T) {
	a := newTestApp(t)
	if err := os.MkdirAll(a.dataDir+"/projects/inner", 0o755); err != nil {
		t.Fatal(err)
	}
	rr := a.do(t, http.MethodGet, "/folders.json?path="+url.QueryEscape(a.dataDir), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("folders returned %d", rr.Code)
	}
	var payload struct {
		Path    string        `json:"path"`
		Parent  string        `json:"parent"`
		Folders []folderEntry `json:"folders"`
		Error   string        `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error != "" || payload.Path != a.dataDir || payload.Parent == "" {
		t.Fatalf("unexpected folder payload: %+v", payload)
	}
	found := false
	for _, f := range payload.Folders {
		if f.Name == "projects" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sub-folder missing from %+v", payload.Folders)
	}
}

func TestStatusJSONReportsRealState(t *testing.T) {
	a := newTestApp(t)
	rr := a.do(t, http.MethodGet, "/status.json", nil)
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["running"] != false || payload["gmailReady"] != false {
		t.Fatalf("an unconfigured install should not report itself ready: %v", payload)
	}
	if payload["version"] != version || payload["allowedCount"] != float64(1) {
		t.Fatalf("status payload is wrong: %v", payload)
	}
}

func TestActivityFeedCarriesDurations(t *testing.T) {
	a := newTestApp(t)
	activityLogForStatePath(a.statePath).AddTimed("success", "agent", "Agent completed successfully", "8455551212", "C", "m1", 2400*time.Millisecond)
	rr := a.do(t, http.MethodGet, "/activity.json", nil)
	var events []ActivityEvent
	if err := json.Unmarshal(rr.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].DurationMS != 2400 {
		t.Fatalf("duration was not recorded: %+v", events)
	}
}

func TestClearingActivityRedirectsBack(t *testing.T) {
	a := newTestApp(t)
	activityLogForStatePath(a.statePath).Add("info", "bridge", "Background bridge started", "", "", "")
	rr := a.do(t, http.MethodPost, "/activity/clear", nil)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "/activity") {
		t.Fatalf("clear returned %d -> %q", rr.Code, rr.Header().Get("Location"))
	}
	if got := activityLogForStatePath(a.statePath).Recent(10); len(got) != 0 {
		t.Fatalf("activity log still holds %d events", len(got))
	}
}

func TestResetSetupKeepsTheWindowUsable(t *testing.T) {
	a := newTestApp(t)
	token := a.cfg.LocalToken
	if err := saveAppPasswordSecret(appPasswordPath(a.dataDir), "you@gmail.com", "abcd efgh ijkl mnop"); err != nil {
		t.Fatal(err)
	}
	if rr := a.do(t, http.MethodPost, "/settings/reset", nil); rr.Code != http.StatusSeeOther {
		t.Fatalf("reset returned %d", rr.Code)
	}
	cfg := a.reloadConfig(t)
	if cfg.LocalToken != token {
		t.Fatal("reset changed the session token and would lock the open window out")
	}
	if cfg.Gmail.Method != "" || cfg.GoogleVoice.AllowedFrom != "" {
		t.Fatalf("reset left configuration behind: %#v", cfg)
	}
	if hasAppPasswordSecret(appPasswordPath(a.dataDir)) {
		t.Fatal("reset left the stored Gmail App Password on disk")
	}
}

func TestResultPageStaysInsideTheApp(t *testing.T) {
	cases := map[string]string{
		"":                            "/",
		"http://127.0.0.1:8765/phone": "/phone",
		"https://example.com/phone":   "/",
		"::not a url::":               "/",
	}
	for referer, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/gmail/test", nil)
		req.Host = "127.0.0.1:8765"
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		if got := backTarget(req); got != want {
			t.Fatalf("referer %q -> %q, want %q", referer, got, want)
		}
	}
}

func TestAllowlistRepresentationsStayInStep(t *testing.T) {
	gv := GoogleVoiceConfig{AllowedFrom: "845-555-1212\n+1 (212) 555-0147"}
	syncAllowedNumbers(&gv)
	if len(gv.AllowedNumbers) != 2 {
		t.Fatalf("expected 2 records, got %#v", gv.AllowedNumbers)
	}
	if gv.AllowedFrom != "2125550147\n8455551212" {
		t.Fatalf("routing list was not normalized: %q", gv.AllowedFrom)
	}
	gv.AllowedNumbers[0].Label = "Work"
	if err := addAllowedNumber(&gv, "8455551212", "duplicate"); err == nil {
		t.Fatal("a duplicate number was accepted")
	}
	if err := removeAllowedNumber(&gv, "2125550147"); err != nil {
		t.Fatal(err)
	}
	if gv.AllowedFrom != "8455551212" || len(gv.AllowedNumbers) != 1 {
		t.Fatalf("removal left the lists inconsistent: %q %#v", gv.AllowedFrom, gv.AllowedNumbers)
	}
}

// An install made before the desktop redesign has no "ui" block. Decoding must
// not read those absent booleans as deliberate "off" choices.
func TestUpgradeKeepsWindowDefaults(t *testing.T) {
	tmp := t.TempDir()
	legacy := `{"codexPath":"codex","claudePath":"claude","cwd":"C:\\Users\\User","listen":"127.0.0.1:8765","localToken":"abc","defaultAgent":"C","gmail":{"method":"app_password"},"googleVoice":{"allowedFrom":"8455551212"},"security":{"requireCode":true,"codeSalt":"s","codeHash":"h"}}`
	path := tmp + "/bridge.json"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UI.CloseToTray || !cfg.UI.Alerts || cfg.UI.Theme != ThemeLight {
		t.Fatalf("upgrade did not restore window defaults: %#v", cfg.UI)
	}
	if len(cfg.GoogleVoice.AllowedNumbers) != 1 || cfg.GoogleVoice.AllowedNumbers[0].Number != "8455551212" {
		t.Fatalf("the existing allowlist was not migrated: %#v", cfg.GoogleVoice.AllowedNumbers)
	}
	// An explicit choice survives the next load.
	cfg.UI.CloseToTray = false
	if err := saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := loadConfig(path, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if again.UI.CloseToTray {
		t.Fatal("an explicit close-to-tray choice was overwritten by the default")
	}
}

// countingMailClient records how often the bridge asks for the mailbox listing.
type countingMailClient struct{ listCalls int }

func (c *countingMailClient) Authorized() bool           { return true }
func (c *countingMailClient) Test(context.Context) error { return nil }
func (c *countingMailClient) List(context.Context) ([]string, error) {
	c.listCalls++
	return nil, nil
}
func (c *countingMailClient) Get(context.Context, string) (GmailMessage, error) {
	return GmailMessage{}, nil
}
func (c *countingMailClient) SendText(context.Context, string, string) error { return nil }
