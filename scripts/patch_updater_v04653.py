from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    s = p.read_text(encoding="utf-8")
    if old not in s:
        raise SystemExit(f"expected text not found in {path}: {old[:120]!r}")
    p.write_text(s.replace(old, new, 1), encoding="utf-8")


# ---------------------------------------------------------------------------
# update.go: persist progress, report it to the UI, and resume .part downloads.
# ---------------------------------------------------------------------------
replace_once(
    "update.go",
    '''\tDownloading      bool      `json:"downloading,omitempty"`\n\tDownloadedPath   string    `json:"downloadedPath,omitempty"`''',
    '''\tDownloading      bool      `json:"downloading,omitempty"`\n\tDownloadedBytes  int64     `json:"downloadedBytes,omitempty"`\n\tTotalBytes       int64     `json:"totalBytes,omitempty"`\n\tDownloadedPath   string    `json:"downloadedPath,omitempty"`''',
)

replace_once(
    "update.go",
    '''func (r ReleaseInfo) Ready() bool {\n\tif !r.Newer() || r.DownloadedPath == "" {\n\t\treturn false\n\t}\n\tst, err := os.Stat(r.DownloadedPath)\n\treturn err == nil && !st.IsDir() && st.Size() > 0\n}\n''',
    '''func (r ReleaseInfo) Ready() bool {\n\tif !r.Newer() || r.DownloadedPath == "" {\n\t\treturn false\n\t}\n\tst, err := os.Stat(r.DownloadedPath)\n\treturn err == nil && !st.IsDir() && st.Size() > 0\n}\n\n// ProgressPercent is the small percentage shown beside FlipAi's version while\n// an update is being staged. A verified installer is always 100%; a resumable\n// partial download uses its persisted byte counts so restarting FlipAi does not\n// make the indicator jump back to zero.\nfunc (r ReleaseInfo) ProgressPercent() int {\n\tif r.Ready() {\n\t\treturn 100\n\t}\n\tif r.TotalBytes <= 0 || r.DownloadedBytes <= 0 {\n\t\treturn 0\n\t}\n\tp := int((r.DownloadedBytes * 100) / r.TotalBytes)\n\tif p < 0 {\n\t\treturn 0\n\t}\n\tif p > 100 {\n\t\treturn 100\n\t}\n\treturn p\n}\n''',
)

replace_once(
    "update.go",
    '''\tif info.Version == current.Version && info.AssetURL == current.AssetURL && current.Ready() {\n\t\tinfo.DownloadedPath = current.DownloadedPath\n\t\tinfo.DownloadedSHA256 = current.DownloadedSHA256\n\t\tinfo.DownloadedAt = current.DownloadedAt\n\t}\n''',
    '''\tif info.Version == current.Version && info.AssetURL == current.AssetURL {\n\t\tinfo.DownloadedBytes = current.DownloadedBytes\n\t\tinfo.TotalBytes = current.TotalBytes\n\t\tif current.Ready() {\n\t\t\tinfo.DownloadedPath = current.DownloadedPath\n\t\t\tinfo.DownloadedSHA256 = current.DownloadedSHA256\n\t\t\tinfo.DownloadedAt = current.DownloadedAt\n\t\t}\n\t}\n''',
)

replace_once(
    "update.go",
    '''\tdlCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)\n\tdefer cancel()\n\tpath, err := downloadUpdate(dlCtx, current)\n''',
    '''\tdlCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)\n\tdefer cancel()\n\tlastPercent := -1\n\tlastSaved := time.Time{}\n\tprogress := func(done, total int64) {\n\t\tp := 0\n\t\tif total > 0 {\n\t\t\tp = int((done * 100) / total)\n\t\t\tif p > 100 {\n\t\t\t\tp = 100\n\t\t\t}\n\t\t}\n\t\tif p == lastPercent && time.Since(lastSaved) < 500*time.Millisecond {\n\t\t\treturn\n\t\t}\n\t\tlatest := loadUpdateState(a.statePath)\n\t\tif latest.Version != current.Version || latest.AssetURL != current.AssetURL {\n\t\t\treturn\n\t\t}\n\t\tlatest.Downloading = true\n\t\tlatest.DownloadedBytes = done\n\t\tlatest.TotalBytes = total\n\t\tlatest.Error = ""\n\t\tsaveUpdateState(a.statePath, latest)\n\t\tlastPercent = p\n\t\tlastSaved = time.Now()\n\t}\n\tpath, err := downloadUpdateWithProgress(dlCtx, current, progress)\n''',
)

replace_once(
    "update.go",
    '''\tlatest.Error = ""\n\tlatest.DownloadedPath = path\n\tlatest.DownloadedSHA256 = sum\n\tlatest.DownloadedAt = time.Now()\n\tsaveUpdateState(a.statePath, latest)\n''',
    '''\tlatest.Error = ""\n\tlatest.DownloadedPath = path\n\tlatest.DownloadedSHA256 = sum\n\tlatest.DownloadedAt = time.Now()\n\tif st, statErr := os.Stat(path); statErr == nil {\n\t\tlatest.DownloadedBytes = st.Size()\n\t\tlatest.TotalBytes = st.Size()\n\t}\n\tsaveUpdateState(a.statePath, latest)\n''',
)

replace_once(
    "update.go",
    '''func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {\n\tupdateDownloadMu.Lock()''',
    '''func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {\n\treturn downloadUpdateWithProgress(ctx, info, nil)\n}\n\nfunc downloadUpdateWithProgress(ctx context.Context, info ReleaseInfo, progress func(done, total int64)) (string, error) {\n\tupdateDownloadMu.Lock()''',
)

replace_once(
    "update.go",
    '''\tif sum, err := sha256File(dest); err == nil && strings.EqualFold(sum, want) {\n\t\treturn dest, nil\n\t}\n\t_ = os.Remove(dest)\n\tsum, err := download(ctx, info.AssetURL, dest)''',
    '''\tif sum, err := sha256File(dest); err == nil && strings.EqualFold(sum, want) {\n\t\tif st, statErr := os.Stat(dest); statErr == nil && progress != nil {\n\t\t\tprogress(st.Size(), st.Size())\n\t\t}\n\t\treturn dest, nil\n\t}\n\t_ = os.Remove(dest)\n\tsum, err := downloadWithProgress(ctx, info.AssetURL, dest, progress)''',
)

p = Path("update.go")
s = p.read_text(encoding="utf-8")
marker = '''// download saves a trusted URL atomically and returns the file's SHA-256. A\n// partial network transfer is never left behind under an executable filename.\n'''
start = s.find(marker)
if start < 0:
    raise SystemExit("download tail marker missing in update.go")
new_tail = r'''// download saves a trusted URL atomically and returns the file's SHA-256.
// The .part file is deterministic and resumable: if FlipAi or Windows restarts
// mid-download, the next staging pass continues with an HTTP Range request
// rather than discarding the bytes already received. The executable filename
// is created only after the transfer is complete.
func download(ctx context.Context, rawURL, dest string) (string, error) {
	return downloadWithProgress(ctx, rawURL, dest, nil)
}

func updateResponseTotal(resp *http.Response, offset int64) int64 {
	if resp == nil {
		return 0
	}
	if raw := strings.TrimSpace(resp.Header.Get("Content-Range")); raw != "" {
		if slash := strings.LastIndex(raw, "/"); slash >= 0 && slash+1 < len(raw) {
			if total, err := strconv.ParseInt(strings.TrimSpace(raw[slash+1:]), 10, 64); err == nil && total >= 0 {
				return total
			}
		}
	}
	if resp.ContentLength >= 0 {
		return offset + resp.ContentLength
	}
	return 0
}

func downloadWithProgress(ctx context.Context, rawURL, dest string, progress func(done, total int64)) (string, error) {
	if !trustedUpdateURL(rawURL) {
		return "", errors.New("download URL is not a trusted GitHub endpoint")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	part := dest + ".part"
	offset := int64(0)
	if st, err := os.Stat(part); err == nil && !st.IsDir() {
		offset = st.Size()
		if offset < 0 || offset > maxUpdateBytes {
			_ = os.Remove(part)
			offset = 0
		}
	}

	request := func(start int64) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "FlipAi/"+version)
		if start > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
		}
		return updateHTTPClient(10 * time.Minute).Do(req)
	}

	resp, err := request(offset)
	if err != nil {
		return "", err
	}
	if offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		_ = resp.Body.Close()
		_ = os.Remove(part)
		offset = 0
		resp, err = request(0)
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	if offset > 0 && resp.StatusCode == http.StatusOK {
		// The endpoint ignored Range. Restart safely instead of appending a full
		// response to an existing partial installer.
		offset = 0
		_ = os.Remove(part)
	} else if offset > 0 && resp.StatusCode != http.StatusPartialContent {
		return "", fmt.Errorf("resumed download returned HTTP %d", resp.StatusCode)
	} else if offset == 0 && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	total := updateResponseTotal(resp, offset)
	if total > maxUpdateBytes || (resp.ContentLength > maxUpdateBytes && offset == 0) {
		return "", errors.New("update installer is larger than the allowed size")
	}
	if progress != nil {
		progress(offset, total)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return "", err
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			_ = f.Close()
			return "", err
		}
	}

	done := offset
	buf := make([]byte, 128*1024)
	var copyErr error
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if done+int64(n) > maxUpdateBytes {
				copyErr = errors.New("update installer is larger than the allowed size")
				break
			}
			written, writeErr := f.Write(buf[:n])
			done += int64(written)
			if progress != nil {
				progress(done, total)
			}
			if writeErr != nil {
				copyErr = writeErr
				break
			}
			if written != n {
				copyErr = io.ErrShortWrite
				break
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			copyErr = readErr
			break
		}
	}
	if copyErr == nil && total > 0 && done != total {
		copyErr = fmt.Errorf("update download ended at %d of %d bytes", done, total)
	}
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}

	sum, err := sha256File(part)
	if err != nil {
		return "", err
	}
	_ = os.Remove(dest)
	if err := os.Rename(part, dest); err != nil {
		return "", err
	}
	if progress != nil {
		if total <= 0 {
			total = done
		}
		progress(done, total)
	}
	return sum, nil
}
'''
p.write_text(s[:start] + new_tail, encoding="utf-8")

# ---------------------------------------------------------------------------
# Local JSON status endpoint for the sidebar. It never exposes the asset URL.
# ---------------------------------------------------------------------------
Path("update_status.go").write_text(r'''package main

import "net/http"

func (a *App) updateStatusJSON(w http.ResponseWriter, r *http.Request) {
	info := loadUpdateState(a.statePath)
	available := info.Newer()
	writeJSON(w, map[string]any{
		"currentVersion": version,
		"available":      available,
		"targetVersion":  info.Version,
		"downloading":    available && info.Downloading,
		"ready":          available && info.Ready(),
		"percent":        info.ProgressPercent(),
		"downloadedBytes": info.DownloadedBytes,
		"totalBytes":      info.TotalBytes,
	})
}
''', encoding="utf-8")

replace_once(
    "webui.go",
    '''\tm.HandleFunc("/folders.json", a.requireAuth(a.foldersJSON))\n\tm.HandleFunc("/chatgpt/status.json", a.requireAuth(a.chatGPTStatusJSON))''',
    '''\tm.HandleFunc("/folders.json", a.requireAuth(a.foldersJSON))\n\tm.HandleFunc("/update/status.json", a.requireAuth(a.updateStatusJSON))\n\tm.HandleFunc("/chatgpt/status.json", a.requireAuth(a.chatGPTStatusJSON))''',
)

# ---------------------------------------------------------------------------
# A completed verified update installs automatically on the next app/PC start.
# The installer is always asked to reopen the FlipAi window afterwards.
# ---------------------------------------------------------------------------
Path("update_startup.go").write_text(r'''package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

func updateInstallingFlag(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "update-installing.flag")
}

func clearBrokenStagedUpdate(statePath string, info ReleaseInfo, detail string) {
	if info.DownloadedPath != "" {
		_ = os.Remove(info.DownloadedPath)
	}
	info.Downloading = false
	info.DownloadedPath = ""
	info.DownloadedSHA256 = ""
	info.DownloadedAt = time.Time{}
	info.DownloadedBytes = 0
	info.TotalBytes = 0
	info.Error = truncate(detail, 200)
	saveUpdateState(statePath, info)
}

// maybeInstallStagedUpdateAtStartup is called only at a real app/watchdog
// startup. Merely reaching 100% while FlipAi is running does not interrupt the
// user: the sidebar becomes an Install update button. If the app or computer is
// restarted first, this consumes the already-verified installer automatically.
func maybeInstallStagedUpdateAtStartup(statePath string) bool {
	info := loadUpdateState(statePath)
	flag := updateInstallingFlag(statePath)
	if !info.Newer() {
		_ = os.Remove(flag)
		return false
	}
	if !info.Ready() {
		return false
	}
	if strings.TrimSpace(info.DownloadedSHA256) == "" {
		clearBrokenStagedUpdate(statePath, info, "staged update checksum is missing; downloading it again")
		return false
	}
	sum, err := sha256File(info.DownloadedPath)
	if err != nil || !strings.EqualFold(sum, info.DownloadedSHA256) {
		clearBrokenStagedUpdate(statePath, info, "staged update no longer matches its verified checksum; downloading it again")
		return false
	}

	// A launcher and the Windows sign-in watchdog can overlap. This tiny flag
	// prevents both from starting the same installer. A stale flag is recoverable.
	tryFlag := func() (*os.File, error) {
		return os.OpenFile(flag, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	}
	lock, lockErr := tryFlag()
	if os.IsExist(lockErr) {
		if st, statErr := os.Stat(flag); statErr == nil && time.Since(st.ModTime()) > 10*time.Minute {
			_ = os.Remove(flag)
			lock, lockErr = tryFlag()
		} else {
			// Another startup already handed off to Setup. Do not start the old
			// build underneath it.
			return true
		}
	}
	if lockErr != nil {
		return false
	}
	_, _ = lock.WriteString(info.Version + "\n")
	_ = lock.Close()

	if err := runUpdateInstaller(info.DownloadedPath, true); err != nil {
		_ = os.Remove(flag)
		info.Error = truncate("could not start staged update: "+err.Error(), 200)
		saveUpdateState(statePath, info)
		return false
	}
	activityLogForStatePath(statePath).Add("info", "host", "Installing staged FlipAi "+info.Version+" after restart", "", "", "")
	return true
}
''', encoding="utf-8")

replace_once(
    "main.go",
    '''func runLauncher(dataDir, cfgPath string) {\n\tcfg := loadOrCreateConfig(cfgPath, dataDir)\n\t_ = os.Remove(filepath.Join(dataDir, "quit.flag"))''',
    '''func runLauncher(dataDir, cfgPath string) {\n\tcfg := loadOrCreateConfig(cfgPath, dataDir)\n\tif maybeInstallStagedUpdateAtStartup(filepath.Join(dataDir, "state.json")) {\n\t\treturn\n\t}\n\t_ = os.Remove(filepath.Join(dataDir, "quit.flag"))''',
)

replace_once(
    "main.go",
    '''\tdefer release()\n\n\tcfg := loadOrCreateConfig(cfgPath, dataDir)\n\t// The quit flag is deliberately NOT cleared here.''',
    '''\tdefer release()\n\n\tcfg := loadOrCreateConfig(cfgPath, dataDir)\n\tif maybeInstallStagedUpdateAtStartup(filepath.Join(dataDir, "state.json")) {\n\t\treturn\n\t}\n\t// The quit flag is deliberately NOT cleared here.''',
)

# ---------------------------------------------------------------------------
# Sidebar UI: only the updater area changes. No page banner or Settings card.
# ---------------------------------------------------------------------------
Path("zzzzzzzz_quiet_updater_ui.go").write_text(r'''package main

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
	return "waiting"
}

func updaterUIPercent(releaseVersion string) int {
	info := currentUpdateSnapshot()
	if releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
		return 0
	}
	return info.ProgressPercent()
}

func init() {
	// Updates live only beside the version in the sidebar. Keep Settings focused
	// on startup/calling and remove the old page-wide updater surfaces.
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

	updatedShell := shellHTML
	oldSidebar := `      {{if .Shell.UpdateVersion}}<a class="side-update" href="/settings#updates" title="FlipAi {{.Shell.UpdateVersion}} is available">{{icon "download"}}<span>v{{.Shell.Version}} &rarr; {{.Shell.UpdateVersion}}</span></a>{{else}}<span>v{{.Shell.Version}}</span>{{end}}`
	newSidebar := `      <div class="side-version-row" id="flipai-version-row">
        <span class="side-version">v{{.Shell.Version}}</span>
        <span class="side-update-control" id="flipai-update-control">
        {{if .Shell.UpdateVersion}}
          {{$updateState := updaterState .Shell.UpdateVersion}}
          {{$updatePercent := updaterPercent .Shell.UpdateVersion}}
          {{if eq $updateState "ready"}}
            <button class="side-update side-update-ready" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" title="Install FlipAi {{.Shell.UpdateVersion}}">Install update</button>
          {{else}}
            <span class="side-update-progress" title="Downloading FlipAi {{.Shell.UpdateVersion}}"><span class="side-update-ring" style="--update-percent:{{$updatePercent}}"></span><span class="side-update-percent">{{$updatePercent}}%</span></span>
          {{end}}
        {{end}}
        </span>
      </div>`
	updatedShell = strings.Replace(updatedShell, oldSidebar, newSidebar, 1)

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
.side-version-row{display:flex;align-items:center;justify-content:space-between;gap:8px;min-height:30px;width:100%}.side-version{white-space:nowrap}.side-update-control{display:flex;align-items:center;margin-left:auto}.side-update-progress{display:inline-flex;align-items:center;gap:5px;white-space:nowrap;color:var(--muted);font-size:12px;font-weight:650}.side-update-ring{--update-percent:0;width:20px;height:20px;border-radius:50%;display:inline-block;position:relative;background:conic-gradient(var(--accent) calc(var(--update-percent) * 1%),var(--line) 0)}.side-update-ring:after{content:"";position:absolute;inset:4px;border-radius:50%;background:var(--surface)}.side-update{border:0;font:inherit}.side-update-ready{min-height:28px;padding:5px 9px;border-radius:8px;background:var(--accent);color:#fff;font-size:12px;font-weight:700;cursor:pointer;white-space:nowrap}.side-update-ready:hover{filter:brightness(.96)}.side-update-ready:disabled{cursor:default;opacity:.62}
</style>`
	updatedShell = strings.Replace(updatedShell, `</head>`, updaterStyle+`</head>`, 1)

	// The host owns update checks/downloads. This script only reads the local
	// status endpoint so the small percentage can move smoothly without page
	// reloads or GitHub requests from the UI.
	const updaterScript = `<script>
(() => {
  const control = () => document.getElementById('flipai-update-control');
  const install = async (button) => {
    if (!button || button.disabled) return;
    button.disabled = true;
    button.textContent = 'Installing…';
    try {
      const response = await fetch('/update/install', {method:'POST', credentials:'same-origin', cache:'no-store'});
      if (!response.ok) throw new Error('install failed');
    } catch (_) {
      // If Setup has already stopped FlipAi, the request can end while the
      // local server is disappearing. Leave the installing state in that case.
      setTimeout(() => { if (document.body.contains(button)) { button.disabled=false; button.textContent='Install update'; } }, 4000);
    }
  };
  const render = (s) => {
    const host = control();
    if (!host) return;
    host.replaceChildren();
    if (!s || !s.available) return;
    if (s.ready) {
      const button=document.createElement('button');
      button.type='button';
      button.id='flipai-update-install';
      button.className='side-update side-update-ready';
      button.dataset.version=s.targetVersion || '';
      button.title='Install FlipAi '+(s.targetVersion || 'update');
      button.textContent='Install update';
      button.addEventListener('click',()=>install(button));
      host.append(button);
      return;
    }
    const wrap=document.createElement('span');
    wrap.className='side-update-progress';
    wrap.title='Downloading FlipAi '+(s.targetVersion || 'update');
    const ring=document.createElement('span');
    ring.className='side-update-ring';
    const pct=Math.max(0,Math.min(100,Number(s.percent)||0));
    ring.style.setProperty('--update-percent',String(pct));
    const text=document.createElement('span');
    text.className='side-update-percent';
    text.textContent=pct+'%';
    wrap.append(ring,text);
    host.append(wrap);
  };
  const refresh = async () => {
    try {
      const response=await fetch('/update/status.json',{credentials:'same-origin',cache:'no-store'});
      if (response.ok) render(await response.json());
    } catch (_) {}
  };
  const initial=document.getElementById('flipai-update-install');
  if(initial) initial.addEventListener('click',()=>install(initial));
  refresh();
  window.setInterval(refresh,1000);
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
''', encoding="utf-8")

# ---------------------------------------------------------------------------
# Focused regressions for the new UX and resumable progress state.
# ---------------------------------------------------------------------------
Path("quiet_updater_test.go").write_text(r'''package main

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
''', encoding="utf-8")

Path("update_startup_test.go").write_text(r'''package main

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
''', encoding="utf-8")

# Keep installer fallback metadata in sync with the release version. The release
# workflow also passes /DMyVersion, but a locally compiled Setup should match.
replace_once("config.go", 'const version = "0.46.52"', 'const version = "0.46.53"')
replace_once("installer/FlipAi.iss", '#define MyVersion "0.46.52"', '#define MyVersion "0.46.53"')
Path("VERSION").write_text("0.46.53\n", encoding="utf-8")
Path("docs/RELEASE-NOTES.md").write_text('''# FlipAi v0.46.53\n\nThis release finishes the direct Google Voice media/startup work from v0.46.51 and gives FlipAi a much cleaner update experience without changing the rest of the app layout.\n\n## Update experience\n\n- New releases begin downloading automatically in the background as soon as FlipAi detects them.\n- The small control beside the FlipAi version now shows live percentage progress instead of a generic download icon.\n- At 100%, the progress control becomes a compact **Install update** button; the user chooses when to click it.\n- Partial installer downloads are kept as private `.part` files and resume after an app or computer restart when the server supports HTTP Range requests.\n- A fully downloaded, checksum-verified update installs automatically the next time FlipAi or Windows restarts if the user has not installed it manually first.\n- After either manual or restart-triggered installation, FlipAi always reopens the application window.\n- Update banners and update controls remain removed from Settings; only the small version-area updater is shown.\n\n## Included direct Google Voice work\n\n- Incoming Google Voice MMS photos, audio/voice notes, and supported video are delivered to browser-backed AI agents as the actual file rather than the text “MMS received.”\n- Browser-agent returned images/files are captured from the provider page. Images are sent back through Google Voice MMS when supported; unsupported/oversized media falls back to the exact provider conversation link.\n- Gmail forwarding is no longer exposed in the app UI; the old Gmail implementation remains preserved in Git history/rollback branch.\n- FlipAi starts hidden at Windows sign-in and recovers the direct Google Voice background listener without requiring the main window to be opened manually.\n\nNo transcription is added.\n''', encoding="utf-8")
