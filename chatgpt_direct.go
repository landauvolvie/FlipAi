package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// chatGPTDirectProbeResult is intentionally metadata-only. The direct ChatGPT
// experiment must discover a clean local transport without copying browser
// cookies, bearer tokens, or the desktop app's private credentials.
type chatGPTDirectProbeResult struct {
	Supported           bool     `json:"supported"`
	ProcessCount        int      `json:"processCount"`
	ProcessNames        []string `json:"processNames,omitempty"`
	LoopbackPorts       []int    `json:"loopbackPorts,omitempty"`
	CDPPorts            []int    `json:"cdpPorts,omitempty"`
	NamedPipes          []string `json:"namedPipes,omitempty"`
	IgnoredPipes        []string `json:"ignoredPipes,omitempty"`
	DebugPipe           bool     `json:"debugPipe,omitempty"`
	StaticFilesScanned  int      `json:"staticFilesScanned,omitempty"`
	StaticResourceFiles []string `json:"staticResourceFiles,omitempty"`
	ProtocolMarkers     []string `json:"protocolMarkers,omitempty"`
	StaticInspectDetail string   `json:"staticInspectDetail,omitempty"`

	// RuntimeArchitecture is the second-stage diagnostic added after v0.46.7.
	// It records only safe process/package metadata. It never contains cookies,
	// tokens, request headers, process memory, or full command lines.
	RuntimeArchitecture string   `json:"runtimeArchitecture,omitempty"`
	RuntimeSignals      []string `json:"runtimeSignals,omitempty"`
	ProcessInventory    []string `json:"processInventory,omitempty"`
	ChildProcesses      []string `json:"childProcesses,omitempty"`
	PackageIdentity     []string `json:"packageIdentity,omitempty"`
	PackageTopLevel     []string `json:"packageTopLevel,omitempty"`
	AppExtensions       []string `json:"appExtensions,omitempty"`
	ProtocolSchemes     []string `json:"protocolSchemes,omitempty"`
	WindowClasses       []string `json:"windowClasses,omitempty"`
	NetworkPeers        []string `json:"networkPeers,omitempty"`
	OpenAIDNSNames      []string `json:"openAIDNSNames,omitempty"`
	MarkerSources       []string `json:"markerSources,omitempty"`
	NoisyMarkerSources  []string `json:"noisyMarkerSources,omitempty"`
	ASARArchives        []string `json:"asarArchives,omitempty"`
	ASARCodeEntries     []string `json:"asarCodeEntries,omitempty"`
	ASARMarkerSources   []string `json:"asarMarkerSources,omitempty"`
	ASARIPCCandidates   []string `json:"asarIpcCandidates,omitempty"`
	ASARScanDetail      string   `json:"asarScanDetail,omitempty"`
	DirectAssessment    string   `json:"directAssessment,omitempty"`

	Detail string `json:"detail,omitempty"`
}

func uniqueSortedInts(in []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(in))
	for _, v := range in {
		if v <= 0 || v > 65535 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func uniqueSortedStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func joinInts(in []int) string {
	if len(in) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(in))
	for _, v := range in {
		parts = append(parts, fmt.Sprintf("%d", v))
	}
	return strings.Join(parts, ", ")
}

// provenTransport is deliberately stricter than "we saw an interesting name".
// A named pipe discovered from the global Windows pipe namespace has no owner
// attached to it, so its name alone must never turn the probe green. A direct
// candidate is proven only when Windows ties a loopback listener to a ChatGPT
// process, or when a ChatGPT-owned Chromium debugging channel is identified.
func (p chatGPTDirectProbeResult) provenTransport() bool {
	return len(p.LoopbackPorts) > 0 || len(p.CDPPorts) > 0 || p.DebugPipe
}

func appendSummaryList(lines []string, label string, values []string, limit int) []string {
	if len(values) == 0 {
		return lines
	}
	if limit <= 0 || limit > len(values) {
		limit = len(values)
	}
	text := strings.Join(values[:limit], " | ")
	if limit < len(values) {
		text += fmt.Sprintf(" | ... +%d more", len(values)-limit)
	}
	return append(lines, label+": "+text)
}

func (p chatGPTDirectProbeResult) summary() string {
	if !p.Supported {
		if p.Detail != "" {
			return p.Detail
		}
		return "Direct ChatGPT discovery is only available on Windows."
	}
	if p.ProcessCount == 0 {
		return "ChatGPT desktop was not found running. Open the ChatGPT desktop app, sign in, then run the diagnostic again."
	}
	lines := []string{
		fmt.Sprintf("ChatGPT desktop processes: %d", p.ProcessCount),
		"ChatGPT-owned loopback listeners: " + joinInts(p.LoopbackPorts),
		"ChatGPT-owned Chromium DevTools listeners: " + joinInts(p.CDPPorts),
	}
	if p.DebugPipe {
		lines = append(lines, "ChatGPT Chromium remote-debugging pipe: advertised")
	} else {
		lines = append(lines, "ChatGPT Chromium remote-debugging pipe: not advertised")
	}
	if len(p.NamedPipes) > 0 {
		lines = append(lines, "ChatGPT/OpenAI-named pipes (ownership not proven): "+strings.Join(p.NamedPipes, ", "))
	} else {
		lines = append(lines, "ChatGPT/OpenAI-named pipes: none found")
	}
	if len(p.IgnoredPipes) > 0 {
		lines = append(lines, "Codex pipes ignored: "+strings.Join(p.IgnoredPipes, ", "))
	}

	lines = append(lines, fmt.Sprintf("Static ChatGPT app resource files scanned: %d", p.StaticFilesScanned))
	if len(p.StaticResourceFiles) > 0 {
		lines = append(lines, "Static resource files inspected: "+strings.Join(p.StaticResourceFiles, ", "))
	}
	if len(p.ProtocolMarkers) > 0 {
		lines = append(lines, "Static protocol markers found: "+strings.Join(p.ProtocolMarkers, " | "))
	} else {
		lines = append(lines, "Static protocol markers found: none")
	}
	if strings.TrimSpace(p.StaticInspectDetail) != "" {
		lines = append(lines, "Static inspection note: "+strings.TrimSpace(p.StaticInspectDetail))
	}

	if p.RuntimeArchitecture != "" {
		lines = append(lines, "Runtime architecture: "+p.RuntimeArchitecture)
	}
	lines = appendSummaryList(lines, "Runtime signals", p.RuntimeSignals, 20)
	lines = appendSummaryList(lines, "Process inventory", p.ProcessInventory, 24)
	lines = appendSummaryList(lines, "Child/runtime processes", p.ChildProcesses, 24)
	lines = appendSummaryList(lines, "Package identity", p.PackageIdentity, 8)
	lines = appendSummaryList(lines, "Package top-level entries", p.PackageTopLevel, 35)
	lines = appendSummaryList(lines, "App manifest extensions", p.AppExtensions, 24)
	lines = appendSummaryList(lines, "Registered activation protocols", p.ProtocolSchemes, 16)
	lines = appendSummaryList(lines, "Window classes", p.WindowClasses, 16)
	lines = appendSummaryList(lines, "Active ChatGPT network peers", p.NetworkPeers, 20)
	lines = appendSummaryList(lines, "OpenAI/ChatGPT DNS names currently cached on this PC", p.OpenAIDNSNames, 30)
	lines = appendSummaryList(lines, "App-specific protocol marker sources", p.MarkerSources, 30)
	lines = appendSummaryList(lines, "Generic/runtime marker sources ignored for connection decisions", p.NoisyMarkerSources, 20)
	lines = appendSummaryList(lines, "Electron ASAR archives opened", p.ASARArchives, 10)
	lines = appendSummaryList(lines, "ASAR app-code entries indexed", p.ASARCodeEntries, 40)
	lines = appendSummaryList(lines, "ASAR app-code protocol marker sources", p.ASARMarkerSources, 40)
	lines = appendSummaryList(lines, "ASAR IPC/bridge candidates", p.ASARIPCCandidates, 40)
	if strings.TrimSpace(p.ASARScanDetail) != "" {
		lines = append(lines, "ASAR scan note: "+strings.TrimSpace(p.ASARScanDetail))
	}
	if strings.TrimSpace(p.DirectAssessment) != "" {
		lines = append(lines, "Direct-backend assessment: "+strings.TrimSpace(p.DirectAssessment))
	}

	if p.provenTransport() {
		lines = append(lines, "Result: FlipAi proved at least one ChatGPT-owned background transport candidate. This is still only a diagnostic; ChatGPT SMS routing is not enabled yet.")
	} else if len(p.ProtocolMarkers) > 0 || p.RuntimeArchitecture != "" {
		lines = append(lines, "Result: no live ChatGPT-owned local transport is exposed. FlipAi completed the runtime/package architecture survey so the next implementation can choose the correct direct path instead of guessing or using visible UI automation.")
	} else {
		lines = append(lines, "Result: diagnostic completed, but no ChatGPT-owned direct transport was proven and no useful architecture or protocol marker was found. ChatGPT is NOT connected or enabled. Codex pipes do not count as ChatGPT Chat connectivity.")
	}
	return strings.Join(lines, "\n")
}

func (a *App) chatGPTDirectProbe(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	probe, err := platformProbeChatGPTDirect(ctx)
	if err != nil {
		activityLogForStatePath(a.statePath).Add("error", "agent", "ChatGPT direct-backend diagnostic failed: "+truncate(err.Error(), 220), "", "G", "")
		renderResult(w, r, http.StatusInternalServerError, false, "ChatGPT diagnostic failed", err.Error())
		return
	}
	if err := augmentChatGPTDirectProbe(ctx, &probe); err != nil {
		probe.RuntimeSignals = append(probe.RuntimeSignals, "architecture add-on could not complete: "+truncate(err.Error(), 180))
	}
	if err := augmentChatGPTASARProbe(ctx, &probe); err != nil {
		probe.ASARScanDetail = "ASAR add-on could not complete: " + truncate(err.Error(), 180)
	}
	probe.DirectAssessment = assessChatGPTDirectPath(probe)
	probe.LoopbackPorts = uniqueSortedInts(probe.LoopbackPorts)
	probe.CDPPorts = uniqueSortedInts(probe.CDPPorts)
	probe.ProcessNames = uniqueSortedStrings(probe.ProcessNames)
	probe.NamedPipes = uniqueSortedStrings(probe.NamedPipes)
	probe.IgnoredPipes = uniqueSortedStrings(probe.IgnoredPipes)
	probe.StaticResourceFiles = uniqueSortedStrings(probe.StaticResourceFiles)
	probe.ProtocolMarkers = uniqueSortedStrings(probe.ProtocolMarkers)
	probe.RuntimeSignals = uniqueSortedStrings(probe.RuntimeSignals)
	probe.ProcessInventory = uniqueSortedStrings(probe.ProcessInventory)
	probe.ChildProcesses = uniqueSortedStrings(probe.ChildProcesses)
	probe.PackageIdentity = uniqueSortedStrings(probe.PackageIdentity)
	probe.PackageTopLevel = uniqueSortedStrings(probe.PackageTopLevel)
	probe.AppExtensions = uniqueSortedStrings(probe.AppExtensions)
	probe.ProtocolSchemes = uniqueSortedStrings(probe.ProtocolSchemes)
	probe.WindowClasses = uniqueSortedStrings(probe.WindowClasses)
	probe.NetworkPeers = uniqueSortedStrings(probe.NetworkPeers)
	probe.OpenAIDNSNames = uniqueSortedStrings(probe.OpenAIDNSNames)
	probe.MarkerSources = uniqueSortedStrings(probe.MarkerSources)
	probe.NoisyMarkerSources = uniqueSortedStrings(probe.NoisyMarkerSources)
	probe.ASARArchives = uniqueSortedStrings(probe.ASARArchives)
	probe.ASARCodeEntries = uniqueSortedStrings(probe.ASARCodeEntries)
	probe.ASARMarkerSources = uniqueSortedStrings(probe.ASARMarkerSources)
	probe.ASARIPCCandidates = uniqueSortedStrings(probe.ASARIPCCandidates)
	message := probe.summary()
	level := "info"
	if probe.ProcessCount == 0 || (!probe.provenTransport() && len(probe.ProtocolMarkers) == 0 && probe.RuntimeArchitecture == "") {
		level = "warn"
	}
	activityLogForStatePath(a.statePath).Add(level, "agent", "ChatGPT direct-backend diagnostic: "+truncate(strings.ReplaceAll(message, "\n", " · "), 1500), "", "G", "")
	ok := probe.ProcessCount > 0 && probe.provenTransport()
	title := "ChatGPT architecture diagnostic complete"
	if ok {
		title = "ChatGPT transport candidate found"
	} else if probe.RuntimeArchitecture != "" {
		title = "ChatGPT architecture identified"
	} else if len(probe.ProtocolMarkers) > 0 {
		title = "ChatGPT protocol clues found"
	} else if probe.ProcessCount > 0 {
		title = "ChatGPT is not connected yet"
	}
	renderResult(w, r, http.StatusOK, ok, title, message)
}
