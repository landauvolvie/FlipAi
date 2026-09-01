package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// chatGPTDirectProbeResult is intentionally metadata-only. The first direct
// ChatGPT experiment must discover a supported/local transport without copying
// browser cookies, bearer tokens, or the desktop app's private credentials.
type chatGPTDirectProbeResult struct {
	Supported     bool     `json:"supported"`
	ProcessCount  int      `json:"processCount"`
	ProcessNames  []string `json:"processNames,omitempty"`
	LoopbackPorts []int    `json:"loopbackPorts,omitempty"`
	CDPPorts      []int    `json:"cdpPorts,omitempty"`
	NamedPipes    []string `json:"namedPipes,omitempty"`
	DebugPipe     bool     `json:"debugPipe,omitempty"`
	Detail        string   `json:"detail,omitempty"`
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

func (p chatGPTDirectProbeResult) summary() string {
	if !p.Supported {
		if p.Detail != "" {
			return p.Detail
		}
		return "Direct ChatGPT discovery is only available on Windows."
	}
	if p.ProcessCount == 0 {
		return "ChatGPT desktop was not found running. Open the ChatGPT desktop app, sign in, then run the probe again."
	}
	lines := []string{
		fmt.Sprintf("ChatGPT desktop processes: %d", p.ProcessCount),
		"Loopback listeners owned by ChatGPT: " + joinInts(p.LoopbackPorts),
		"Chromium DevTools listeners: " + joinInts(p.CDPPorts),
	}
	if p.DebugPipe {
		lines = append(lines, "Chromium remote-debugging pipe: advertised")
	} else {
		lines = append(lines, "Chromium remote-debugging pipe: not advertised")
	}
	if len(p.NamedPipes) > 0 {
		lines = append(lines, "Relevant named pipes: "+strings.Join(p.NamedPipes, ", "))
	} else {
		lines = append(lines, "Relevant named pipes: none found")
	}
	if len(p.CDPPorts) > 0 || p.DebugPipe || len(p.NamedPipes) > 0 || len(p.LoopbackPorts) > 0 {
		lines = append(lines, "Result: FlipAi found at least one background transport candidate. The next step is protocol identification; SMS routing is intentionally not enabled yet.")
	} else {
		lines = append(lines, "Result: the desktop app is running, but it exposed no obvious local transport. The next diagnostic will inspect app IPC without using accessibility or stealing focus.")
	}
	return strings.Join(lines, "\n")
}

func (a *App) chatGPTDirectProbe(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	probe, err := platformProbeChatGPTDirect(ctx)
	if err != nil {
		activityLogForStatePath(a.statePath).Add("error", "agent", "ChatGPT direct-backend probe failed: "+truncate(err.Error(), 220), "", "G", "")
		renderResult(w, r, http.StatusInternalServerError, false, "ChatGPT direct probe failed", err.Error())
		return
	}
	probe.LoopbackPorts = uniqueSortedInts(probe.LoopbackPorts)
	probe.CDPPorts = uniqueSortedInts(probe.CDPPorts)
	probe.ProcessNames = uniqueSortedStrings(probe.ProcessNames)
	probe.NamedPipes = uniqueSortedStrings(probe.NamedPipes)
	message := probe.summary()
	level := "info"
	if probe.ProcessCount == 0 {
		level = "warn"
	}
	activityLogForStatePath(a.statePath).Add(level, "agent", "ChatGPT direct-backend probe: "+truncate(strings.ReplaceAll(message, "\n", " · "), 500), "", "G", "")
	ok := probe.ProcessCount > 0 && (len(probe.LoopbackPorts) > 0 || len(probe.NamedPipes) > 0 || probe.DebugPipe)
	renderResult(w, r, http.StatusOK, ok, "ChatGPT direct backend probe", message)
}
