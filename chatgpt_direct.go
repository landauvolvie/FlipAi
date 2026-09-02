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
	Detail              string   `json:"detail,omitempty"`
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

	if p.provenTransport() {
		lines = append(lines, "Result: FlipAi proved at least one ChatGPT-owned background transport candidate. This is still only a diagnostic; ChatGPT SMS routing is not enabled yet.")
	} else if len(p.ProtocolMarkers) > 0 {
		lines = append(lines, "Result: no live ChatGPT-owned local transport is exposed, but FlipAi found protocol clues in the installed desktop app package. ChatGPT is still NOT connected; the next step is to identify the request/IPC shape from these static clues without touching the visible UI or credentials.")
	} else {
		lines = append(lines, "Result: diagnostic completed, but no ChatGPT-owned direct transport was proven and no useful static protocol marker was found. ChatGPT is NOT connected or enabled. Codex pipes do not count as ChatGPT Chat connectivity.")
	}
	return strings.Join(lines, "\n")
}

func (a *App) chatGPTDirectProbe(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	probe, err := platformProbeChatGPTDirect(ctx)
	if err != nil {
		activityLogForStatePath(a.statePath).Add("error", "agent", "ChatGPT direct-backend diagnostic failed: "+truncate(err.Error(), 220), "", "G", "")
		renderResult(w, r, http.StatusInternalServerError, false, "ChatGPT diagnostic failed", err.Error())
		return
	}
	probe.LoopbackPorts = uniqueSortedInts(probe.LoopbackPorts)
	probe.CDPPorts = uniqueSortedInts(probe.CDPPorts)
	probe.ProcessNames = uniqueSortedStrings(probe.ProcessNames)
	probe.NamedPipes = uniqueSortedStrings(probe.NamedPipes)
	probe.IgnoredPipes = uniqueSortedStrings(probe.IgnoredPipes)
	probe.StaticResourceFiles = uniqueSortedStrings(probe.StaticResourceFiles)
	probe.ProtocolMarkers = uniqueSortedStrings(probe.ProtocolMarkers)
	message := probe.summary()
	level := "info"
	if probe.ProcessCount == 0 || (!probe.provenTransport() && len(probe.ProtocolMarkers) == 0) {
		level = "warn"
	}
	activityLogForStatePath(a.statePath).Add(level, "agent", "ChatGPT direct-backend diagnostic: "+truncate(strings.ReplaceAll(message, "\n", " · "), 900), "", "G", "")
	ok := probe.ProcessCount > 0 && probe.provenTransport()
	title := "ChatGPT diagnostic complete"
	if ok {
		title = "ChatGPT transport candidate found"
	} else if len(probe.ProtocolMarkers) > 0 {
		title = "ChatGPT protocol clues found"
	} else if probe.ProcessCount > 0 {
		title = "ChatGPT is not connected yet"
	}
	renderResult(w, r, http.StatusOK, ok, title, message)
}
