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

// The Microsoft Edge receiver can see a Google Voice ring through DevTools,
// but Google Voice sometimes ignores a synthetic DOM element.click(). This
// narrow fallback invokes the already-authorized Answer control through
// Windows UI Automation — the same accessibility action a keyboard/screen
// reader uses. It only wakes while a fresh authorized ring exists, so there is
// no polling PowerShell process during normal idle operation.
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
	ticker := time.NewTicker(650 * time.Millisecond)
	defer ticker.Stop()

	var handledRing time.Time
	var fallbackAgent string
	var fallbackStarted bool
	var inactiveTicks int
	var lastStateCheck time.Time

	for range ticker.C {
		if quitRequested(dataDir) {
			return
		}
		rt := loadVoiceRuntime(dataDir)
		freshRing := !rt.LastRingAt.IsZero() && time.Since(rt.LastRingAt) >= 0 && time.Since(rt.LastRingAt) < 35*time.Second
		needsAnswer := freshRing && rt.LastRingAt.After(handledRing) && rt.LastEvent == "authorized-call-ringing"
		needsState := freshRing && !rt.InCall
		if fallbackStarted && time.Since(lastStateCheck) >= 2*time.Second {
			needsState = true
		}
		if !needsAnswer && !needsState {
			continue
		}

		hwnd := googleVoiceHWND()
		if hwnd == 0 {
			continue
		}

		if needsAnswer {
			if err := invokeGoogleVoiceAnswerUIA(hwnd); err == nil {
				handledRing = rt.LastRingAt
				mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
					s.LastEvent = "answer-clicked-accessibly"
					// Preserve a useful audio-path error if one already exists; the
					// answer action itself succeeded and should not replace it.
				})
			} else {
				// Keep trying this ring. Google Voice can create its visible card a
				// fraction of a second before the accessible button arrives.
				mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
					if s.LastEvent == "authorized-call-ringing" {
						s.LastError = "Incoming call is authorized, but FlipAi has not been able to press Answer yet: " + err.Error()
					}
				})
			}
		}

		// The ordinary Edge detector should see the live-call controls and call
		// bridge.Answered. This is only the fallback for the real-world case seen
		// in the recording where the user answered by hand but the desktop app
		// never started.
		if needsState {
			lastStateCheck = time.Now()
			state, _ := googleVoiceUIAState(hwnd)
			rt = loadVoiceRuntime(dataDir)
			if !fallbackStarted && freshRing && !rt.InCall && state.Active && !state.Ringing && rt.Agent != "" {
				cfg := loadVoiceCallConfig(dataDir)
				if err := activateAgentVoiceWithRouting(dataDir, cfg, rt.Agent); err == nil {
					fallbackStarted = true
					fallbackAgent = rt.Agent
					inactiveTicks = 0
					mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
						s.InCall = true
						s.Blocked = ""
						s.LastEvent = "call-bridged-accessibility-fallback"
						if currentVoiceCablePlan(dataDir).Warning == "" {
							s.LastError = ""
						}
					})
				} else if err != nil {
					mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
						s.LastError = "Google Voice answered, but FlipAi could not start desktop Voice: " + err.Error()
						s.LastEvent = "agent-voice-error"
					})
				}
			}

			if fallbackStarted {
				if state.Active {
					inactiveTicks = 0
					continue
				}
				inactiveTicks++
				if inactiveTicks < 2 {
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
		return errors.New("the incoming call is visible, but Windows accessibility cannot find an enabled Answer control")
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
	// UI Automation is deliberately coordinate-free. The panel can be docked,
	// resized, moved, or displayed at a different DPI and the same accessible
	// Answer control is still invoked.
	script := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
$root = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]%d)
if ($null -eq $root) { Write-Output 'no-root'; exit 2 }
$all = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants,[System.Windows.Automation.Condition]::TrueCondition)
$ringing = $false
$active = $false
$answer = $null
$hasHold = $false
$hasMute = $false
$hasKeypad = $false
foreach ($e in $all) {
  $name = '' + $e.Current.Name
  $id = '' + $e.Current.AutomationId
  $help = '' + $e.Current.HelpText
  $text = ($name + ' ' + $id + ' ' + $help).Trim()
  if ($text -match '(?i)(incoming\s+call|answer|accept|pick\s*up|take\s+call)') { $ringing = $true }
  if ($text -match '(?i)(hang\s*up|end\s+(the\s+)?call|disconnect|leave\s+call)') { $active = $true }
  if ($text -match '(?i)\bhold\b') { $hasHold = $true }
  if ($text -match '(?i)\bmute\b') { $hasMute = $true }
  if ($text -match '(?i)\bkeypad\b') { $hasKeypad = $true }
  if ($null -eq $answer -and $e.Current.ControlType -eq [System.Windows.Automation.ControlType]::Button -and $e.Current.IsEnabled -and $text -match '(?i)(answer|accept|pick\s*up|take\s+call)') { $answer = $e }
}
if (-not $ringing -and $hasHold -and $hasMute -and $hasKeypad) { $active = $true }
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
