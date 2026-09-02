//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func platformProbeChatGPTDirect(ctx context.Context) (chatGPTDirectProbeResult, error) {
	// Do not inspect or return cookies, tokens, Local Storage, browser profiles,
	// process memory, or full process command lines. This diagnostic asks
	// Windows which ChatGPT processes/listeners exist and, when no live local
	// transport is exposed, reads only the installed application package files
	// for static protocol strings.
	const script = `$ErrorActionPreference='Stop'
$procs = @(Get-CimInstance Win32_Process | Where-Object {
  $_.Name -match '^(ChatGPT|OpenAI).*\.exe$' -or
  ($_.ExecutablePath -and $_.ExecutablePath -match '(?i)\\(ChatGPT|OpenAI)\\')
})
$pids = @($procs | ForEach-Object { [int]$_.ProcessId })
$names = @($procs | ForEach-Object { [string]$_.Name } | Sort-Object -Unique)
$exePaths = @($procs | ForEach-Object { [string]$_.ExecutablePath } | Where-Object { $_ } | Sort-Object -Unique)
$appxLocations = @(Get-AppxPackage -ErrorAction SilentlyContinue | Where-Object {
  $_.Name -match '(?i)(ChatGPT|OpenAI)' -or $_.PackageFullName -match '(?i)(ChatGPT|OpenAI)'
} | ForEach-Object { [string]$_.InstallLocation } | Where-Object { $_ } | Sort-Object -Unique)
$listeners = @()
if ($pids.Count -gt 0) {
  $listeners = @(Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object {
    ($pids -contains [int]$_.OwningProcess) -and ($_.LocalAddress -eq '127.0.0.1' -or $_.LocalAddress -eq '::1')
  } | ForEach-Object { [int]$_.LocalPort } | Sort-Object -Unique)
}
$allPipes = @(Get-ChildItem '\\.\pipe\' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name)
# Pipe names come from a machine-global namespace. A name is useful metadata,
# but it does not prove ownership. In particular, the Codex view of the same
# OpenAI desktop install creates codex-* pipes; those must never be treated as
# regular ChatGPT Chat connectivity.
$chatPipes = @($allPipes | Where-Object {
  $_ -match '(?i)(chatgpt|openai)'
} | Select-Object -First 40)
$ignoredCodexPipes = @($allPipes | Where-Object {
  $_ -match '(?i)^codex(?:-|$)'
} | Select-Object -First 40)
[pscustomobject]@{
  supported=$true
  processCount=$pids.Count
  processNames=@($names)
  executablePaths=@($exePaths)
  installLocations=@($appxLocations)
  loopbackPorts=@($listeners)
  namedPipes=@($chatPipes)
  ignoredPipes=@($ignoredCodexPipes)
} | ConvertTo-Json -Compress -Depth 5`

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return chatGPTDirectProbeResult{}, fmt.Errorf("Windows ChatGPT discovery: %s", msg)
	}
	var raw struct {
		Supported        bool     `json:"supported"`
		ProcessCount     int      `json:"processCount"`
		ProcessNames     []string `json:"processNames"`
		ExecutablePaths  []string `json:"executablePaths"`
		InstallLocations []string `json:"installLocations"`
		LoopbackPorts    []int    `json:"loopbackPorts"`
		NamedPipes       []string `json:"namedPipes"`
		IgnoredPipes     []string `json:"ignoredPipes"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return chatGPTDirectProbeResult{}, fmt.Errorf("decode Windows ChatGPT discovery: %w", err)
	}
	result := chatGPTDirectProbeResult{
		Supported:     true,
		ProcessCount:  raw.ProcessCount,
		ProcessNames:  raw.ProcessNames,
		LoopbackPorts: raw.LoopbackPorts,
		NamedPipes:    raw.NamedPipes,
		IgnoredPipes:  raw.IgnoredPipes,
	}

	// A local Chromium renderer can expose safe DevTools metadata on a listener
	// it already owns. We do not ask the desktop app to open any port; we only
	// classify listeners that already exist and belong to a ChatGPT process.
	for _, port := range raw.LoopbackPorts {
		if chatGPTDirectLooksLikeCDP(ctx, port) {
			result.CDPPorts = append(result.CDPPorts, port)
		}
	}

	// The real PC showed no ChatGPT-owned listener. The next safe place to look
	// is the installed program package itself: Electron/desktop bundles often
	// contain route names, custom protocols, preload IPC identifiers, and API
	// paths as plain static strings. This never touches the signed-in profile.
	files, markers, detail := chatGPTInspectStaticResources(ctx, raw.ExecutablePaths, raw.InstallLocations)
	result.StaticFilesScanned = len(files)
	result.StaticResourceFiles = files
	result.ProtocolMarkers = markers
	result.StaticInspectDetail = detail
	return result, nil
}

func chatGPTDirectLooksLikeCDP(ctx context.Context, port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	c := &http.Client{Timeout: 650 * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", port), nil)
	if err != nil {
		return false
	}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		return false
	}
	text := strings.ToLower(string(b))
	return strings.Contains(text, "websocketdebuggerurl") || strings.Contains(text, "protocol-version") || strings.Contains(text, "browser")
}

type chatGPTStaticCandidate struct {
	path     string
	display  string
	priority int
	size     int64
}

func chatGPTInspectStaticResources(ctx context.Context, executablePaths, installLocations []string) ([]string, []string, string) {
	roots := make([]string, 0, len(executablePaths)+len(installLocations))
	seenRoot := map[string]bool{}
	addRoot := func(v string) {
		v = filepath.Clean(strings.TrimSpace(v))
		if v == "." || v == "" {
			return
		}
		key := strings.ToLower(v)
		if !seenRoot[key] {
			seenRoot[key] = true
			roots = append(roots, v)
		}
	}
	for _, exe := range executablePaths {
		if exe = strings.TrimSpace(exe); exe != "" {
			addRoot(filepath.Dir(exe))
		}
	}
	for _, root := range installLocations {
		addRoot(root)
	}

	candidates := make([]chatGPTStaticCandidate, 0, 64)
	seenFile := map[string]bool{}
	skippedPrivate := 0
	walkErrors := 0
	for _, root := range roots {
		if ctx.Err() != nil {
			break
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				walkErrors++
				return nil
			}
			lowerPath := strings.ToLower(path)
			for _, privatePart := range []string{"\\user data\\", "\\local storage\\", "\\indexeddb\\", "\\session storage\\", "\\network\\", "\\cache\\", "\\gpucache\\"} {
				if strings.Contains(lowerPath, privatePart) {
					skippedPrivate++
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e != nil || info.Size() <= 0 || info.Size() > 128<<20 {
				return nil
			}
			base := strings.ToLower(d.Name())
			ext := strings.ToLower(filepath.Ext(base))
			priority := 99
			switch {
			case base == "app.asar":
				priority = 0
			case strings.Contains(base, "preload") || strings.Contains(base, "renderer") || strings.Contains(base, "main"):
				if ext == ".js" || ext == ".mjs" || ext == ".cjs" || ext == ".json" {
					priority = 1
				}
			case strings.Contains(lowerPath, "resources") && (ext == ".js" || ext == ".mjs" || ext == ".cjs" || ext == ".json" || ext == ".html"):
				priority = 2
			}
			if priority == 99 {
				return nil
			}
			key := strings.ToLower(filepath.Clean(path))
			if seenFile[key] {
				return nil
			}
			seenFile[key] = true
			display := d.Name()
			if rel, e := filepath.Rel(root, path); e == nil && rel != "." {
				display = filepath.ToSlash(rel)
			}
			if len(display) > 120 {
				display = "..." + display[len(display)-117:]
			}
			candidates = append(candidates, chatGPTStaticCandidate{path: path, display: display, priority: priority, size: info.Size()})
			return nil
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		if candidates[i].size != candidates[j].size {
			return candidates[i].size > candidates[j].size
		}
		return candidates[i].display < candidates[j].display
	})
	if len(candidates) > 36 {
		candidates = candidates[:36]
	}

	files := make([]string, 0, len(candidates))
	markers := make([]string, 0, 40)
	seenMarker := map[string]bool{}
	var totalRead int64
	readErrors := 0
	for _, c := range candidates {
		if ctx.Err() != nil || totalRead >= 96<<20 || len(markers) >= 40 {
			break
		}
		f, err := os.Open(c.path)
		if err != nil {
			readErrors++
			continue
		}
		limit := int64(48 << 20)
		if remain := int64(96<<20) - totalRead; remain < limit {
			limit = remain
		}
		b, err := io.ReadAll(io.LimitReader(f, limit))
		_ = f.Close()
		if err != nil {
			readErrors++
			continue
		}
		totalRead += int64(len(b))
		files = append(files, c.display)
		for _, m := range extractChatGPTProtocolMarkers(b) {
			if !seenMarker[m] {
				seenMarker[m] = true
				markers = append(markers, m)
				if len(markers) >= 40 {
					break
				}
			}
		}
	}
	sort.Strings(markers)

	noteParts := []string{}
	if len(roots) == 0 {
		noteParts = append(noteParts, "Windows did not provide an installed ChatGPT package path")
	}
	if len(candidates) == 0 && len(roots) > 0 {
		noteParts = append(noteParts, "no Electron-style app.asar or JavaScript resource bundle was readable in the discovered install roots")
	}
	if walkErrors > 0 || readErrors > 0 {
		noteParts = append(noteParts, fmt.Sprintf("%d package paths/files could not be read", walkErrors+readErrors))
	}
	if skippedPrivate > 0 {
		noteParts = append(noteParts, "user-data/profile directories were explicitly skipped")
	}
	return files, markers, strings.Join(noteParts, "; ")
}
