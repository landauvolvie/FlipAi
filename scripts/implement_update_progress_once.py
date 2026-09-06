from pathlib import Path

ROOT = Path('.')

def read(path):
    return (ROOT / path).read_text(encoding='utf-8')

def write(path, content):
    (ROOT / path).write_text(content, encoding='utf-8')

def replace_once(path, old, new):
    text = read(path)
    if old not in text:
        raise SystemExit(f'missing expected block in {path}: {old[:120]!r}')
    text = text.replace(old, new, 1)
    write(path, text)

# ---------------------------------------------------------------------------
# Update engine: persist real download progress, expose 100% only after the
# installer has been verified, and keep the existing checksum/trusted-host
# protections intact.
# ---------------------------------------------------------------------------
replace_once('update.go', '''var (
\tupdateDownloadMu sync.Mutex
\tupdateSnapshotMu sync.RWMutex
\tupdateSnapshot   ReleaseInfo
)''', '''var (
\tupdateDownloadMu sync.Mutex
\tupdateStateMu    sync.Mutex
\tupdateSnapshotMu sync.RWMutex
\tupdateSnapshot   ReleaseInfo
)''')

replace_once('update.go', '''\tError            string    `json:"error,omitempty"`
\tDownloading      bool      `json:"downloading,omitempty"`
\tDownloadedPath   string    `json:"downloadedPath,omitempty"`
\tDownloadedSHA256 string    `json:"downloadedSha256,omitempty"`
\tDownloadedAt     time.Time `json:"downloadedAt,omitempty"`
}''', '''\tError            string    `json:"error,omitempty"`
\tDownloading      bool      `json:"downloading,omitempty"`
\tDownloadBytes    int64     `json:"downloadBytes,omitempty"`
\tDownloadTotal    int64     `json:"downloadTotal,omitempty"`
\tDownloadPercent  int       `json:"downloadPercent,omitempty"`
\tDownloadedPath   string    `json:"downloadedPath,omitempty"`
\tDownloadedSHA256 string    `json:"downloadedSha256,omitempty"`
\tDownloadedAt     time.Time `json:"downloadedAt,omitempty"`
}''')

replace_once('update.go', '''func loadUpdateState(statePath string) ReleaseInfo {
\tvar info ReleaseInfo
\tif raw, err := os.ReadFile(updateStatePath(statePath)); err == nil {
\t\t_ = json.Unmarshal(raw, &info)
\t}
\tif info.DownloadedPath != "" && !info.Ready() {
\t\tinfo.DownloadedPath = ""
\t\tinfo.DownloadedSHA256 = ""
\t\tinfo.DownloadedAt = time.Time{}
\t}
\trememberUpdateSnapshot(info)
\treturn info
}

func saveUpdateState(statePath string, info ReleaseInfo) {
\trememberUpdateSnapshot(info)
\tif raw, err := json.MarshalIndent(info, "", "  "); err == nil {
\t\t_ = os.WriteFile(updateStatePath(statePath), raw, 0o600)
\t}
}''', '''func loadUpdateState(statePath string) ReleaseInfo {
\tupdateStateMu.Lock()
\tdefer updateStateMu.Unlock()
\tvar info ReleaseInfo
\tif raw, err := os.ReadFile(updateStatePath(statePath)); err == nil {
\t\t_ = json.Unmarshal(raw, &info)
\t}
\tif info.DownloadedPath != "" && !info.Ready() {
\t\tinfo.DownloadedPath = ""
\t\tinfo.DownloadedSHA256 = ""
\t\tinfo.DownloadedAt = time.Time{}
\t\tinfo.DownloadBytes = 0
\t\tinfo.DownloadTotal = 0
\t\tinfo.DownloadPercent = 0
\t}
\trememberUpdateSnapshot(info)
\treturn info
}

func saveUpdateState(statePath string, info ReleaseInfo) {
\tif info.DownloadPercent < 0 {
\t\tinfo.DownloadPercent = 0
\t}
\tif info.DownloadPercent > 100 {
\t\tinfo.DownloadPercent = 100
\t}
\trememberUpdateSnapshot(info)
\tupdateStateMu.Lock()
\tdefer updateStateMu.Unlock()
\tif raw, err := json.MarshalIndent(info, "", "  "); err == nil {
\t\t_ = os.WriteFile(updateStatePath(statePath), raw, 0o600)
\t}
}

// updateDownloadProgress converts byte progress into the percentage shown in
// the sidebar. 100% is reserved for a checksum-verified installer, so a file
// that has finished transferring but is still being verified stays at 99%.
func updateDownloadProgress(done, total int64) int {
\tif done <= 0 || total <= 0 {
\t\treturn 0
\t}
\tp := int(done * 100 / total)
\tif p < 1 {
\t\tp = 1
\t}
\tif p > 99 {
\t\tp = 99
\t}
\treturn p
}''')

replace_once('update.go', '''\tif info.Version == current.Version && info.AssetURL == current.AssetURL && current.Ready() {
\t\tinfo.DownloadedPath = current.DownloadedPath
\t\tinfo.DownloadedSHA256 = current.DownloadedSHA256
\t\tinfo.DownloadedAt = current.DownloadedAt
\t}
\tinfo.Error = ""''', '''\tif info.Version == current.Version && info.AssetURL == current.AssetURL {
\t\tif current.Ready() {
\t\t\tinfo.DownloadedPath = current.DownloadedPath
\t\t\tinfo.DownloadedSHA256 = current.DownloadedSHA256
\t\t\tinfo.DownloadedAt = current.DownloadedAt
\t\t\tinfo.DownloadBytes = current.DownloadBytes
\t\t\tinfo.DownloadTotal = current.DownloadTotal
\t\t\tinfo.DownloadPercent = 100
\t\t} else if current.Downloading {
\t\t\tinfo.Downloading = true
\t\t\tinfo.DownloadBytes = current.DownloadBytes
\t\t\tinfo.DownloadTotal = current.DownloadTotal
\t\t\tinfo.DownloadPercent = current.DownloadPercent
\t\t}
\t}
\tinfo.Error = ""''')

old_stage = '''func (a *App) stageUpdate(ctx context.Context, info ReleaseInfo) {
\tcurrent := loadUpdateState(a.statePath)
\tif current.Version != info.Version || current.AssetURL != info.AssetURL {
\t\tcurrent = info
\t}
\tif current.Ready() {
\t\treturn
\t}
\tcurrent.Downloading = true
\tcurrent.Error = ""
\tcurrent.DownloadedPath = ""
\tcurrent.DownloadedSHA256 = ""
\tcurrent.DownloadedAt = time.Time{}
\tsaveUpdateState(a.statePath, current)

\tdlCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
\tdefer cancel()
\tpath, err := downloadUpdate(dlCtx, current)
\tlatest := loadUpdateState(a.statePath)
\tif latest.Version != current.Version || latest.AssetURL != current.AssetURL {
\t\treturn
\t}
\tlatest.Downloading = false
\tif err != nil {
\t\tlatest.Error = truncate(err.Error(), 200)
\t\tlatest.DownloadedPath = ""
\t\tlatest.DownloadedSHA256 = ""
\t\tlatest.DownloadedAt = time.Time{}
\t\tsaveUpdateState(a.statePath, latest)
\t\treturn
\t}
\tsum, hashErr := sha256File(path)
\tif hashErr != nil {
\t\tlatest.Error = truncate(hashErr.Error(), 200)
\t\tlatest.DownloadedPath = ""
\t\tlatest.DownloadedSHA256 = ""
\t\tlatest.DownloadedAt = time.Time{}
\t\tsaveUpdateState(a.statePath, latest)
\t\treturn
\t}
\tlatest.Error = ""
\tlatest.DownloadedPath = path
\tlatest.DownloadedSHA256 = sum
\tlatest.DownloadedAt = time.Now()
\tsaveUpdateState(a.statePath, latest)
}'''
new_stage = '''func (a *App) stageUpdate(ctx context.Context, info ReleaseInfo) {
\tcurrent := loadUpdateState(a.statePath)
\tif current.Version != info.Version || current.AssetURL != info.AssetURL {
\t\tcurrent = info
\t}
\tif current.Ready() {
\t\treturn
\t}
\tcurrent.Downloading = true
\tcurrent.Error = ""
\tcurrent.DownloadBytes = 0
\tcurrent.DownloadTotal = 0
\tcurrent.DownloadPercent = 0
\tcurrent.DownloadedPath = ""
\tcurrent.DownloadedSHA256 = ""
\tcurrent.DownloadedAt = time.Time{}
\tsaveUpdateState(a.statePath, current)

\tdlCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
\tdefer cancel()
\tlastPercent := -1
\tlastSaved := time.Time{}
\tpath, err := downloadUpdateWithProgress(dlCtx, current, func(done, total int64) {
\t\tpercent := updateDownloadProgress(done, total)
\t\t// Avoid rewriting update.json for every network buffer. Percentage
\t\t// changes are written immediately; unknown-size transfers update twice a
\t\t// second so the byte counters still prove the download is moving.
\t\tif percent == lastPercent && time.Since(lastSaved) < 500*time.Millisecond {
\t\t\treturn
\t\t}
\t\tlatest := currentUpdateSnapshot()
\t\tif latest.Version != current.Version || latest.AssetURL != current.AssetURL {
\t\t\treturn
\t\t}
\t\tlatest.Downloading = true
\t\tlatest.DownloadBytes = done
\t\tlatest.DownloadTotal = total
\t\tlatest.DownloadPercent = percent
\t\tsaveUpdateState(a.statePath, latest)
\t\tlastPercent = percent
\t\tlastSaved = time.Now()
\t})
\tlatest := loadUpdateState(a.statePath)
\tif latest.Version != current.Version || latest.AssetURL != current.AssetURL {
\t\treturn
\t}
\tlatest.Downloading = false
\tif err != nil {
\t\tlatest.Error = truncate(err.Error(), 200)
\t\tlatest.DownloadBytes = 0
\t\tlatest.DownloadTotal = 0
\t\tlatest.DownloadPercent = 0
\t\tlatest.DownloadedPath = ""
\t\tlatest.DownloadedSHA256 = ""
\t\tlatest.DownloadedAt = time.Time{}
\t\tsaveUpdateState(a.statePath, latest)
\t\treturn
\t}
\tsum, hashErr := sha256File(path)
\tif hashErr != nil {
\t\tlatest.Error = truncate(hashErr.Error(), 200)
\t\tlatest.DownloadBytes = 0
\t\tlatest.DownloadTotal = 0
\t\tlatest.DownloadPercent = 0
\t\tlatest.DownloadedPath = ""
\t\tlatest.DownloadedSHA256 = ""
\t\tlatest.DownloadedAt = time.Time{}
\t\tsaveUpdateState(a.statePath, latest)
\t\treturn
\t}
\tlatest.Error = ""
\tlatest.DownloadedPath = path
\tlatest.DownloadedSHA256 = sum
\tlatest.DownloadedAt = time.Now()
\tif st, statErr := os.Stat(path); statErr == nil {
\t\tlatest.DownloadBytes = st.Size()
\t\tif latest.DownloadTotal <= 0 {
\t\t\tlatest.DownloadTotal = st.Size()
\t\t}
\t}
\tlatest.DownloadPercent = 100
\tsaveUpdateState(a.statePath, latest)
}'''
replace_once('update.go', old_stage, new_stage)

replace_once('update.go', '''func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {
\tupdateDownloadMu.Lock()
\tdefer updateDownloadMu.Unlock()''', '''func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {
\treturn downloadUpdateWithProgress(ctx, info, nil)
}

func downloadUpdateWithProgress(ctx context.Context, info ReleaseInfo, progress func(done, total int64)) (string, error) {
\tupdateDownloadMu.Lock()
\tdefer updateDownloadMu.Unlock()''')

replace_once('update.go', '''\tif sum, err := sha256File(dest); err == nil && strings.EqualFold(sum, want) {
\t\treturn dest, nil
\t}
\t_ = os.Remove(dest)
\tsum, err := download(ctx, info.AssetURL, dest)''', '''\tif sum, err := sha256File(dest); err == nil && strings.EqualFold(sum, want) {
\t\tif st, statErr := os.Stat(dest); statErr == nil && progress != nil {
\t\t\tprogress(st.Size(), st.Size())
\t\t}
\t\treturn dest, nil
\t}
\t_ = os.Remove(dest)
\t// A killed process can leave only a .part file. It is never executable and
\t// never trusted as a completed update; clear stale parts before retrying.
\tif entries, readErr := os.ReadDir(dir); readErr == nil {
\t\tfor _, entry := range entries {
\t\t\tif !entry.IsDir() && strings.HasPrefix(entry.Name(), ".flipai-update-") && strings.HasSuffix(entry.Name(), ".part") {
\t\t\t\t_ = os.Remove(filepath.Join(dir, entry.Name()))
\t\t\t}
\t\t}
\t}
\tsum, err := downloadWithProgress(ctx, info.AssetURL, dest, progress)''')

old_download = '''// download saves a trusted URL atomically and returns the file's SHA-256. A
// partial network transfer is never left behind under an executable filename.
func download(ctx context.Context, rawURL, dest string) (string, error) {
\tif !trustedUpdateURL(rawURL) {
\t\treturn "", errors.New("download URL is not a trusted GitHub endpoint")
\t}
\treq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
\tif err != nil {
\t\treturn "", err
\t}
\treq.Header.Set("User-Agent", "FlipAi/"+version)
\tresp, err := updateHTTPClient(10 * time.Minute).Do(req)
\tif err != nil {
\t\treturn "", err
\t}
\tdefer resp.Body.Close()
\tif resp.StatusCode != http.StatusOK {
\t\treturn "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
\t}
\tif resp.ContentLength > maxUpdateBytes {
\t\treturn "", errors.New("update installer is larger than the allowed size")
\t}
\tif err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
\t\treturn "", err
\t}
\tf, err := os.CreateTemp(filepath.Dir(dest), ".flipai-update-*.part")
\tif err != nil {
\t\treturn "", err
\t}
\ttmp := f.Name()
\tdefer os.Remove(tmp)
\tif err := f.Chmod(0o600); err != nil {
\t\t_ = f.Close()
\t\treturn "", err
\t}
\th := sha256.New()
\tn, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxUpdateBytes+1))
\tif copyErr == nil && n > maxUpdateBytes {
\t\tcopyErr = errors.New("update installer is larger than the allowed size")
\t}
\tif copyErr == nil {
\t\tcopyErr = f.Sync()
\t}
\tcloseErr := f.Close()
\tif copyErr != nil {
\t\treturn "", copyErr
\t}
\tif closeErr != nil {
\t\treturn "", closeErr
\t}
\t_ = os.Remove(dest)
\tif err := os.Rename(tmp, dest); err != nil {
\t\treturn "", err
\t}
\treturn hex.EncodeToString(h.Sum(nil)), nil
}'''
new_download = '''// download saves a trusted URL atomically and returns the file's SHA-256. A
// partial network transfer is never left behind under an executable filename.
func download(ctx context.Context, rawURL, dest string) (string, error) {
\treturn downloadWithProgress(ctx, rawURL, dest, nil)
}

type updateProgressReader struct {
\tr      io.Reader
\tdone   int64
\ttotal  int64
\treport func(done, total int64)
}

func (p *updateProgressReader) Read(buf []byte) (int, error) {
\tn, err := p.r.Read(buf)
\tif n > 0 {
\t\tp.done += int64(n)
\t\tif p.report != nil {
\t\t\tp.report(p.done, p.total)
\t\t}
\t}
\treturn n, err
}

func downloadWithProgress(ctx context.Context, rawURL, dest string, progress func(done, total int64)) (string, error) {
\tif !trustedUpdateURL(rawURL) {
\t\treturn "", errors.New("download URL is not a trusted GitHub endpoint")
\t}
\treq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
\tif err != nil {
\t\treturn "", err
\t}
\treq.Header.Set("User-Agent", "FlipAi/"+version)
\tresp, err := updateHTTPClient(10 * time.Minute).Do(req)
\tif err != nil {
\t\treturn "", err
\t}
\tdefer resp.Body.Close()
\tif resp.StatusCode != http.StatusOK {
\t\treturn "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
\t}
\tif resp.ContentLength > maxUpdateBytes {
\t\treturn "", errors.New("update installer is larger than the allowed size")
\t}
\tif err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
\t\treturn "", err
\t}
\tf, err := os.CreateTemp(filepath.Dir(dest), ".flipai-update-*.part")
\tif err != nil {
\t\treturn "", err
\t}
\ttmp := f.Name()
\tdefer os.Remove(tmp)
\tif err := f.Chmod(0o600); err != nil {
\t\t_ = f.Close()
\t\treturn "", err
\t}
\tif progress != nil {
\t\tprogress(0, resp.ContentLength)
\t}
\th := sha256.New()
\treader := &updateProgressReader{
\t\tr:      io.LimitReader(resp.Body, maxUpdateBytes+1),
\t\ttotal:  resp.ContentLength,
\t\treport: progress,
\t}
\tn, copyErr := io.Copy(io.MultiWriter(f, h), reader)
\tif copyErr == nil && n > maxUpdateBytes {
\t\tcopyErr = errors.New("update installer is larger than the allowed size")
\t}
\tif copyErr == nil {
\t\tcopyErr = f.Sync()
\t}
\tcloseErr := f.Close()
\tif copyErr != nil {
\t\treturn "", copyErr
\t}
\tif closeErr != nil {
\t\treturn "", closeErr
\t}
\t_ = os.Remove(dest)
\tif err := os.Rename(tmp, dest); err != nil {
\t\treturn "", err
\t}
\treturn hex.EncodeToString(h.Sum(nil)), nil
}'''
replace_once('update.go', old_download, new_download)

# ---------------------------------------------------------------------------
# Startup: a completed staged update installs on an actual app/watchdog restart.
# Both manual and restart-triggered installs use restartapp=1, which makes the
# installer restore the background bridge and reopen the FlipAi window.
# ---------------------------------------------------------------------------
write('update_startup.go', '''package main

import "log"

// startStagedUpdateInstaller is replaceable in tests so restart behavior can be
// verified without launching a Windows Setup EXE.
var startStagedUpdateInstaller = runUpdateInstaller

// installStagedUpdateOnStartup runs only for a real app launch or the watchdog
// Windows starts after sign-in. Helper/browser/host subprocesses must never
// independently start an installer.
func installStagedUpdateOnStartup(mode, statePath string) bool {
\tif mode != "ui" && mode != "--watchdog" {
\t\treturn false
\t}
\tinfo := loadUpdateState(statePath)
\tif !info.Ready() {
\t\treturn false
\t}
\tif err := startStagedUpdateInstaller(info.DownloadedPath, true); err != nil {
\t\tlog.Printf("staged FlipAi %s update could not start after restart: %v", info.Version, err)
\t\tactivityLogForStatePath(statePath).Add("error", "host", "Staged update could not start after restart: "+truncate(err.Error(), 200), "", "", "")
\t\treturn false
\t}
\tactivityLogForStatePath(statePath).Add("info", "host", "Installing staged FlipAi "+info.Version+" after restart", "", "", "")
\treturn true
}
''')

replace_once('main.go', '''\tif len(os.Args) > 1 {
\t\tmode = os.Args[1]
\t}
\tswitch mode {''', '''\tif len(os.Args) > 1 {
\t\tmode = os.Args[1]
\t}
\t// A verified update waits quietly until the user chooses Install. If they
\t// restart FlipAi or Windows first, that restart is the choice point: launch
\t// the staged installer and let Setup reopen FlipAi on the new version.
\tif installStagedUpdateOnStartup(mode, statePath) {
\t\treturn
\t}
\tswitch mode {''')

# ---------------------------------------------------------------------------
# Local status endpoint used by the tiny sidebar progress control.
# ---------------------------------------------------------------------------
replace_once('webui.go', '''\tm.HandleFunc("/status.json", a.requireAuth(a.statusJSON))
\tm.HandleFunc("/activity.json", a.requireAuth(a.activityJSON))''', '''\tm.HandleFunc("/status.json", a.requireAuth(a.statusJSON))
\tm.HandleFunc("/update/status.json", a.requireAuth(a.updateStatusJSON))
\tm.HandleFunc("/activity.json", a.requireAuth(a.activityJSON))''')

# Insert endpoint immediately before updateCheck.
replace_once('ui_actions.go', '''// ---------------------------------------------------------------------------
// Updates
// ---------------------------------------------------------------------------

func (a *App) updateCheck''', '''// ---------------------------------------------------------------------------
// Updates
// ---------------------------------------------------------------------------

// updateStatusJSON returns only the small amount of state needed by the sidebar
// control. Local file paths and checksum details never leave the host process.
func (a *App) updateStatusJSON(w http.ResponseWriter, r *http.Request) {
\tinfo := loadUpdateState(a.statePath)
\tstate := "idle"
\tpercent := info.DownloadPercent
\tif info.Newer() {
\t\tswitch {
\t\tcase info.Ready():
\t\t\tstate = "ready"
\t\t\tpercent = 100
\t\tcase info.Downloading:
\t\t\tstate = "downloading"
\t\tcase info.Error != "":
\t\t\tstate = "error"
\t\tdefault:
\t\t\tstate = "waiting"
\t\t}
\t}
\tif percent < 0 {
\t\tpercent = 0
\t}
\tif percent > 100 {
\t\tpercent = 100
\t}
\twriteJSON(w, map[string]any{
\t\t"currentVersion": version,
\t\t"updateVersion":  info.Version,
\t\t"newer":          info.Newer(),
\t\t"state":          state,
\t\t"downloading":    info.Downloading,
\t\t"ready":          info.Ready(),
\t\t"percent":        percent,
\t\t"bytes":          info.DownloadBytes,
\t\t"total":          info.DownloadTotal,
\t})
}

func (a *App) updateCheck''')

# ---------------------------------------------------------------------------
# Sidebar-only UI. Do not touch the rest of the app shell.
# ---------------------------------------------------------------------------
write('zzzzzzzz_quiet_updater_ui.go', r'''package main

import (
	"html/template"
	"strings"
)

func updaterUIState(releaseVersion string) string {
	info := currentUpdateSnapshot()
	if releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
		return ""
	}
	if info.Ready() {
		return "ready"
	}
	if info.Downloading {
		return "downloading"
	}
	if info.Error != "" {
		return "error"
	}
	return "waiting"
}

func updaterUIPercent(releaseVersion string) int {
	info := currentUpdateSnapshot()
	if releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
		return 0
	}
	if info.Ready() {
		return 100
	}
	if info.DownloadPercent < 0 {
		return 0
	}
	if info.DownloadPercent > 99 {
		return 99
	}
	return info.DownloadPercent
}

func init() {
	// Settings no longer owns updates. Keep only startup/calling controls. The
	// old handlers remain for compatibility with an already-open older window.
	settings := cleanSettingsHTML
	settings = strings.Replace(settings,
		`<div><h1>Settings</h1><p>Keep FlipAi running and manage app updates and calling.</p></div>`,
		`<div><h1>Settings</h1><p>Keep FlipAi running and manage calling.</p></div>`, 1)
	if start := strings.Index(settings, `<section class="card settings-compact-card">`); start >= 0 {
		if relEnd := strings.Index(settings[start:], `<section class="card settings-startup-card">`); relEnd >= 0 {
			settings = settings[:start] + settings[start+relEnd:]
		}
	}
	settings = strings.Replace(settings, " Check for updates", "", 1)
	registerPage("settings", settings)

	// Only replace the version row at the bottom of the existing sidebar. No
	// navigation, page layout, branding or unrelated app UI is changed.
	updatedShell := shellHTML
	oldSidebar := `      {{if .Shell.UpdateVersion}}<a class="side-update" href="/settings#updates" title="FlipAi {{.Shell.UpdateVersion}} is available">{{icon "download"}}<span>v{{.Shell.Version}} &rarr; {{.Shell.UpdateVersion}}</span></a>{{else}}<span>v{{.Shell.Version}}</span>{{end}}`
	newSidebar := `      <div class="side-version-row" id="flipai-version-row" data-current-version="{{.Shell.Version}}">
        <span class="side-version">v{{.Shell.Version}}</span>
        {{if .Shell.UpdateVersion}}
          {{$updateState := updaterState .Shell.UpdateVersion}}
          {{$updatePercent := updaterPercent .Shell.UpdateVersion}}
          {{if eq $updateState "ready"}}
            <button class="side-update-control side-update-ready" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" title="Install FlipAi {{.Shell.UpdateVersion}}">Install update</button>
          {{else}}
            <span class="side-update-control side-update-progress" title="Downloading FlipAi {{.Shell.UpdateVersion}}">
              <span class="side-update-ring" style="--flipai-progress:{{$updatePercent}}%" aria-hidden="true"></span>
              <span class="side-update-percent">{{$updatePercent}}%</span>
            </span>
          {{end}}
        {{end}}
      </div>`
	updatedShell = strings.Replace(updatedShell, oldSidebar, newSidebar, 1)

	// The retired page-wide banner is intentionally gone; update status belongs
	// only beside the version.
	bannerStart := `    {{if .Shell.UpdateVersion}}
    <div class="banner update">`
	bannerEnd := `    {{end}}
    {{template "content" .}}`
	if start := strings.Index(updatedShell, bannerStart); start >= 0 {
		if relEnd := strings.Index(updatedShell[start:], bannerEnd); relEnd >= 0 {
			updatedShell = updatedShell[:start] + `    {{template "content" .}}` + updatedShell[start+relEnd+len(bannerEnd):]
		}
	}

	const updaterStyle = `<style>
.side-version-row{display:flex;align-items:center;gap:8px;min-height:30px;max-width:100%}.side-version{white-space:nowrap;flex:0 0 auto}.side-update-control{flex:0 0 auto}.side-update-progress{display:inline-flex;align-items:center;gap:5px;height:26px;color:var(--muted);font-size:12px;font-weight:700;white-space:nowrap}.side-update-ring{--flipai-progress:0%;width:20px;height:20px;border-radius:50%;display:inline-block;position:relative;background:conic-gradient(var(--accent) var(--flipai-progress),var(--line) 0)}.side-update-ring:after{content:"";position:absolute;inset:4px;border-radius:50%;background:var(--sidebar-bg,var(--surface,#fff))}.side-update-percent{min-width:29px;text-align:right;font-variant-numeric:tabular-nums}.side-update-ready{min-height:28px;border:0;border-radius:8px;padding:5px 9px;color:#fff;background:var(--accent);font:700 11px/1.1 inherit;cursor:pointer;white-space:nowrap;box-shadow:none}.side-update-ready:before{content:"↓";font-size:13px;margin-right:5px}.side-update-ready:hover{filter:brightness(.97)}.side-update-ready:disabled{cursor:default;opacity:.68}.side-update-ready.is-installing:before{content:"";display:inline-block;width:10px;height:10px;border:2px solid rgba(255,255,255,.45);border-top-color:#fff;border-radius:50%;animation:flipaiUpdateSpin .8s linear infinite;vertical-align:-1px}@keyframes flipaiUpdateSpin{to{transform:rotate(360deg)}}
</style>`
	updatedShell = strings.Replace(updatedShell, `</head>`, updaterStyle+`</head>`, 1)

	const updaterScript = `<script>
(() => {
  const row = () => document.getElementById('flipai-version-row');
  const clamp = (v) => Math.max(0, Math.min(100, Number.isFinite(Number(v)) ? Math.round(Number(v)) : 0));

  function progressControl(r, status) {
    let control = r.querySelector('.side-update-control');
    if (!control || !control.classList.contains('side-update-progress')) {
      if (control) control.remove();
      control = document.createElement('span');
      control.className = 'side-update-control side-update-progress';
      const ring = document.createElement('span');
      ring.className = 'side-update-ring';
      ring.setAttribute('aria-hidden', 'true');
      const pct = document.createElement('span');
      pct.className = 'side-update-percent';
      control.append(ring, pct);
      r.append(control);
    }
    const p = Math.min(99, clamp(status.percent));
    control.title = status.state === 'error'
      ? 'Update download will retry automatically'
      : 'Downloading FlipAi ' + (status.updateVersion || 'update');
    control.querySelector('.side-update-ring').style.setProperty('--flipai-progress', p + '%');
    control.querySelector('.side-update-percent').textContent = p + '%';
  }

  function readyControl(r, status) {
    let button = r.querySelector('#flipai-update-install');
    const existing = r.querySelector('.side-update-control');
    if (!button) {
      if (existing) existing.remove();
      button = document.createElement('button');
      button.type = 'button';
      button.id = 'flipai-update-install';
      button.className = 'side-update-control side-update-ready';
      button.textContent = 'Install update';
      r.append(button);
    }
    button.dataset.version = status.updateVersion || '';
    button.title = 'Install FlipAi ' + (status.updateVersion || 'update');
    bindInstall(button);
  }

  function bindInstall(button) {
    if (!button || button.dataset.bound === '1') return;
    button.dataset.bound = '1';
    button.addEventListener('click', async () => {
      if (button.disabled) return;
      button.disabled = true;
      button.classList.add('is-installing');
      button.textContent = 'Installing…';
      try {
        const response = await fetch('/update/install', {
          method:'POST', credentials:'same-origin', cache:'no-store',
          headers:{'X-FlipAi-Inline':'1'}
        });
        if (!response.ok) throw new Error('install failed');
      } catch (_) {
        button.disabled = false;
        button.classList.remove('is-installing');
        button.textContent = 'Install update';
      }
    });
  }

  function render(status) {
    const r = row();
    if (!r) return;
    if (!status || !status.newer) {
      const control = r.querySelector('.side-update-control');
      if (control) control.remove();
      return;
    }
    if (status.ready || status.state === 'ready') readyControl(r, status);
    else progressControl(r, status);
  }

  async function refresh() {
    try {
      const response = await fetch('/update/status.json', {credentials:'same-origin', cache:'no-store'});
      if (!response.ok) return;
      render(await response.json());
    } catch (_) {}
  }

  bindInstall(document.getElementById('flipai-update-install'));
  refresh();
  window.setInterval(refresh, 1000);
})();
</script>`
	updatedShell = strings.Replace(updatedShell, `</body>`, updaterScript+`</body>`, 1)

	for _, page := range uiPages {
		page.Funcs(template.FuncMap{"updaterState": updaterUIState, "updaterPercent": updaterUIPercent})
		if _, err := page.Parse(updatedShell); err != nil {
			panic(err)
		}
	}
}
''')

# ---------------------------------------------------------------------------
# Tests for the new exact behavior.
# ---------------------------------------------------------------------------
write('update_progress_test.go', r'''package main

import (
	"context"
	"encoding/json"
	"errors"
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
''')

# Adjust existing updater UI regressions to assert the percentage/button text.
replace_once('quiet_updater_test.go', '''\t\tDownloading: true,
\t}''', '''\t\tDownloading:     true,
\t\tDownloadBytes:   45,
\t\tDownloadTotal:   100,
\t\tDownloadPercent: 45,
\t}''')
replace_once('quiet_updater_test.go', '''\tif strings.Contains(body, `id="flipai-update-install"`) {
\t\tt.Fatal("install button appeared before the installer was staged")
\t}''', '''\tif strings.Contains(body, `id="flipai-update-install"`) {
\t\tt.Fatal("install button appeared before the installer was staged")
\t}
\tif !strings.Contains(body, `side-update-ring`) || !strings.Contains(body, `45%`) {
\t\tt.Fatal("downloading update should show the compact progress ring and percentage")
\t}''')
replace_once('quiet_updater_test.go', '''\tif !strings.Contains(body, `id="flipai-update-install"`) || !strings.Contains(body, "side-update-ready") {
\t\tt.Fatal("staged update should become the compact install button")
\t}''', '''\tif !strings.Contains(body, `id="flipai-update-install"`) || !strings.Contains(body, "side-update-ready") || !strings.Contains(body, "Install update") {
\t\tt.Fatal("staged update should become the compact Install update button")
\t}''')

# Keep old config semantics tests accurate: the removed Settings flag does not
# control the new deterministic install-on-restart behavior.
text = read('quiet_updater_test.go').replace('installation must never become automatic', 'retired saved automatic-install flag must stay disabled')
write('quiet_updater_test.go', text)
text = read('update_behavior_test.go')
text = text.replace('// Update checks are automatic app behavior now; installation is never enabled\n// unattended by a saved legacy flag.', '// Update checks are automatic app behavior now. The retired saved flag never\n// enables timer-based installation; the separate staged-update restart path is deterministic.')
text = text.replace('// Keep the busy-state protection even though unattended installation is off;', '// Keep the busy-state protection for maintenance paths;')
write('update_behavior_test.go', text)

# ---------------------------------------------------------------------------
# Release metadata. Merging this PR will publish v0.46.53 through the existing
# release workflow, containing all prior main changes plus this updater work.
# ---------------------------------------------------------------------------
replace_once('config.go', 'const version = "0.46.52"', 'const version = "0.46.53"')
write('VERSION', '0.46.53\n')
replace_once('installer/FlipAi.iss', '#define MyVersion "0.46.52"', '#define MyVersion "0.46.53"')
write('docs/RELEASE-NOTES.md', '''# FlipAi v0.46.53

FlipAi now has a compact, persistent update flow in the sidebar instead of a separate update screen or popup.

## What changed

- A newly published update starts downloading automatically in the background as soon as FlipAi discovers it.
- The version area shows a small circular download indicator with the live percentage while the installer is transferring.
- 100% is shown only after the installer has finished downloading and passed its published SHA-256 verification; the progress control then becomes a compact **Install update** button.
- The user can leave the verified update staged and choose when to press Install.
- If FlipAi or Windows is restarted while a verified update is waiting, FlipAi installs that staged update automatically on restart and reopens the FlipAi application afterward.
- Download state and percentage are persisted locally, failed downloads retry quietly, and incomplete `.part` files are never treated as installers.
- The update section remains removed from Settings and there are no update popups or unrelated interface changes.
- All Google Voice media, startup, Copilot, and exact AI-mode routing improvements from the previous releases remain included.

## Validation

The release pipeline runs the full Linux and Windows Go test suites, real-browser flow tests, vet and race tests, the Windows x64 build, Google Voice background smoke tests, Microsoft Defender scans, installer install/uninstall smoke tests, SBOM generation, checksums, and build provenance before publishing the release.

No Authenticode/code-signing certificate is included in this release.
''')

print('Updater implementation applied.')
