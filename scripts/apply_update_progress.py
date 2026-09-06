from pathlib import Path
import re


def replace(path, old, new, count=1):
    p = Path(path)
    s = p.read_text(encoding="utf-8")
    if old not in s:
        raise SystemExit(f"missing patch anchor in {path}: {old[:120]!r}")
    s = s.replace(old, new, count)
    p.write_text(s, encoding="utf-8")


# ---------------------------------------------------------------------------
# Actual download progress, persistent partial download, and restart install.
# ---------------------------------------------------------------------------
replace("update.go",
'''\tDownloading      bool      `json:"downloading,omitempty"`
\tDownloadedPath   string    `json:"downloadedPath,omitempty"`''',
'''\tDownloading      bool      `json:"downloading,omitempty"`
\tDownloadBytes    int64     `json:"downloadBytes,omitempty"`
\tDownloadTotal    int64     `json:"downloadTotal,omitempty"`
\tDownloadedPath   string    `json:"downloadedPath,omitempty"`''')

replace("update.go",
'''func (r ReleaseInfo) Ready() bool {
\tif !r.Newer() || r.DownloadedPath == "" {
\t\treturn false
\t}
\tst, err := os.Stat(r.DownloadedPath)
\treturn err == nil && !st.IsDir() && st.Size() > 0
}
''',
'''func (r ReleaseInfo) Ready() bool {
\tif !r.Newer() || r.DownloadedPath == "" {
\t\treturn false
\t}
\tst, err := os.Stat(r.DownloadedPath)
\treturn err == nil && !st.IsDir() && st.Size() > 0
}

// ProgressPercent is the compact sidebar percentage. A completed download may
// spend a short moment at 100% while its published checksum is verified; only
// Ready turns that 100% indicator into the Install update button.
func (r ReleaseInfo) ProgressPercent() int {
\tif r.Ready() {
\t\treturn 100
\t}
\tif r.DownloadTotal <= 0 || r.DownloadBytes <= 0 {
\t\treturn 0
\t}
\tp := int((r.DownloadBytes * 100) / r.DownloadTotal)
\tif p < 0 {
\t\treturn 0
\t}
\tif p > 100 {
\t\treturn 100
\t}
\treturn p
}
''')

replace("update.go",
'''\tcurrent.Downloading = true
\tcurrent.Error = ""
\tcurrent.DownloadedPath = ""
\tcurrent.DownloadedSHA256 = ""
\tcurrent.DownloadedAt = time.Time{}
\tsaveUpdateState(a.statePath, current)

\tdlCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
\tdefer cancel()
\tpath, err := downloadUpdate(dlCtx, current)
''',
'''\tcurrent.Downloading = true
\tcurrent.Error = ""
\tcurrent.DownloadedPath = ""
\tcurrent.DownloadedSHA256 = ""
\tcurrent.DownloadedAt = time.Time{}
\tsaveUpdateState(a.statePath, current)

\tdlCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
\tdefer cancel()
\tlastPercent := -1
\tprogress := func(done, total int64) {
\t\tcurrent.DownloadBytes = done
\t\tcurrent.DownloadTotal = total
\t\tpct := current.ProgressPercent()
\t\t// At most one state-file write per displayed percentage. The in-memory
\t\t// snapshot changes at the same cadence, which is more than enough for
\t\t// the one-second local sidebar refresh and avoids disk churn.
\t\tif pct == lastPercent {
\t\t\treturn
\t\t}
\t\tlastPercent = pct
\t\tsaveUpdateState(a.statePath, current)
\t}
\tpath, err := downloadUpdateWithProgress(dlCtx, current, progress)
''')

replace("update.go",
'''\tlatest.Error = ""
\tlatest.DownloadedPath = path
\tlatest.DownloadedSHA256 = sum
\tlatest.DownloadedAt = time.Now()
\tsaveUpdateState(a.statePath, latest)
}

// bridgeBusy reports''',
'''\tlatest.Error = ""
\tlatest.DownloadedPath = path
\tlatest.DownloadedSHA256 = sum
\tlatest.DownloadedAt = time.Now()
\tif st, statErr := os.Stat(path); statErr == nil {
\t\tlatest.DownloadBytes = st.Size()
\t\tlatest.DownloadTotal = st.Size()
\t}
\tsaveUpdateState(a.statePath, latest)
}

// installStagedUpdateOnStartup is called only by the watchdog instance that
// successfully owns the current Windows session. That distinction matters: a
// second click on an already-running FlipAi must merely raise the app, while a
// real app/PC restart should consume a verified staged update automatically.
func installStagedUpdateOnStartup(statePath string) bool {
\treturn installStagedUpdateOnStartupWith(statePath, runUpdateInstaller)
}

func installStagedUpdateOnStartupWith(statePath string, launch func(string, bool) error) bool {
\tinfo := loadUpdateState(statePath)
\tif !info.Ready() {
\t\treturn false
\t}
\tif err := launch(info.DownloadedPath, true); err != nil {
\t\tinfo.Error = truncate("automatic restart install failed: "+err.Error(), 200)
\t\tsaveUpdateState(statePath, info)
\t\treturn false
\t}
\tactivityLogForStatePath(statePath).Add("info", "host", "Installing staged FlipAi "+info.Version+" after restart", "", "", "")
\treturn true
}

// bridgeBusy reports''')

replace("update.go",
'''func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {
\tupdateDownloadMu.Lock()''',
'''func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {
\treturn downloadUpdateWithProgress(ctx, info, nil)
}

func downloadUpdateWithProgress(ctx context.Context, info ReleaseInfo, progress func(done, total int64)) (string, error) {
\tupdateDownloadMu.Lock()''')

replace("update.go",
'''\tsum, err := download(ctx, info.AssetURL, dest)
''',
'''\tsum, err := downloadWithProgress(ctx, info.AssetURL, dest, progress)
''')

# Replace the last download function with a resumable .part implementation.
p = Path("update.go")
s = p.read_text(encoding="utf-8")
marker = '''// download saves a trusted URL atomically and returns the file's SHA-256. A
// partial network transfer is never left behind under an executable filename.
func download(ctx context.Context, rawURL, dest string) (string, error) {'''
idx = s.find(marker)
if idx < 0:
    raise SystemExit("missing download function anchor")
prefix = s[:idx]
new_download = r'''// download saves a trusted URL atomically and returns the file's SHA-256.
// Incomplete bytes stay under a non-executable .part name so a real app or PC
// restart can continue the same download instead of throwing progress away.
func download(ctx context.Context, rawURL, dest string) (string, error) {
	return downloadWithProgress(ctx, rawURL, dest, nil)
}

type updateProgressWriter struct {
	w        io.Writer
	done     int64
	total    int64
	progress func(done, total int64)
}

func (w *updateProgressWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.done += int64(n)
	if w.progress != nil {
		w.progress(w.done, w.total)
	}
	return n, err
}

func contentRangeTotal(v string) int64 {
	if i := strings.LastIndex(strings.TrimSpace(v), "/"); i >= 0 && i+1 < len(v) {
		if n, err := strconv.ParseInt(strings.TrimSpace(v[i+1:]), 10, 64); err == nil && n > 0 {
			return n
		}
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
		resp.Body.Close()
		_ = os.Remove(part)
		offset = 0
		resp, err = request(0)
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	resume := offset > 0 && resp.StatusCode == http.StatusPartialContent
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	if !resume {
		offset = 0
	}

	total := int64(0)
	if resume {
		total = contentRangeTotal(resp.Header.Get("Content-Range"))
		if total == 0 && resp.ContentLength >= 0 {
			total = offset + resp.ContentLength
		}
	} else if resp.ContentLength >= 0 {
		total = resp.ContentLength
	}
	if total > maxUpdateBytes || (resp.ContentLength > 0 && offset+resp.ContentLength > maxUpdateBytes) {
		return "", errors.New("update installer is larger than the allowed size")
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
			f.Close()
			return "", err
		}
	}
	if progress != nil {
		progress(offset, total)
	}
	pw := &updateProgressWriter{w: f, done: offset, total: total, progress: progress}
	limit := maxUpdateBytes - offset + 1
	if limit < 1 {
		limit = 1
	}
	n, copyErr := io.Copy(pw, io.LimitReader(resp.Body, limit))
	written := offset + n
	if copyErr == nil && written > maxUpdateBytes {
		copyErr = errors.New("update installer is larger than the allowed size")
	}
	if copyErr == nil && total > 0 && written != total {
		copyErr = fmt.Errorf("update download stopped at %d of %d bytes", written, total)
	}
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		// Keep the .part file. The next updater run resumes it with an HTTP Range
		// request; the file can never be executed while it has this suffix.
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	_ = os.Remove(dest)
	if err := os.Rename(part, dest); err != nil {
		return "", err
	}
	if progress != nil {
		progress(written, written)
	}
	return sha256File(dest)
}
'''
p.write_text(prefix + new_download, encoding="utf-8")

# ---------------------------------------------------------------------------
# Startup: only the owning watchdog consumes a staged update. The normal
# launcher waits longer when a verified staged update is about to be installed.
# ---------------------------------------------------------------------------
replace("main.go", 'runWatchdog(dataDir, cfgPath)', 'runWatchdog(dataDir, cfgPath, statePath)', 1)
replace("main.go", 'runLauncher(dataDir, cfgPath)', 'runLauncher(dataDir, cfgPath, statePath)', 1)
replace("main.go", 'func runLauncher(dataDir, cfgPath string) {', 'func runLauncher(dataDir, cfgPath, statePath string) {', 1)
replace("main.go",
'''\tready := false
\tdeadline := time.Now().Add(10 * time.Second)
''',
'''\tready := false
\twaitForUpdateRestart := loadUpdateState(statePath).Ready()
\tdeadline := time.Now().Add(10 * time.Second)
\tif waitForUpdateRestart {
\t\tdeadline = time.Now().Add(45 * time.Second)
\t}
''')
replace("main.go", 'func runWatchdog(dataDir, cfgPath string) {', 'func runWatchdog(dataDir, cfgPath, statePath string) {', 1)
replace("main.go",
'''\tdefer release()

\tcfg := loadOrCreateConfig(cfgPath, dataDir)
''',
'''\tdefer release()

\t// A staged, checksum-verified update is deliberately installed only after
\t// this process proves it owns the watchdog. Merely opening an already-running
\t// FlipAi launches a duplicate watchdog that cannot own the mutex, so it does
\t// not steal the user's choice of when to install. A true app/PC restart does.
\tif installStagedUpdateOnStartup(statePath) {
\t\treturn
\t}

\tcfg := loadOrCreateConfig(cfgPath, dataDir)
''')

# ---------------------------------------------------------------------------
# Sidebar UI: circular real percentage -> 100% -> compact Install update button.
# ---------------------------------------------------------------------------
ui = Path("zzzzzzzz_quiet_updater_ui.go").read_text(encoding="utf-8")
ui = ui.replace(
'''func updaterUIState(releaseVersion string) string {
\tinfo := currentUpdateSnapshot()
\tif releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
\t\treturn ""
\t}
\tif info.Ready() {
\t\treturn "ready"
\t}
\tif info.Downloading {
\t\treturn "downloading"
\t}
\treturn "waiting"
}
''',
'''func updaterUIState(releaseVersion string) string {
\tinfo := currentUpdateSnapshot()
\tif releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
\t\treturn ""
\t}
\tif info.Ready() {
\t\treturn "ready"
\t}
\tif info.Downloading {
\t\treturn "downloading"
\t}
\treturn "waiting"
}

func updaterUIProgress(releaseVersion string) int {
\tinfo := currentUpdateSnapshot()
\tif releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
\t\treturn 0
\t}
\treturn info.ProgressPercent()
}
''')
old_sidebar = '''      <div class="side-version-row" id="flipai-version-row">
        <span class="side-version">v{{.Shell.Version}}</span>
        {{if .Shell.UpdateVersion}}
          {{$updateState := updaterState .Shell.UpdateVersion}}
          {{if eq $updateState "ready"}}
            <button class="side-update side-update-ready" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" title="Install FlipAi {{.Shell.UpdateVersion}} and restart">{{icon "download"}}</button>
          {{else}}
            <span class="side-update side-update-downloading" title="Downloading FlipAi {{.Shell.UpdateVersion}}">{{icon "download"}}</span>
          {{end}}
        {{end}}
      </div>'''
new_sidebar = '''      <div class="side-version-row" id="flipai-version-row">
        <span class="side-version">v{{.Shell.Version}}</span>
        {{if .Shell.UpdateVersion}}
          {{$updateState := updaterState .Shell.UpdateVersion}}
          {{$updateProgress := updaterProgress .Shell.UpdateVersion}}
          {{if eq $updateState "ready"}}
            <button class="side-update side-update-ready" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" title="Install FlipAi {{.Shell.UpdateVersion}} and restart">{{icon "download"}}<span>Install update</span></button>
          {{else}}
            <span class="side-update-progress" title="Downloading FlipAi {{.Shell.UpdateVersion}} — {{$updateProgress}}%">
              <span class="side-update-ring" style="--flipai-update-progress:{{$updateProgress}}"></span><span class="side-update-percent">{{$updateProgress}}%</span>
            </span>
          {{end}}
        {{end}}
      </div>'''
if old_sidebar not in ui:
    raise SystemExit("missing quiet updater sidebar anchor")
ui = ui.replace(old_sidebar, new_sidebar, 1)
style_re = re.compile(r'const updaterStyle = `<style>\n.*?\n</style>`', re.S)
new_style = '''const updaterStyle = `<style>
.side-version-row{display:flex;align-items:center;gap:8px;min-height:30px;flex-wrap:wrap}.side-version{white-space:nowrap}.side-update-progress{display:inline-flex;align-items:center;gap:5px;color:var(--accent);font-size:12px;font-weight:650;white-space:nowrap}.side-update-ring{--flipai-update-progress:0;width:18px;height:18px;border-radius:50%;background:conic-gradient(var(--accent) calc(var(--flipai-update-progress)*1%),var(--line) 0);position:relative;display:inline-block}.side-update-ring:after{content:"";position:absolute;inset:3px;border-radius:50%;background:var(--surface)}.side-update-percent{min-width:29px}.side-update{border:0;border-radius:8px;color:var(--accent);background:transparent}.side-update-ready{min-height:29px;padding:4px 7px;display:inline-flex;align-items:center;gap:5px;cursor:pointer;font-size:11px;font-weight:700;white-space:nowrap}.side-update-ready svg{width:14px;height:14px}.side-update-ready:hover{background:var(--accent-soft)}.side-update-ready:disabled{cursor:default;opacity:.6}
</style>`'''
ui, n = style_re.subn(new_style, ui, count=1)
if n != 1:
    raise SystemExit("missing quiet updater style anchor")
ui = ui.replace('''      button.disabled = true;
      try {
        await fetch('/update/install', {method:'POST', credentials:'same-origin', cache:'no-store'});
      } catch (_) {
        button.disabled = false;
      }
''', '''      button.disabled = true;
      const oldHTML = button.innerHTML;
      button.textContent = 'Installing…';
      try {
        const response = await fetch('/update/install', {method:'POST', credentials:'same-origin', cache:'no-store'});
        if (!response.ok) throw new Error('install failed');
      } catch (_) {
        button.disabled = false;
        button.innerHTML = oldHTML;
      }
''')
ui = ui.replace('window.setInterval(refreshUpdateControl, 5000);', 'window.setInterval(refreshUpdateControl, 1000);')
ui = ui.replace('five-minute host timer', 'background host timer')
ui = ui.replace('page.Funcs(template.FuncMap{"updaterState": updaterUIState})', 'page.Funcs(template.FuncMap{"updaterState": updaterUIState, "updaterProgress": updaterUIProgress})')
Path("zzzzzzzz_quiet_updater_ui.go").write_text(ui, encoding="utf-8")

# ---------------------------------------------------------------------------
# Regression tests for percentage persistence, resume, and restart install.
# ---------------------------------------------------------------------------
replace("quiet_updater_test.go",
'''\tif a.autoUpdateEnabled() {
\t\tt.Fatal("installation must never become automatic")
\t}
''',
'''\tif a.autoUpdateEnabled() {
\t\tt.Fatal("installation must not happen unattended while the current app session remains open")
\t}
''')
replace("quiet_updater_test.go",
'''\t\tDownloading: true,
\t}
''',
'''\t\tDownloading:   true,
\t\tDownloadBytes: 45,
\t\tDownloadTotal: 100,
\t}
''', 1)
replace("quiet_updater_test.go",
'''\tif !strings.Contains(body, `title="Downloading FlipAi 99.0.0"`) {
\t\tt.Fatal("available update should show the quiet download indicator")
\t}
''',
'''\tif !strings.Contains(body, `Downloading FlipAi 99.0.0`) || !strings.Contains(body, `45%`) {
\t\tt.Fatal("available update should show the quiet real download percentage")
\t}
''')
replace("quiet_updater_test.go",
'''\tif !strings.Contains(body, `id="flipai-update-install"`) || !strings.Contains(body, "side-update-ready") {
\t\tt.Fatal("staged update should become the compact install button")
\t}
''',
'''\tif !strings.Contains(body, `id="flipai-update-install"`) || !strings.Contains(body, "side-update-ready") || !strings.Contains(body, "Install update") {
\t\tt.Fatal("staged update should become the compact Install update button")
\t}
''')

Path("update_progress_restart_test.go").write_text(r'''package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateProgressPersists(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	info := ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe", Downloading: true, DownloadBytes: 45, DownloadTotal: 100}
	saveUpdateState(statePath, info)
	got := loadUpdateState(statePath)
	if got.DownloadBytes != 45 || got.DownloadTotal != 100 || got.ProgressPercent() != 45 {
		t.Fatalf("persisted progress = %d/%d (%d%%), want 45/100 (45%%)", got.DownloadBytes, got.DownloadTotal, got.ProgressPercent())
	}
}

func TestRestartInstallsVerifiedStagedUpdateAndReopensWindow(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	installer := filepath.Join(dir, "FlipAi-Setup-v99.0.0.exe")
	if err := os.WriteFile(installer, []byte("verified"), 0600); err != nil {
		t.Fatal(err)
	}
	saveUpdateState(statePath, ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe", DownloadedPath: installer, DownloadBytes: 8, DownloadTotal: 8})
	called := false
	if !installStagedUpdateOnStartupWith(statePath, func(path string, reopen bool) error {
		called = true
		if path != installer || !reopen {
			t.Fatalf("launch = (%q,%v), want staged installer and reopen=true", path, reopen)
		}
		return nil
	}) {
		t.Fatal("verified staged update was not consumed on restart")
	}
	if !called {
		t.Fatal("installer launcher was not called")
	}
}

func TestRestartDoesNotInstallIncompleteUpdate(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	saveUpdateState(statePath, ReleaseInfo{Version: "99.0.0", AssetURL: "https://example.invalid/FlipAi-Setup-v99.0.0.exe", Downloading: true, DownloadBytes: 45, DownloadTotal: 100})
	if installStagedUpdateOnStartupWith(statePath, func(string, bool) error {
		t.Fatal("incomplete update must not launch")
		return nil
	}) {
		t.Fatal("incomplete update reported as installed")
	}
}

func TestDownloadResumesPartFileAndReportsProgress(t *testing.T) {
	oldMax := maxUpdateBytes
	_ = oldMax
	payload := []byte(strings.Repeat("abcdef", 4096))
	var sawRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRange = r.Header.Get("Range")
		start := 0
		if strings.HasPrefix(sawRange, "bytes=") {
			fmt.Sscanf(strings.TrimPrefix(sawRange, "bytes="), "%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(payload[start:])
	}))
	defer server.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "FlipAi-Setup-v99.0.0.exe")
	part := dest + ".part"
	prefix := payload[:5000]
	if err := os.WriteFile(part, prefix, 0600); err != nil {
		t.Fatal(err)
	}
	var lastDone, lastTotal int64
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := downloadWithProgress(ctx, server.URL, dest, func(done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatal(err)
	}
	if sawRange != "bytes=5000-" {
		t.Fatalf("Range = %q, want bytes=5000-", sawRange)
	}
	if lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Fatalf("final progress = %d/%d, want %d/%d", lastDone, lastTotal, len(payload), len(payload))
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatal("resumed file bytes differ from source")
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("successful download left the .part file behind")
	}
}
''', encoding="utf-8")

# Release metadata: this branch becomes v0.46.53 once merged.
replace("VERSION", "0.46.52\n", "0.46.53\n")
replace("config.go", 'const version = "0.46.52"', 'const version = "0.46.53"')
replace("installer/FlipAi.iss", '#define MyVersion "0.46.52"', '#define MyVersion "0.46.53"')

Path("docs/RELEASE-NOTES.md").write_text('''# FlipAi v0.46.53\n\nThis release finishes the direct Google Voice media/startup work and replaces the old update presentation with the compact update flow in the sidebar.\n\n## Updates\n\n- A newly published FlipAi version starts downloading automatically in the background.\n- The existing version area now shows the real download percentage with a small circular progress indicator.\n- At 100%, after checksum verification, the progress control becomes a compact **Install update** button.\n- The user can keep working and choose when to install.\n- If FlipAi or Windows is actually restarted while a verified update is staged, the owning watchdog installs it automatically and reopens the FlipAi application. Simply opening an already-running FlipAi does not force the install.\n- Partial installer downloads stay under a non-executable `.part` filename and resume with HTTP Range requests after an app/PC restart when GitHub supports ranges.\n- Update progress is stored in FlipAi's private update state so the sidebar survives restarts cleanly.\n- The old page-wide update banner and Settings update card remain removed; no unrelated app UI is changed.\n\n## Included direct Google Voice work\n\n- Incoming Google Voice MMS media is delivered to supported browser agents as the actual local file, without transcription or a download-link substitution.\n- Browser-chat returned images/files are captured; supported images are sent through Google Voice MMS and unsupported/oversized returned media falls back to the exact provider conversation URL.\n- The Gmail connection UI remains retired while the old implementation is preserved in Git history and the `archive/gmail-voice-bridge-v0.46.50` rollback branch.\n- Hidden Windows sign-in startup and direct Google Voice background-worker recovery remain enabled.\n\n## Validation\n\nThe release pipeline runs Linux and Windows tests, real-browser flow tests, vet/race checks, Windows builds, Google Voice background smoke checks, Defender scans, installer install/uninstall smoke tests, SBOM generation, checksums and build provenance before publishing.\n''', encoding="utf-8")

print("update progress/restart install patch applied")
