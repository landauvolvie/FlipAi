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
	"time"
)

// FlipAi ships as a GitHub release. The app checks that release feed so an
// existing install can tell the user a newer build exists and install it in
// place, instead of the user finding a download that looks like a first-time
// setup all over again.
const (
	updateRepo          = "landauvolvie/FlipAi"
	updateAPI           = "https://api.github.com/repos/" + updateRepo + "/releases/latest"
	updateCheckInterval = 12 * time.Hour
)

// updateAPIURL is a variable so tests can point the check at a local server.
var updateAPIURL = updateAPI

// ReleaseInfo is what the last release check found. It lives in state.json so
// the UI can report it without touching the network on every page render.
type ReleaseInfo struct {
	Version   string    `json:"version,omitempty"`
	Tag       string    `json:"tag,omitempty"`
	PageURL   string    `json:"pageUrl,omitempty"`
	AssetURL  string    `json:"assetUrl,omitempty"`
	AssetName string    `json:"assetName,omitempty"`
	SumsURL   string    `json:"sumsUrl,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Published time.Time `json:"published,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// Newer reports whether the checked release is ahead of this build.
func (r ReleaseInfo) Newer() bool {
	return r.Version != "" && r.AssetURL != "" && versionLess(version, r.Version)
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

// fetchLatestRelease asks GitHub for the newest published release. It is the
// only outbound request FlipAi makes that is not Gmail, and it sends nothing
// about the user: no identifier, no configuration, no message data.
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

// checkForUpdate refreshes the stored release info. force skips the interval
// that keeps the background check quiet.
func (a *App) checkForUpdate(ctx context.Context, force bool) ReleaseInfo {
	st := loadState(a.statePath)
	if !force && time.Since(st.Update.CheckedAt) < updateCheckInterval {
		return st.Update
	}
	info, err := fetchLatestRelease(ctx)
	if err != nil {
		st.Update.CheckedAt = time.Now()
		st.Update.Error = truncate(err.Error(), 200)
		_ = saveState(a.statePath, st)
		return st.Update
	}
	st.Update = info
	_ = saveState(a.statePath, st)
	return info
}

// watchForUpdates runs one check shortly after the host starts and then keeps
// checking on a slow interval.
func (a *App) watchForUpdates(ctx context.Context) {
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		info := a.checkForUpdate(ctx, false)
		if info.Newer() {
			activityLogForStatePath(a.statePath).Add("info", "host", "Update available: FlipAi "+info.Version, "", "", "")
		}
		timer.Reset(updateCheckInterval)
	}
}

// downloadUpdate fetches the release installer into the temp folder and checks
// it against the SHA256SUMS.txt published beside it. A mismatch aborts: FlipAi
// will not run an installer it cannot verify.
func downloadUpdate(ctx context.Context, info ReleaseInfo) (string, error) {
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
		// A missing checksum file is not proof of tampering, but FlipAi should
		// say so rather than pretend the download was verified.
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
