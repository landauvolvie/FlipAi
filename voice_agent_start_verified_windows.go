//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"
)

// startAgentVoiceSessionVerified is the v0.46 call path. The v0.45 field test
// proved that finding the correct "Start voice chat" element is not enough:
// Windows accepted a coordinate mouse-down/up, FlipAi called that a click, but
// Chromium never entered live Voice. This path treats every activation method
// as only an attempt and verifies live Voice before it can succeed.
func startAgentVoiceSessionVerified(dataDir string, cfg VoiceCallConfig, agent string) error {
	target := voiceAgentConfig(cfg, agent)
	hwnd, err := ensureAgentAppWindow(agent, target)
	if err != nil {
		routeAgentAppAudio(dataDir, cfg, agent)
		return err
	}
	bringToFront(hwnd)

	// Try to establish the route before Voice opens its streams. Electron may
	// not have created the actual audio child process yet; a second routing pass
	// is therefore mandatory after Voice is confirmed active.
	routeAgentAppAudio(dataDir, cfg, agent)

	accessibilityRestarted := false
	if state, stateErr := readAgentVoiceState(hwnd); stateErr == nil {
		recordAgentVoiceObservation(dataDir, target.AppTitle, state)
		if state.Active {
			_ = stopAgentVoiceSession(cfg, agent)
			if h, ensureErr := ensureAgentAppWindow(agent, target); ensureErr == nil {
				hwnd = h
				bringToFront(hwnd)
			}
		} else if shouldRestartAgentForAccessibility(target.AppTitle, state) {
			state.Result = "restarting-for-accessibility"
			recordAgentVoiceObservation(dataDir, target.AppTitle, state)
			h, restartErr := restartAgentAppForAccessibility(agent, target, hwnd)
			if restartErr != nil {
				return fmt.Errorf("%s was running with renderer accessibility off and FlipAi could not restart it for voice: %w", target.AppTitle, restartErr)
			}
			hwnd = h
			accessibilityRestarted = true
			bringToFront(hwnd)
			routeAgentAppAudio(dataDir, cfg, agent)
		}
	}

	deadline := time.Now().Add(agentVoiceStartTimeout)
	method := 0
	var last agentVoiceState
	var lastErr error

	for time.Now().Before(deadline) {
		if method >= len(agentVoiceStartActions) {
			break
		}
		action := agentVoiceStartActions[method]
		state, actionErr := runAgentVoiceAction(hwnd, action)
		if actionErr != nil {
			lastErr = actionErr
			time.Sleep(agentVoicePoll)
			continue
		}
		last = state
		recordAgentVoiceObservation(dataDir, target.AppTitle, state)

		if state.Active || state.Result == "already-active" {
			return completeVerifiedAgentVoiceStart(dataDir, cfg, agent)
		}

		if !accessibilityRestarted && shouldRestartAgentForAccessibility(target.AppTitle, state) {
			state.Result = "restarting-for-accessibility"
			recordAgentVoiceObservation(dataDir, target.AppTitle, state)
			h, restartErr := restartAgentAppForAccessibility(agent, target, hwnd)
			if restartErr != nil {
				lastErr = fmt.Errorf("could not restart %s with renderer accessibility enabled: %w", target.AppTitle, restartErr)
				time.Sleep(agentVoicePoll)
				continue
			}
			hwnd = h
			accessibilityRestarted = true
			method = 0
			bringToFront(hwnd)
			routeAgentAppAudio(dataDir, cfg, agent)
			deadline = time.Now().Add(agentVoiceStartTimeout)
			continue
		}

		if state.Result == "not-found" {
			// The UIA scan already waited for Chromium's tree. Only use an app's
			// configured shortcut when it genuinely has no visible Voice control.
			if target.VoiceShortcut != "" {
				if shortcutErr := sendAgentVoiceShortcut(target); shortcutErr != nil {
					lastErr = shortcutErr
				} else if waitForAgentVoice(hwnd, true, agentVoiceAttemptDeadline(deadline)) {
					return completeVerifiedAgentVoiceStart(dataDir, cfg, agent)
				}
			}
			time.Sleep(agentVoicePoll)
			continue
		}

		// A "*-sent" result says only that Windows accepted that operation. It
		// is not success until a second UIA read sees the live-Voice end control.
		// If it did nothing, move to a genuinely different activation mechanism
		// instead of spending the whole 45-second budget believing one mouse
		// command was a click.
		if strings.HasSuffix(state.Result, "-sent") {
			if waitForAgentVoice(hwnd, true, agentVoiceAttemptDeadline(deadline)) {
				return completeVerifiedAgentVoiceStart(dataDir, cfg, agent)
			}
		}
		method++
		time.Sleep(150 * time.Millisecond)
	}

	// One final read catches a transition that completed just after the last
	// method's short confirmation window without pressing anything a second time.
	if state, stateErr := readAgentVoiceState(hwnd); stateErr == nil {
		last = state
		recordAgentVoiceObservation(dataDir, target.AppTitle, state)
		if state.Active {
			return completeVerifiedAgentVoiceStart(dataDir, cfg, agent)
		}
	}
	if lastErr != nil && !last.Found {
		return fmt.Errorf("could not drive %s: %w", target.AppTitle, lastErr)
	}
	return agentVoiceStartFailure(target.AppTitle, last)
}

// completeVerifiedAgentVoiceStart deliberately routes again after Voice is
// active. That is when Electron's audio service/renderer process definitely
// exists, so the Windows per-app policy can be written against the process that
// actually owns the audio session rather than only the top-level window PID.
func completeVerifiedAgentVoiceStart(dataDir string, cfg VoiceCallConfig, agent string) error {
	routeAgentAppAudio(dataDir, cfg, agent)
	return nil
}
