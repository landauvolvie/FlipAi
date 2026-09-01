//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

func platformProbeChatGPTDirect(ctx context.Context) (chatGPTDirectProbeResult, error) {
	// Do not inspect or return cookies, tokens, Local Storage, browser profiles,
	// or full command lines. This probe only asks Windows which ChatGPT-owned
	// processes and local IPC/listeners exist so we can find a clean backend
	// transport without ever touching the visible UI.
	const script = `$ErrorActionPreference='Stop'
$procs = @(Get-CimInstance Win32_Process | Where-Object {
  $_.Name -match '^(ChatGPT|OpenAI).*\.exe$' -or
  ($_.ExecutablePath -and $_.ExecutablePath -match '(?i)\\(ChatGPT|OpenAI)\\') -or
  ($_.CommandLine -and $_.CommandLine -match '(?i)(^|[\\/])ChatGPT(\.exe)?([\"\s]|$)')
})
$pids = @($procs | ForEach-Object { [int]$_.ProcessId })
$names = @($procs | ForEach-Object { [string]$_.Name } | Sort-Object -Unique)
$listeners = @()
if ($pids.Count -gt 0) {
  $listeners = @(Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object {
    ($pids -contains [int]$_.OwningProcess) -and ($_.LocalAddress -eq '127.0.0.1' -or $_.LocalAddress -eq '::1')
  } | ForEach-Object { [int]$_.LocalPort } | Sort-Object -Unique)
}
$pipes = @(Get-ChildItem '\\.\pipe\' -ErrorAction SilentlyContinue | Where-Object {
  $_.Name -match '(?i)(chatgpt|openai|codex)'
} | Select-Object -First 40 -ExpandProperty Name)
$debugPipe = $false
$debugPorts = @()
foreach ($p in $procs) {
  $cmd = [string]$p.CommandLine
  if ($cmd -match '(?i)--remote-debugging-pipe') { $debugPipe = $true }
  $matches = [regex]::Matches($cmd, '(?i)--remote-debugging-port(?:=|\s+)(\d+)')
  foreach ($m in $matches) { $debugPorts += [int]$m.Groups[1].Value }
}
[pscustomobject]@{
  supported=$true
  processCount=$pids.Count
  processNames=@($names)
  loopbackPorts=@($listeners)
  hintedDebugPorts=@($debugPorts | Sort-Object -Unique)
  namedPipes=@($pipes)
  debugPipe=$debugPipe
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
		LoopbackPorts    []int    `json:"loopbackPorts"`
		HintedDebugPorts []int    `json:"hintedDebugPorts"`
		NamedPipes       []string `json:"namedPipes"`
		DebugPipe        bool     `json:"debugPipe"`
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
		DebugPipe:     raw.DebugPipe,
		CDPPorts:      append([]int(nil), raw.HintedDebugPorts...),
	}

	// A renderer can expose the Chrome DevTools protocol without advertising the
	// flag in the parent process command line. Check only ChatGPT-owned loopback
	// listeners and only the harmless /json/version metadata endpoint.
	for _, port := range raw.LoopbackPorts {
		if chatGPTDirectLooksLikeCDP(ctx, port) {
			result.CDPPorts = append(result.CDPPorts, port)
		}
	}
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
