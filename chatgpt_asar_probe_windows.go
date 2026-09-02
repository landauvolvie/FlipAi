//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// augmentChatGPTASARProbe inspects only installed application-package files.
// It does not inspect ChatGPT profile/user-data storage, credentials, cookies,
// Local Storage, process memory, or network payloads.
func augmentChatGPTASARProbe(ctx context.Context, p *chatGPTDirectProbeResult) error {
	const script = `$ErrorActionPreference='SilentlyContinue'
$roots = @()
$packages = @(Get-AppxPackage -ErrorAction SilentlyContinue | Where-Object {
  $_.Name -match '(?i)(ChatGPT|OpenAI|Codex)' -or $_.PackageFullName -match '(?i)(ChatGPT|OpenAI|Codex)'
})
$roots += @($packages | ForEach-Object { [string]$_.InstallLocation } | Where-Object { $_ })
$procs = @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {
  $_.Name -match '^(ChatGPT|OpenAI).*\.exe$' -or ($_.ExecutablePath -and $_.ExecutablePath -match '(?i)\\(ChatGPT|OpenAI|Codex)\\')
})
$roots += @($procs | ForEach-Object {
  if ($_.ExecutablePath) { Split-Path -Parent ([string]$_.ExecutablePath) }
} | Where-Object { $_ })
@($roots | Sort-Object -Unique) | ConvertTo-Json -Compress`

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("discover ChatGPT ASAR roots: %s", msg)
	}

	var roots []string
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &roots); err != nil {
			return fmt.Errorf("decode ChatGPT ASAR roots: %w", err)
		}
	} else {
		var one string
		if err := json.Unmarshal([]byte(trimmed), &one); err != nil {
			return fmt.Errorf("decode ChatGPT ASAR root: %w", err)
		}
		roots = []string{one}
	}
	for i, root := range roots {
		roots[i] = filepath.Clean(root)
	}

	// First pass: broad ASAR attribution retained from v0.46.9.
	scan := scanChatGPTASARArchives(ctx, roots)
	p.ASARArchives = append(p.ASARArchives, scan.Archives...)
	p.ASARCodeEntries = append(p.ASARCodeEntries, scan.CodeEntries...)
	p.ASARMarkerSources = append(p.ASARMarkerSources, scan.MarkerSources...)
	p.ASARIPCCandidates = append(p.ASARIPCCandidates, scan.IPCCandidates...)
	p.ASARScanDetail = strings.TrimSpace(scan.Detail)

	// Second pass: focused protocol mapping. The result deliberately stores only
	// names, route paths and schema key names; never values or authenticated data.
	mapped := scanChatGPTProtocolMaps(ctx, roots)
	for _, v := range mapped.BridgeExposures {
		p.ASARIPCCandidates = append(p.ASARIPCCandidates, "BRIDGE EXPOSURE: "+v)
	}
	for _, v := range mapped.BridgeMethods {
		p.ASARIPCCandidates = append(p.ASARIPCCandidates, "BRIDGE METHOD: "+v)
	}
	for _, v := range mapped.IPCBindings {
		p.ASARIPCCandidates = append(p.ASARIPCCandidates, "IPC BINDING: "+v)
	}
	for _, v := range mapped.BackendRoutes {
		p.ASARMarkerSources = append(p.ASARMarkerSources, "BACKEND ROUTE: "+v)
	}
	for _, v := range mapped.RequestShapeKeys {
		p.ASARMarkerSources = append(p.ASARMarkerSources, "REQUEST KEY: "+v)
	}
	for _, v := range mapped.AuthFlowMarkers {
		p.ASARMarkerSources = append(p.ASARMarkerSources, "AUTH FLOW: "+v)
	}
	for _, v := range mapped.TransportSignals {
		p.ASARMarkerSources = append(p.ASARMarkerSources, "TRANSPORT: "+v)
	}
	for _, v := range mapped.ExternalSignals {
		p.ASARIPCCandidates = append(p.ASARIPCCandidates, "EXTERNAL TRANSPORT SIGNAL: "+v)
	}
	if strings.TrimSpace(mapped.Detail) != "" {
		if p.ASARScanDetail != "" {
			p.ASARScanDetail += " | "
		}
		p.ASARScanDetail += strings.TrimSpace(mapped.Detail)
	}
	return nil
}
