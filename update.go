package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FlipAi ships as a GitHub release. Updates are intentionally quiet: the host
// checks a tiny version marker every 30 seconds, downloads a newer installer
// in the background, and leaves installation to the small button beside the
// version in the app. The heavier GitHub release API is only queried after the
// marker says a newer version exists.
const (
	updateRepo          = "landauvolvie/FlipAi"
	updateAPI           = "https://api.github.com/repos/" + updateRepo + "/releases/latest"
	updateVersionFeed   = "https://raw.githubusercontent.com/" + updateRepo + "/main/VERSION"
	updateCheckInterval = 30 * time.Second
)

// These are variables so tests can point both network checks at local servers.
var (
	updateAPIURL         = updateAPI
	updateVersionFeedURL = updateVersionFeed
)

var (
	updateDownloadMu sync.Mutex
	updateSnapshotMu sync.RWMutex
	updateSnapshot   ReleaseInfo
)

func rememberUpdateSnapshot(info ReleaseInfo) {
	updateSnapshotMu.Lock()
	updateSnapshot = info
	updateSnapshotMu.Unlock()
}

func currentUpdateSnapshot() ReleaseInfo {
	updateSnapshotMu.RLock()
	defer updateSnapshotMu.RUnlock()
	return updateSnapshot
}

// ReleaseInfo is what the last release check found. It lives in update.json so
// the UI can read update readiness without touching the network on page loads.
type ReleaseInfo struct {
	Version          string    `json:"version,omitempty"`
	Tag              string    `json:"tag,omitempty"`
	PageURL          string    `json:"pageUrl,omitempty"`
	AssetURL         string    `json:"assetUrl,omitempty"`
	AssetName        string    `json:"assetName,omitempty"`
	SumsURL          string    `json:"sumsUrl,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	Published        time.Time `json:"published,omitempty"`
	CheckedAt        time.Time `json:"checkedAt,omitempty"`
	Error            string    `json:"error,omitempty"`
	Downloading      bool      `json:"downloading,omitempty"`
	DownloadedPath   string    `json:"downloadedPath,omitempty"`
	DownloadedSHA256 string    `json:"downloadedSha256,omitempty"`
	DownloadedAt     time.Time `json:"downloadedAt,omitempty"`
}

// Newer reports whether the checked release is ahead of this build.
func (r ReleaseInfo) Newer() bool {
	return r.Version != "" && r.AssetURL != "" && versionLess(version, r.Version)
}

// Ready reports that the verified installer for the currently offered release
// is still present locally and can be installed without another download.
func (r ReleaseInfo) Ready() bool {
	if !r.Newer() || r.DownloadedPath == "" {
		return false
	}
	st, err := os.Stat(r.DownloadedPath)
	return err == nil && !st.IsDir() && st.Size() > 0
}

// versionLess compares dotted versions numerically, so 0.10.0 is correctly
// newer than 0.9.0 — the comparison string ordering gets wrong.
func versionLess(a, b string) bool {
	as, bs := versionParts(a), versionParts(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// fetchLatestVersionMarker reads the tiny VERSION file from GitHub's raw CDN.
// This is the frequent 30-second check; it avoids spending GitHub API quota
// when nothing changed. The release API is queried only after this marker is
// newer than the running build.
func fetchLatestVersionMarker(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateVersionFeedURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "FlipAi/"+version)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version marker returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	v := strings.TrimPrefix(strings.TrimSpace(string(raw)), "v")
	if len(versionParts(v)) == 0 {
		return "", errors.New("version marker did not contain a valid version")
	}
	return v, nil
}

// fetchLatestRelease asks GitHub for the newest published release. It sends no
// user identifier, configuration, or message data.
func fetchLatestRelease(ctx context.Context) (ReleaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateAPIURL, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FlipAi/"+version)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("GitHub returned HTTP %d for the release feed", resp.StatusCode)
	}
	var gr githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&gr); err != nil {
		return ReleaseInfo{}, err
	}
	if gr.Draft || gr.Prerelease {
		return ReleaseInfo{CheckedAt: time.Now()}, nil
	}
	info := ReleaseInfo{
		Version:   strings.TrimPrefix(gr.TagName, "v"),
		Tag:       gr.TagName,
		PageURL:   gr.HTMLURL,
		Notes:     truncate(strings.TrimSpace(gr.Body), 600),
		Published: gr.PublishedAt,
		CheckedAt: time.Now(),
	}
	for _, asset := range gr.Assets {
		switch {
		case strings.HasPrefix(asset.Name, "FlipAi-Setup-") && strings.HasSuffix(asset.Name, ".exe"):
			info.AssetName, info.AssetURL = asset.Name, asset.BrowserDownloadURL
		case asset.Name == "SHA256SUMS.txt":
			info.SumsURL = asset.BrowserDownloadURL
		}
	}
	return info, nil
}

// The release check lives in its own file rather than in state.json, which the
// bridge rewrites after every turn: two writers doing read-modify-write on the
// same file could drop a message checkpoint.
func updateStatePath(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "update.json")
}

func loadUpdateState(statePath string) ReleaseInfo {
	var info ReleaseInfo
	if raw, err := os.ReadFile(updateStatePath(statePath)); err == nil {
		_ = json.Unmarshal(raw, &info)
	}
	if info.DownloadedPath != "" && !info.Ready() {
		info.DownloadedPath = ""
		info.DownloadedSHA256 = ""
		info.DownloadedAt = time.Time{}
	}
	rememberUpdateSnapshot(info)
	return info
}

func saveUpdateState(statePath string, info ReleaseInfo) {
	rememberUpdateSnapshot(info)
	if raw, err := json.MarshalIndent(info, "", "  "); err == nil {
		_ = os.WriteFile(updateStatePath(statePath), raw, 0o600)
	}
}

// updateInterval is deliberately not configurable. Updates are lightweight and
// FlipAi should discover them consistently on every machine.
func (a *App) updateInterval() time.Duration { return updateCheckInterval }

// autoUpdateEnabled remains for source/config compatibility with older tests and
// installs. Installation is never automatic; only downloading is.
func (a *App) autoUpdateEnabled() bool { return false }

// checkForUpdate refreshes the stored release info. force skips the interval
// that keeps background checks quiet. A verified staged installer is preserved
// when GitHub reports the same release again.
func (a *App) checkForUpdate(ctx context.Context, force bool) ReleaseInfo {
	current := loadUpdateState(a.statePath)
	if !force && time.Since(current.CheckedAt) < a.updateInterval() {
		return current
	}
	if !force {
		marker, err := fetchLatestVersionMarker(ctx)
		if err != nil {
			current.CheckedAt = time.Now()
			current.Error = truncate(err.Error(), 200)
			saveUpdateState(a.statePath, current)
			return current
		}
		if !versionLess(version, marker) {
			current.CheckedAt = time.Now()
			current.Error = ""
			saveUpdateState(a.statePath, current)
			return current
		}
		if current.Version == marker && current.AssetURL != "" {
			current.CheckedAt = time.Now()
			current.Error = ""
			saveUpdateState(a.statePath, current)
			return current
		}
	}
	info, err := fetchLatestRelease(ctx)
	if err != nil {
		current.CheckedAt = time.Now()
		current.Error = truncate(err.Error(), 200)
		current.Downloading = false
		saveUpdateState(a.statePath, current)
		return current
	}
	if info.Version == current.Version && info.AssetURL == current.AssetURL && current.Ready() {
		info.DownloadedPath = current.DownloadedPath
		info.DownloadedSHA256 = current.DownloadedSHA256
		info.DownloadedAt = current.DownloadedAt
	}
	info.Error = ""
	saveUpdateState(a.statePath, info)
	return info
}

// watchForUpdates performs an early check after startup and then checks exactly
// every 30 seconds. A newer release is downloaded and verified immediately,
// but never installed until the user clicks the small install control in the
// sidebar. Download failures remain silent and are retried on a later check.
func (a *App) watchForUpdates(ctx context.Context) {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		info := a.checkForUpdate(ctx, false)
		if info.Newer() && !info.Ready() {
			a.stageUpdate(ctx, info)
		}
		timer.Reset(a.updateInterval())
	}
}

// stageUpdate downloads and verifies an update without interrupting the bridge
// or showing any UI. It persists readiness so clicking Install never needs to
// download the same installer again.
func (a *App) stageUpdate(ctx context.Context, info ReleaseInfo) {
	current := loadUpdateState(a.statePath)
	if current.Version != info.Version || current.AssetURL != info.AssetURL {
		current = info
	}
	if current.Ready() {
		return
	}
	current.Downloading = true
	current.Error = ""
	current.DownloadedPath = ""
	current.DownloadedSHA256 = ""
	current.DownloadedAt = time.Time{}
	saveUpdateState(a.statePath, current)

	dlCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	path, err := downloadUpdate(dlCtx, current)
	latest := loadUpdateState(a.statePath)
	if latest.Version != current.Version || latest.AssetURL != current.AssetURL {
		// A newer release replaced this one while it was downloading. Leave the
		// old temp file alone; the next cycle stages the current release.
		return
	}
	latest.Downloading = false
	if err != nil {
		latest.Error = truncate(err.Error(), 200)
		latest.DownloadedPath = ""
		latest.DownloadedSHA256 = ""
		latest.DownloadedAt = time.Time{}
		saveUpdateState(a.statePath, latest)
		return
	}
	sum, hashErr := sha256File(path)
	if hashErr != nil {
		latest.Error = truncate(hashErr.Error(), 200)
		latest.DownloadedPath = ""
		latest.DownloadedSHA256 = ""
		latest.DownloadedAt = time.Time{}
		saveUpdateState(a.statePath, latest)
		return
	}
	latest.Error = ""
	latest.DownloadedPath = path
	latest.DownloadedSHA256 = sum
	latest.DownloadedAt = time.Now()
	saveUpdateState(a.statePath, latest)
}

// bridgeBusy reports whether an agent turn is running right now.
func (a *App) bridgeBusy() bool {
	a.mu.Lock()
	b := a.bridge
	a.mu.Unlock()
	if b == nil {
		return false
	}
	return b.Busy()
}

// downloadUpdate fetches the release installer into the temp folder and checks
// it against SHA256SUMS.txt. If the same verified installer was already staged,
// the manual install path reuses it instead of downloading a second time.
func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {
	updateDownloadMu.Lock()
	defer updateDownloadMu.Unlock()
	if info.Ready() && info.DownloadedSHA256 != "" {
		if sum, err := sha256File(info.DownloadedPath); err == nil && strings.EqualFold(sum, info.DownloadedSHA256) {
			return info.DownloadedPath, nil
		}
		_ = os.Remove(info.DownloadedPath)
	}
	if info.AssetURL == "" {
		return "", errors.New("this release has no Windows installer attached")
	}
	if !strings.HasPrefix(info.AssetURL, "https://") {
		return "", errors.New("the release asset is not served over HTTPS")
	}
	name := info.AssetName
	if name == "" {
		name = "FlipAi-Setup.exe"
	}
	dest := filepath.Join(os.TempDir(), name)
	sum, err := download(ctx, info.AssetURL, dest)
	if err != nil {
		return "", err
	}
	if info.SumsURL == "" {
		return dest, nil
	}
	sumsPath := dest + ".sha256"
	defer os.Remove(sumsPath)
	if _, err := download(ctx, info.SumsURL, sumsPath); err != nil {
		return "", fmt.Errorf("could not download the checksum file: %w", err)
	}
	raw, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", err
	}
	want := ""
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			want = strings.ToLower(fields[0])
		}
	}
	if want == "" {
		return "", errors.New("the published checksum file does not list " + name)
	}
	if want != sum {
		_ = os.Remove(dest)
		return "", errors.New("the downloaded installer does not match its published checksum")
	}
	return dest, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// download saves a URL to a path and returns the file's SHA-256.
func download(ctx context.Context, url, dest string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "FlipAi/"+version)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 200<<20)); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
