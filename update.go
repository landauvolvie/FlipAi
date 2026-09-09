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
	"net/url"
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
	maxUpdateBytes      = int64(200 << 20)
	maxChecksumBytes    = int64(64 << 10)
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
	DownloadedBytes  int64     `json:"downloadedBytes,omitempty"`
	TotalBytes       int64     `json:"totalBytes,omitempty"`
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

// ProgressPercent is the small percentage shown beside FlipAi's version while
// an update is being staged. A verified installer is always 100%; a resumable
// partial download uses its persisted byte counts so restarting FlipAi does not
// make the indicator jump back to zero.
func (r ReleaseInfo) ProgressPercent() int {
	if r.Ready() {
		return 100
	}
	if r.TotalBytes <= 0 || r.DownloadedBytes <= 0 {
		return 0
	}
	p := int((r.DownloadedBytes * 100) / r.TotalBytes)
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
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

// trustedUpdateURL restricts update traffic to GitHub-owned HTTPS endpoints.
// Loopback HTTP remains allowed only so the repository's httptest-based release
// tests can exercise the same code without reaching the network.
func trustedUpdateURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if (host == "127.0.0.1" || host == "localhost" || host == "::1") && u.Scheme == "http" {
		return true
	}
	if u.Scheme != "https" {
		return false
	}
	if host == "github.com" || host == "api.github.com" || host == "raw.githubusercontent.com" || host == "release-assets.githubusercontent.com" || host == "objects.githubusercontent.com" || host == "github-releases.githubusercontent.com" {
		return true
	}
	return strings.HasSuffix(host, ".githubusercontent.com")
}

func updateHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many update download redirects")
			}
			if !trustedUpdateURL(req.URL.String()) {
				return fmt.Errorf("update redirect left trusted GitHub endpoints: %s", req.URL.Hostname())
			}
			return nil
		},
	}
}

// fetchLatestVersionMarker reads the tiny VERSION file from GitHub's raw CDN.
// This is the frequent 30-second check; it avoids spending GitHub API quota
// when nothing changed. The release API is queried only after this marker is
// newer than the running build.
func fetchLatestVersionMarker(ctx context.Context) (string, error) {
	if !trustedUpdateURL(updateVersionFeedURL) {
		return "", errors.New("version marker URL is not a trusted GitHub endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateVersionFeedURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "FlipAi/"+version)
	resp, err := updateHTTPClient(10 * time.Second).Do(req)
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
	if !trustedUpdateURL(updateAPIURL) {
		return ReleaseInfo{}, errors.New("release API URL is not a trusted GitHub endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateAPIURL, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FlipAi/"+version)
	resp, err := updateHTTPClient(20 * time.Second).Do(req)
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
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return
	}
	// Readers and restart handoffs must see a complete record, even if the app
	// is stopped while progress is being saved.
	tmp, err := os.CreateTemp(filepath.Dir(statePath), ".update-*.tmp")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		return
	}
	if os.Rename(tmp.Name(), updateStatePath(statePath)) == nil {
		rememberUpdateSnapshot(info)
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
	if info.Version == current.Version && info.AssetURL == current.AssetURL {
		info.DownloadedBytes = current.DownloadedBytes
		info.TotalBytes = current.TotalBytes
		if current.Ready() {
			info.DownloadedPath = current.DownloadedPath
			info.DownloadedSHA256 = current.DownloadedSHA256
			info.DownloadedAt = current.DownloadedAt
		}
	}
	info.Error = ""
	saveUpdateState(a.statePath, info)
	return info
}

// watchForUpdates performs an early check after startup and then checks exactly
// every 30 seconds. A newer release is downloaded and verified immediately,
// but only installed when the user clicks the sidebar icon or restarts FlipAi.
// Download failures remain silent and are retried on a later check.
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
	lastPercent := -1
	lastSaved := time.Time{}
	progress := func(done, total int64) {
		p := 0
		if total > 0 {
			p = int((done * 100) / total)
			if p > 100 {
				p = 100
			}
		}
		if p == lastPercent && time.Since(lastSaved) < 500*time.Millisecond {
			return
		}
		latest := loadUpdateState(a.statePath)
		if latest.Version != current.Version || latest.AssetURL != current.AssetURL {
			return
		}
		latest.Downloading = true
		latest.DownloadedBytes = done
		latest.TotalBytes = total
		latest.Error = ""
		saveUpdateState(a.statePath, latest)
		lastPercent = p
		lastSaved = time.Now()
	}
	path, err := downloadUpdateWithProgress(dlCtx, current, progress)
	latest := loadUpdateState(a.statePath)
	if latest.Version != current.Version || latest.AssetURL != current.AssetURL {
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
	if st, statErr := os.Stat(path); statErr == nil {
		latest.DownloadedBytes = st.Size()
		latest.TotalBytes = st.Size()
	}
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

func safeUpdateAssetName(name string) bool {
	return name != "" && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`) && strings.HasPrefix(name, "FlipAi-Setup-") && strings.HasSuffix(strings.ToLower(name), ".exe")
}

func updateDownloadDir() (string, error) {
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(dataDir, "updates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create update staging directory: %w", err)
	}
	return dir, nil
}

// downloadUpdate fetches the release installer into FlipAi's private per-user
// update directory and requires a matching SHA256SUMS.txt entry. Nothing from a
// generic Downloads/TEMP location is accepted as the staged update.
func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {
	return downloadUpdateWithProgress(ctx, info, nil)
}

func downloadUpdateWithProgress(ctx context.Context, info ReleaseInfo, progress func(done, total int64)) (string, error) {
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
	if !trustedUpdateURL(info.AssetURL) {
		return "", errors.New("the release asset is not served by a trusted GitHub endpoint")
	}
	if info.SumsURL == "" || !trustedUpdateURL(info.SumsURL) {
		return "", errors.New("the release is missing a trusted SHA256SUMS.txt asset")
	}
	name := info.AssetName
	if !safeUpdateAssetName(name) {
		return "", errors.New("the release installer has an unexpected filename")
	}

	rawSums, err := downloadSmall(ctx, info.SumsURL, maxChecksumBytes)
	if err != nil {
		return "", fmt.Errorf("could not download the checksum file: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(rawSums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			want = strings.ToLower(fields[0])
			break
		}
	}
	decoded, err := hex.DecodeString(want)
	if want == "" || err != nil || len(decoded) != sha256.Size {
		return "", errors.New("the published checksum file does not contain a valid SHA-256 for " + name)
	}

	dir, err := updateDownloadDir()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, name)
	if sum, err := sha256File(dest); err == nil && strings.EqualFold(sum, want) {
		if st, statErr := os.Stat(dest); statErr == nil && progress != nil {
			progress(st.Size(), st.Size())
		}
		return dest, nil
	}
	_ = os.Remove(dest)
	sum, err := downloadWithProgress(ctx, info.AssetURL, dest, progress)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(want, sum) {
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

func downloadSmall(ctx context.Context, rawURL string, max int64) ([]byte, error) {
	if !trustedUpdateURL(rawURL) {
		return nil, errors.New("download URL is not a trusted GitHub endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FlipAi/"+version)
	resp, err := updateHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > max {
		return nil, errors.New("download is larger than the allowed size")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("download is larger than the allowed size")
	}
	return b, nil
}

// download saves a trusted URL atomically and returns the file's SHA-256.
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
