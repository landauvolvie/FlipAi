//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// This guard exists for the real Microsoft Edge Google Voice receiver. Google
// Voice's Answer button can reject a synthetic DOM .click() even though FlipAi
// can see the ring. The guard watches the already-authorized ring state and
// invokes the visible Answer control through Windows UI Automation, which is
// the same accessibility path a real user action uses.
//
// It also watches the manually-answered state. If the older DevTools detector
// misses the transition into a live call, the desktop agent is started once so
// a caller is never left connected to silence.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--google-voice" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	go runGoogleVoiceAnswerGuard(dataDir)
}

type voiceUIAState struct {
	Ringing bool
	Active  bool
}

func runGoogleVoiceAnswerGuard(dataDir string) {
	ticker := time.NewTicker(450 * time.Millisecond)
	defer ticker.Stop()

	var handledRing time.Time
	var fallbackAgent string
	var fallbackStarted bool
	var inactiveTicks int

	for range ticker.C {
		if quitRequested(dataDir) {
			return
		}

		hwnd := googleVoiceHWND()
		if hwnd == 0 {
			continue
		}

		rt := loadVoiceRuntime(dataDir)
		state, _ := googleVoiceUIAState(hwnd)

		// The normal receiver has already parsed caller ID and made the allowlist
		// decision before it writes authorized-call-ringing. Do not duplicate
		// authorization here; this watcher only makes the accepted call actually
		// get picked up.
		if rt.LastEvent == "authorized-call-ringing" && !rt.LastRingAt.IsZero() && rt.LastRingAt.After(handledRing) {
			if time.Since(rt.LastRingAt) < 20*time.Second {
				if err := invokeGoogleVoiceAnswerUIA(hwnd); err == nil {
					handledRing = rt.LastRingAt
					mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
						s.LastEvent = "answer-clicked-accessibly"
						s.LastError = ""
					})
				}
			}
		}

		// Give the normal bridge first chance to notice that Google Voice moved
		// from ringing to a live call. If it does, it owns activation/teardown.
		// The fallback only starts after the browser is clearly active while the
		// runtime still says no call was bridged.
		rt = loadVoiceRuntime(dataDir)
		if !fallbackStarted && !rt.InCall && state.Active && !state.Ringing && rt.Agent != "" {
			if !rt.LastRingAt.IsZero() && time.Since(rt.LastRingAt) < 30*time.Second {
				cfg := loadVoiceCallConfig(dataDir)
				if err := activateAgentVoiceWithRouting(dataDir, cfg, rt.Agent); err == nil {
					fallbackStarted = true
					fallbackAgent = rt.Agent
					inactiveTicks = 0
					mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
						s.InCall = true
						s.Blocked = ""
						s.LastEvent = "call-bridged-accessibility-fallback"
						s.LastError = ""
					})
				} else {
					mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
						s.LastError = "Google Voice answered, but FlipAi could not start desktop Voice: " + err.Error()
						s.LastEvent = "agent-voice-error"
					})
				}
			}
		}

		if fallbackStarted {
			if state.Active {
				inactiveTicks = 0
				continue
			}
			inactiveTicks++
			if inactiveTicks < 4 {
				continue
			}
			cfg := loadVoiceCallConfig(dataDir)
			_ = deactivateAgentVoice(cfg, fallbackAgent)
			fallbackStarted = false
			fallbackAgent = ""
			inactiveTicks = 0
			mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
				s.InCall = false
				s.Caller = ""
				s.CallerLabel = ""
				s.Agent = ""
				s.LastEvent = "call-ended"
			})
		}
	}
}

func invokeGoogleVoiceAnswerUIA(hwnd uintptr) error {
	if hwnd == 0 {
		return errors.New("Google Voice window is not available")
	}
	out, err := runVoiceUIAScript(hwnd, "answer")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "clicked" {
		return errors.New("the incoming call was visible, but Windows accessibility could not find an enabled Answer control")
	}
	return nil
}

func googleVoiceUIAState(hwnd uintptr) (voiceUIAState, error) {
	if hwnd == 0 {
		return voiceUIAState{}, errors.New("Google Voice window is not available")
	}
	out, err := runVoiceUIAScript(hwnd, "state")
	if err != nil {
		return voiceUIAState{}, err
	}
	var s voiceUIAState
	for _, part := range strings.Fields(strings.ToLower(out)) {
		switch part {
		case "ringing":
			s.Ringing = true
		case "active":
			s.Active = true
		}
	}
	return s, nil
}

func runVoiceUIAScript(hwnd uintptr, action string) (string, error) {
	// UI Automation is intentionally used instead of screen coordinates. The
	// Google Voice panel can be docked, resized, moved, or on another DPI scale;
	// the accessible button remains the same control.
	script := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
$root = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]%d)
if ($null -eq $root) { Write-Output 'no-root'; exit 2 }
$buttons = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants,
  (New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ControlTypeProperty,[System.Windows.Automation.ControlType]::Button)))
$ringing = $false
$active = $false
$answer = $null
foreach ($b in $buttons) {
  $name = '' + $b.Current.Name
  $id = '' + $b.Current.AutomationId
  $help = '' + $b.Current.HelpText
  $text = ($name + ' ' + $id + ' ' + $help).Trim()
  if ($text -match '(?i)(answer|accept|pick\s*up|take\s+call)') {
    $ringing = $true
    if ($null -eq $answer -and $b.Current.IsEnabled) { $answer = $b }
  }
  if ($text -match '(?i)(hang\s*up|end\s+(the\s+)?call|disconnect|leave\s+call)') { $active = $true }
}
if ('%s' -eq 'state') {
  $parts = @()
  if ($ringing) { $parts += 'ringing' }
  if ($active) { $parts += 'active' }
  Write-Output ($parts -join ' ')
  exit 0
}
if ($null -eq $answer) { Write-Output 'not-found'; exit 3 }
try {
  $p = $answer.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
  $p.Invoke()
  Write-Output 'clicked'
  exit 0
} catch {
  try {
    $legacy = $answer.GetCurrentPattern([System.Windows.Automation.LegacyIAccessiblePattern]::Pattern)
    $legacy.DoDefaultAction()
    Write-Output 'clicked'
    exit 0
  } catch {
    Write-Output 'invoke-failed'
    exit 4
  }
}
`, hwnd, action)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	hideWindow(cmd)
	b, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(b))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("Google Voice accessibility action failed: %s", truncate(text, 300))
	}
	return text, nil
}
