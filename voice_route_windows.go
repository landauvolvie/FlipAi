//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// The desktop AI app must use the cables, not the PC's real microphone and
// speakers -- and nobody should have to open the app's audio settings to make
// that true. Windows keeps a per-application default-device store (the same
// one Settings > App volume and device preferences writes), reachable through
// the AudioPolicyConfig factory; FlipAi writes the app's entries there itself,
// pointing its playback at the return cable and its recording at the caller
// cable. The app picks the assignment up when it next opens an audio stream,
// which is exactly when its voice mode starts.
//
// The store is keyed by the process, so the app has to be running to be
// routed. Routing is applied whenever the device list changes and again while
// the phone is still ringing, before anything opens an audio stream -- see
// startAgentVoiceSession, which is the only caller that matters. The outcome,
// including "the app is not running yet", is recorded where the Settings page
// shows it.

var (
	procVoiceGetWindowThreadProcessID = voiceUser32.NewProc("GetWindowThreadProcessId")
)

func windowProcessID(hwnd uintptr) uint32 {
	var pid uint32
	procVoiceGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// voiceRouteMu keeps concurrent routing attempts (a device report racing a
// call) from interleaving their PowerShell runs and their notes.
var voiceRouteMu sync.Mutex

// routeAgentAppAudio points one agent's desktop app at the cables. Best
// effort: every outcome is written to RoutingNote so the status page can say
// what actually happened.
func routeAgentAppAudio(dataDir string, cfg VoiceCallConfig, agent string) {
	voiceRouteMu.Lock()
	defer voiceRouteMu.Unlock()
	note := func(text string) {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) { s.RoutingNote = text })
	}
	target := voiceAgentConfig(cfg, agent)
	plan := currentVoiceCablePlan(dataDir)
	if plan.AgentInput == "" && plan.AgentOutput == "" {
		note("Not applied: " + plan.Warning)
		return
	}
	hwnd := findAgentAppWindow(agent, target)
	if hwnd == 0 {
		note(fmt.Sprintf("Waiting for the %s desktop app: its audio is routed to the cables the moment its window exists.", target.AppTitle))
		return
	}
	pid := windowProcessID(hwnd)
	if pid == 0 {
		note(fmt.Sprintf("Could not identify the %s desktop app's process, so its audio was not re-routed.", target.AppTitle))
		return
	}
	if err := setAppDefaultEndpoints(dataDir, pid, plan.AgentOutput, plan.AgentInput); err != nil {
		note(fmt.Sprintf("Windows refused the automatic audio routing for %s: %v. One-time fallback: in the app's own audio settings choose %q as its microphone and %q as its speaker.", target.AppTitle, err, plan.AgentInput, plan.AgentOutput))
		return
	}
	note(fmt.Sprintf("%s is wired to the cables: it hears the caller on %q and speaks into %q. Applied automatically; nothing to choose in the app.", target.AppTitle, plan.AgentInput, plan.AgentOutput))
}

// platformVoiceDevicesChanged runs when the Google Voice page reports a fresh
// device list: the wiring may just have become possible (a cable installed,
// the app started), so the enabled agents are re-routed in the background.
var voiceRoutePending sync.Mutex
var voiceRouteQueued bool

func platformVoiceDevicesChanged(dataDir string) {
	voiceRoutePending.Lock()
	if voiceRouteQueued {
		voiceRoutePending.Unlock()
		return
	}
	voiceRouteQueued = true
	voiceRoutePending.Unlock()
	go func() {
		defer func() {
			voiceRoutePending.Lock()
			voiceRouteQueued = false
			voiceRoutePending.Unlock()
		}()
		time.Sleep(1 * time.Second)
		cfg := loadVoiceCallConfig(dataDir)
		// The default agent's app is the one a call will reach; route it first
		// so the common case is wired before any call. The other agent is
		// routed at call time if one ever reaches it.
		routeAgentAppAudio(dataDir, cfg, cfg.DefaultAgent)
	}()
}

// setAppDefaultEndpoints writes one process's default render and capture
// endpoints into the Windows per-app audio policy store, finding the
// endpoints by their friendly names. It shells out to PowerShell because the
// store is only reachable through a WinRT factory, and the well-understood
// interop for it is C#.
func setAppDefaultEndpoints(dataDir string, pid uint32, renderLabel, captureLabel string) error {
	script := filepath.Join(dataDir, "route-app-audio.ps1")
	if err := os.WriteFile(script, []byte(routeAppAudioPS), 0600); err != nil {
		return err
	}
	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", script, "-ProcessId", strconv.FormatUint(uint64(pid), 10)}
	if renderLabel != "" {
		args = append(args, "-RenderName", renderLabel)
	}
	if captureLabel != "" {
		args = append(args, "-CaptureName", captureLabel)
	}
	cmd := exec.Command("powershell.exe", args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("%s", truncate(text, 400))
	}
	return nil
}
