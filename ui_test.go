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
	"runtime"
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
	// One number reaches Codex, and neither agent asks for a code -- which is
	// how a fresh install now arrives.
	allowTestNumber(&cfg, "C", "8455551212")
	cfg.Security.AgentsMigrated = true
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
		// The end-to-end check card was removed: it restated status the page
		// already shows and pointed at a Phone page that no longer exists.
		"/connections": {"Gmail / Google Voice"},
		// Everything an agent owns is on its own pane now: who may reach it, its
		// code, its instruction and how it replies.
		"/agents":   {"Codex", "Claude", "Executable path", "Allowed phone numbers", "Security code", "Replies from"},
		"/activity": {"All stages", "Search activity", "Privacy"},
		// Advanced was folded in, so one settings page holds both.
		"/settings": {"Startup", "Appearance", "Notifications", "This install", "Local service", "Log files", "Service tools", "Message routing"},
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
		for _, item := range []string{`href="/connections"`, `href="/agents"`, `href="/activity"`, `href="/settings"`} {
			if !strings.Contains(body, item) {
				t.Errorf("%s is missing nav entry %s", path, item)
			}
		}
		if strings.Contains(body, "//fonts.googleapis.com") || strings.Contains(body, "https://cdn") {
			t.Fatalf("%s must not depend on external fonts or CDNs", path)
		}
	}
}

// The two retired pages still answer, so a bookmark or an older in-app link
// lands where their contents went.
func TestRetiredPagesRedirectToWhereTheirSettingsWent(t *testing.T) {
	a := newTestApp(t)
	for path, want := range map[string]string{"/phone": "/agents", "/advanced": "/settings"} {
		rr := a.do(t, http.MethodGet, path, nil)
		if rr.Code != http.StatusFound {
			t.Errorf("%s status=%d, want a redirect", path, rr.Code)
		}
		if got := rr.Header().Get("Location"); got != want {
			t.Errorf("%s redirected to %q, want %q", path, got, want)
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
	for _, path := range []string{"/bridge/pause", "/agents/numbers/add", "/settings/reset", "/quit", "/activity/clear"} {
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

func TestAgentNumberLifecycle(t *testing.T) {
	a := newTestApp(t)

	rr := a.do(t, http.MethodPost, "/agents/numbers/add", url.Values{
		"agent": {"C"}, "newNumber": {"(212) 555-0147"}, "newLabel": {"Work"}, "newAccess": {"sms"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("add returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	agent, phone, ok := agentForSender(cfg, "2125550147")
	if !ok || agent != "C" {
		t.Fatalf("the number did not land on Codex: agent=%q ok=%v", agent, ok)
	}
	if phone.Label != "Work" || phone.Access != AccessSMS || phone.Added.IsZero() {
		t.Fatalf("label, access and date were not recorded: %#v", phone)
	}
	// A texts-only number still reaches the SMS parser, and never the calls.
	if !strings.Contains(cfg.GoogleVoice.AllowedFrom, "2125550147") {
		t.Fatalf("the routing allowlist was not updated: %q", cfg.GoogleVoice.AllowedFrom)
	}
	if phone.AllowsVoice() {
		t.Fatal("a texts-only number must not be allowed to call")
	}

	page := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	if !strings.Contains(page, "(212) 555-0147") || !strings.Contains(page, "Work") {
		t.Fatal("the Agents page does not list the new number with its label")
	}

	// A number reaches one agent. Claiming it for the other has to be refused,
	// or the allowlist would not answer "who may command this agent".
	rr = a.do(t, http.MethodPost, "/agents/numbers/add", url.Values{
		"agent": {"A"}, "newNumber": {"2125550147"}, "newAccess": {"all"},
	})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "one agent only") {
		t.Fatalf("claiming a number for a second agent returned %d: %s", rr.Code, rr.Body.String())
	}

	if rr := a.do(t, http.MethodPost, "/agents/numbers/remove", url.Values{"number": {"C:2125550147"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("remove returned %d: %s", rr.Code, rr.Body.String())
	}
	if _, _, ok := agentForSender(a.reloadConfig(t), "2125550147"); ok {
		t.Fatal("the number was not removed")
	}

	if rr := a.do(t, http.MethodPost, "/agents/numbers/add", url.Values{"agent": {"C"}, "newNumber": {"nonsense"}}); rr.Code != http.StatusBadRequest {
		t.Fatalf("a malformed number returned %d, want 400", rr.Code)
	}
}

func TestReplyBehaviourIsPerAgent(t *testing.T) {
	a := newTestApp(t)
	rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"replyMaxChars": {"50000"}})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "reply length") {
		t.Fatalf("out-of-range reply length returned %d: %s", rr.Code, rr.Body.String())
	}

	rr = a.do(t, http.MethodPost, "/agents/save", url.Values{
		"replyMaxChars":          {"420"},
		"maxReplyParts":          {"6"},
		"codexAck":               {"1"},
		"codexProgress":          {"0"},
		"codexProgressInterval":  {"300"},
		"claudeAck":              {"0"},
		"claudeProgress":         {"1"},
		"claudeProgressInterval": {"60"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("save returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	if cfg.GoogleVoice.ReplyMaxChars != 420 || cfg.GoogleVoice.MaxReplyParts != 6 {
		t.Fatalf("shared reply sizing was not stored: %#v", cfg.GoogleVoice)
	}
	codex, claude := agentSettings(cfg, "C"), agentSettings(cfg, "A")
	if !codex.ackEnabled() || codex.progressEnabled() || codex.ProgressIntervalSeconds != 300 {
		t.Fatalf("Codex reply behaviour was not stored: %#v", codex)
	}
	if claude.ackEnabled() || !claude.progressEnabled() || claude.ProgressIntervalSeconds != 60 {
		t.Fatalf("Claude reply behaviour was not stored: %#v", claude)
	}
}

func TestAgentSecurityCodeToggle(t *testing.T) {
	a := newTestApp(t)

	// Requiring a code before setting one has to be refused, or the agent would
	// silently stop answering every text.
	rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"codexRequireCode": {"1"}})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "security code") {
		t.Fatalf("requiring a code with none set returned %d: %s", rr.Code, rr.Body.String())
	}

	// Setting the code and requiring it in one save works.
	rr = a.do(t, http.MethodPost, "/agents/save", url.Values{
		"codexCode": {"hunter42"}, "codexRequireCode": {"1"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("setting a code returned %d: %s", rr.Code, rr.Body.String())
	}
	cfg := a.reloadConfig(t)
	codex := agentSettings(cfg, "C")
	if !codex.RequireCode || !verifyAgentCode(codex, "hunter42") {
		t.Fatalf("the Codex code was not stored: %#v", codex)
	}
	// One agent's code is its own.
	if claude := agentSettings(cfg, "A"); claude.RequireCode || verifyAgentCode(claude, "hunter42") {
		t.Fatalf("the code leaked onto the other agent: %#v", claude)
	}

	// Turning it off keeps the stored code so it can be turned back on.
	if rr := a.do(t, http.MethodPost, "/agents/save", url.Values{"codexRequireCode": {"0"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("disabling returned %d: %s", rr.Code, rr.Body.String())
	}
	if codex := agentSettings(a.reloadConfig(t), "C"); codex.RequireCode || codex.CodeHash == "" {
		t.Fatalf("expected the requirement off with the code retained: %#v", codex)
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

// ---------------------------------------------------------------------------
// Updates
// ---------------------------------------------------------------------------

func TestVersionOrderIsNumeric(t *testing.T) {
	cases := []struct {
		a, b string
		less bool
	}{
		{"0.9.0", "0.10.0", true}, // string ordering gets this one wrong
		{"0.10.0", "0.9.0", false},
		{"0.9.0", "0.9.0", false},
		{"0.9.0", "v0.9.1", true},
		{"1.0.0", "0.99.9", false},
		{"0.9.0", "0.9.0-rc1", false},
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.less {
			t.Errorf("versionLess(%q,%q)=%v, want %v", c.a, c.b, got, c.less)
		}
	}
}

func TestUpdateCheckStoresPublishedRelease(t *testing.T) {
	a := newTestApp(t)
	next := bumpVersion(version)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name":"v` + next + `","html_url":"https://example.invalid/releases/v` + next + `",
			"body":"notes","draft":false,"prerelease":false,
			"assets":[
				{"name":"FlipAi-Setup-v` + next + `.exe","browser_download_url":"https://example.invalid/FlipAi-Setup-v` + next + `.exe"},
				{"name":"SHA256SUMS.txt","browser_download_url":"https://example.invalid/SHA256SUMS.txt"}
			]}`))
	}))
	defer srv.Close()
	old := updateAPIURL
	updateAPIURL = srv.URL
	defer func() { updateAPIURL = old }()

	info := a.checkForUpdate(context.Background(), true)
	if !info.Newer() || info.Version != next {
		t.Fatalf("expected %s to be seen as newer: %#v", next, info)
	}
	if loadUpdateState(a.statePath).Version != next {
		t.Fatal("the release check was not stored in state")
	}

	// The banner must appear on an ordinary page, not only in Settings.
	body := a.do(t, http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(body, "FlipAi "+next+" is available") || !strings.Contains(body, `action="/update/install"`) {
		t.Fatal("the update banner is missing from the page")
	}
	if !strings.Contains(body, "not a fresh setup") {
		t.Fatal("the banner should say an update keeps existing settings")
	}
}

func TestUpdateCheckOnCurrentVersionSaysSo(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v` + version + `","draft":false,"prerelease":false,
			"assets":[{"name":"FlipAi-Setup-v` + version + `.exe","browser_download_url":"https://example.invalid/x.exe"}]}`))
	}))
	defer srv.Close()
	old := updateAPIURL
	updateAPIURL = srv.URL
	defer func() { updateAPIURL = old }()

	rr := a.do(t, http.MethodPost, "/update/check", nil)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "update-current") {
		t.Fatalf("check returned %d -> %q", rr.Code, rr.Header().Get("Location"))
	}
	if strings.Contains(a.do(t, http.MethodGet, "/", nil).Body.String(), "is available") {
		t.Fatal("an up-to-date install must not show an update banner")
	}
}

func bumpVersion(v string) string {
	parts := versionParts(v)
	for len(parts) < 3 {
		parts = append(parts, 0)
	}
	parts[1]++
	parts[2] = 0
	return itoa(parts[0]) + "." + itoa(parts[1]) + "." + itoa(parts[2])
}

// ---------------------------------------------------------------------------
// Start before sign-in
// ---------------------------------------------------------------------------

// Enabling the boot task is the only place FlipAi asks for administrator
// rights. Where that is unavailable, the attempt must fail loudly and leave
// credential protection exactly as it was.
func TestBootStartupFailureLeavesSecretsAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this checks the non-Windows refusal path")
	}
	a := newTestApp(t)
	if err := saveAppPasswordSecret(appPasswordPath(a.dataDir), "you@gmail.com", "abcd efgh ijkl mnop"); err != nil {
		t.Fatal(err)
	}
	rr := a.do(t, http.MethodPost, "/settings/bootstartup", url.Values{"bootStartup": {"0", "1"}})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected a clear failure, got %d: %s", rr.Code, rr.Body.String())
	}
	if a.reloadConfig(t).Security.MachineScopeSecrets {
		t.Fatal("a failed attempt must not record machine-scope credentials")
	}
	if secretScopeIsMachine() {
		t.Fatal("the secret scope was left switched after a failed attempt")
	}
	if _, err := loadAppPasswordSecret(appPasswordPath(a.dataDir)); err != nil {
		t.Fatalf("the stored App Password is no longer readable: %v", err)
	}
}

// Re-protecting credentials must round-trip every stored secret.
func TestSecretScopeRewriteKeepsCredentialsReadable(t *testing.T) {
	a := newTestApp(t)
	if err := saveAppPasswordSecret(appPasswordPath(a.dataDir), "you@gmail.com", "abcd efgh ijkl mnop"); err != nil {
		t.Fatal(err)
	}
	if err := saveClaudeToken(claudeTokenPath(a.dataDir), "sk-ant-oat01-"+strings.Repeat("x", 40)); err != nil {
		t.Fatal(err)
	}
	if err := a.applySecretScope(true); err != nil {
		t.Fatalf("re-protect: %v", err)
	}
	defer func() { _ = a.applySecretScope(false) }()
	secret, err := loadAppPasswordSecret(appPasswordPath(a.dataDir))
	if err != nil || secret.Email != "you@gmail.com" {
		t.Fatalf("App Password did not survive re-protection: %v %#v", err, secret)
	}
	if !hasClaudeToken(claudeTokenPath(a.dataDir)) {
		t.Fatal("Claude token did not survive re-protection")
	}
}

func TestSettingsOffersStartupChoices(t *testing.T) {
	a := newTestApp(t)
	body := a.do(t, http.MethodGet, "/settings", nil).Body.String()
	for _, want := range []string{
		"Start FlipAi with Windows",
		"Start before sign-in",
		"administrator approval once",
		`action="/settings/bootstartup"`,
		"Check for updates",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Settings is missing %q", want)
		}
	}
}

// Pressing a test must answer where it was pressed. Every one of them used to
// render a whole result page, so testing anything navigated away from the
// settings the user was in the middle of.
func TestTestsAnswerInlineRatherThanNavigatingAway(t *testing.T) {
	a := newTestApp(t)

	for _, page := range []string{"/connections", "/agents", "/settings"} {
		body := a.do(t, http.MethodGet, page, nil).Body.String()
		for _, gone := range []string{`href="/gmail/test"`, `href="/codex/test"`, `href="/claude/test"`,
			`action="/connections/flowtest"`, `action="/health/check"`} {
			if strings.Contains(body, gone) {
				t.Errorf("%s still reaches a test through %s instead of running it in place", page, gone)
			}
		}
	}

	agents := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	for _, want := range []string{`data-test="/codex/test"`, `data-test="/claude/test"`} {
		if !strings.Contains(agents, want) {
			t.Errorf("Agents page is missing the inline test control %s", want)
		}
	}

	// The same endpoint answers with JSON when the page asks for the result
	// rather than for somewhere to land.
	req := httptest.NewRequest(http.MethodGet, "/gmail/test", nil)
	req.Header.Set("X-FlipAi-Inline", "1")
	req.AddCookie(&http.Cookie{Name: "aisms_session", Value: a.cfg.LocalToken})
	rr := httptest.NewRecorder()
	a.handler().ServeHTTP(rr, req)
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("an inline test replied with %q, want JSON", ct)
	}
	var out struct {
		OK      bool   `json:"ok"`
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("inline result is not JSON: %v (%s)", err, rr.Body.String())
	}
	if out.Title == "" {
		t.Errorf("an inline result must carry something to show: %s", rr.Body.String())
	}
}
