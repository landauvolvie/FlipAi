package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Starting the desktop app's voice mode used to be a one-shot: press whatever
// looks like a Voice control and call the call bridged. That is how a call
// could be "connected" with the desktop app sitting there in text mode. There
// was no check that voice mode had started, no check that it had stopped, and
// two independent retry loops that could each press it again.
//
// This file is the single, checked way in and out of voice mode. Every action
// goes through one Windows UI Automation script whose output is a small
// key=value report, so what FlipAi did and what it saw are both testable
// without Windows.

// agentVoiceState is what the desktop app's accessibility tree says about its
// voice mode.
type agentVoiceState struct {
	// Found is whether the app's window could be read at all.
	Found bool
	// Active is whether a voice conversation is running right now.
	Active bool
	// StartControl and EndControl are the accessible names of the controls
	// that would start and end voice mode, empty when no such control exists.
	// They are reported so a machine whose app does not expose one can say so
	// in words instead of failing silently.
	StartControl string
	EndControl   string
	// Result is what an action did: clicked, not-found, invoke-failed.
	Result string
	// Controls is a short list of what the window offered, for the status page
	// when nothing matched.
	Controls []string
}

// voiceAgentActions are the only actions the script accepts. Nothing outside
// this list is ever interpolated into PowerShell.
var voiceAgentActions = map[string]bool{"state": true, "start": true, "stop": true}

// parseAgentVoiceReport turns the script's key=value output into state.
func parseAgentVoiceReport(out string) agentVoiceState {
	var s agentVoiceState
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "found":
			s.Found = value == "1"
		case "active":
			s.Active = value == "1"
		case "start":
			s.StartControl = value
		case "end":
			s.EndControl = value
		case "result":
			s.Result = value
		case "control":
			if value != "" && len(s.Controls) < 24 {
				s.Controls = append(s.Controls, value)
			}
		}
	}
	return s
}

// agentVoiceStartFailure explains, in the words the status page shows, why a
// voice session did not start. It is deliberately specific: "could not start
// voice" with nothing attached is what left users guessing.
func agentVoiceStartFailure(appTitle string, s agentVoiceState) error {
	switch {
	case !s.Found:
		return fmt.Errorf("FlipAi could not read the %s window through Windows accessibility, so it could not start voice mode", appTitle)
	case s.Active:
		return nil
	case s.StartControl == "" && len(s.Controls) == 0:
		return fmt.Errorf("the %s window exposes no controls to Windows accessibility yet; it may still be starting up", appTitle)
	case s.StartControl == "":
		return fmt.Errorf("FlipAi could not find a Voice control in %s. It offered: %s. Set the app's Voice keyboard shortcut in FlipAi to drive it directly", appTitle, truncate(strings.Join(s.Controls, ", "), 240))
	case s.Result == "invoke-failed":
		return fmt.Errorf("Windows refused to press %q in %s", s.StartControl, appTitle)
	}
	return fmt.Errorf("FlipAi pressed %q in %s but it did not enter voice mode", s.StartControl, appTitle)
}

// voiceAgentUIAScript builds the PowerShell that reads or drives one window's
// voice mode.
//
// The script is coordinate-free on purpose. The desktop app can be moved,
// resized, shown on a different display or at a different scale factor, and the
// same accessible control is still the one invoked -- which is the difference
// between an automation that works on the developer's machine and one that
// works on the user's.
func voiceAgentUIAScript(hwnd uintptr, action string) (string, error) {
	if hwnd == 0 {
		return "", errors.New("no desktop app window to drive")
	}
	if !voiceAgentActions[action] {
		return "", fmt.Errorf("unknown voice action %q", action)
	}
	script := strings.ReplaceAll(voiceAgentUIATemplate, "__HWND__", strconv.FormatUint(uint64(hwnd), 10))
	return strings.ReplaceAll(script, "__ACTION__", action), nil
}

// voiceAgentUIATemplate walks the desktop app's accessibility tree once and
// reports what it found before doing anything, so a failure always comes with
// the list of controls the app actually offered.
//
// Matching is name-based across Name, AutomationId and HelpText because desktop
// AI apps render their Voice control as a plain icon at least as often as a
// labelled button. Ending is matched first and separately: pressing something
// that says both "voice" and "end" to start a call would hang it up.
const voiceAgentUIATemplate = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$root = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]__HWND__)
if ($null -eq $root) { Write-Output 'found=0'; Write-Output 'result=no-window'; exit 0 }
Write-Output 'found=1'

$startEl = $null
$endEl = $null
$startName = ''
$endName = ''
$active = $false
$controls = New-Object System.Collections.Generic.List[string]

# The Codex and ChatGPT desktop apps are Chromium/Electron. Chromium does not
# build its UI Automation tree until a client has attached, and it does not
# build it instantly: the very first query after attaching sees only the
# top-level window, which is exactly the "it offered: ChatGPT" and nothing else
# that left voice mode never starting. So this keeps ONE client alive and
# re-scans until the web content shows up, instead of spawning a fresh client
# that asks once and gives up. A scan is judged populated once it has found a
# voice control, found that voice is already running, or simply seen more than
# the handful of native window elements.
$deadline = (Get-Date).AddSeconds(9)
while ($true) {
  $startEl = $null
  $endEl = $null
  $startName = ''
  $endName = ''
  $active = $false
  $controls.Clear()
  $all = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, [System.Windows.Automation.Condition]::TrueCondition)
  foreach ($e in $all) {
    try {
      $name = '' + $e.Current.Name
      $id = '' + $e.Current.AutomationId
      $help = '' + $e.Current.HelpText
      $enabled = $e.Current.IsEnabled
    } catch { continue }
    $text = ($name + ' ' + $id + ' ' + $help).Trim()
    if ($text -eq '') { continue }

    # A control that ends voice is the clearest sign voice is running.
    if ($text -match '(?i)(end|stop|exit|leave|close|hang)[\s_-]*(the\s+)?(voice|conversation|call|chat|talk)' -or
        $text -match '(?i)(voice|conversation)[\s_-]*(mode\s+)?(end|stop|exit|leave|close)') {
      $active = $true
      if ($null -eq $endEl -and $enabled) { $endEl = $e; $endName = $text }
      continue
    }

    if ($text -match '(?i)(start\s+voice|voice\s+mode|voice\s+chat|voice\s+conversation|use\s+voice|talk\s+to|advanced\s+voice|\bvoice\b|\bmicrophone\b|\bmic\b|\bdictat)') {
      if ($text -match '(?i)(setting|settings|input|output|device|volume|permission|help|learn|mute|unmute)') { continue }
      if ($null -eq $startEl -and $enabled) { $startEl = $e; $startName = $text }
    }

    if ($controls.Count -lt 24 -and $name -ne '' -and $name.Length -le 60) { $controls.Add($name) }
  }
  if ($active -or $null -ne $startEl -or $all.Count -gt 4) { break }
  if ((Get-Date) -ge $deadline) { break }
  Start-Sleep -Milliseconds 700
}

foreach ($c in $controls) { Write-Output ('control=' + $c) }
if ($active) { Write-Output 'active=1' } else { Write-Output 'active=0' }
Write-Output ('start=' + $startName)
Write-Output ('end=' + $endName)

if ('__ACTION__' -eq 'state') { Write-Output 'result=read'; exit 0 }

$target = $null
if ('__ACTION__' -eq 'start') {
  if ($active) { Write-Output 'result=already-active'; exit 0 }
  $target = $startEl
} else {
  if (-not $active) { Write-Output 'result=already-stopped'; exit 0 }
  $target = $endEl
}
if ($null -eq $target) { Write-Output 'result=not-found'; exit 0 }

$done = $false
try { $target.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke(); $done = $true } catch {}
if (-not $done) {
  try { $target.GetCurrentPattern([System.Windows.Automation.LegacyIAccessiblePattern]::Pattern).DoDefaultAction(); $done = $true } catch {}
}
if (-not $done) {
  try { $target.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern).Toggle(); $done = $true } catch {}
}
if (-not $done) {
  try { $target.GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern).Select(); $done = $true } catch {}
}
if ($done) { Write-Output 'result=clicked' } else { Write-Output 'result=invoke-failed' }
exit 0
`

// agentAppTitles are the window-title fragments FlipAi looks for when an agent
// has not been told which window to drive.
//
// The Codex desktop app and the ChatGPT desktop app are both shipped by OpenAI
// and both carry the voice mode a caller talks to; which one is installed
// differs from machine to machine, so both are searched, most specific first.
// A configured title always wins over this list.
func agentAppTitles(agent string) []string {
	if agent == "A" {
		return []string{"Claude"}
	}
	return []string{"Codex", "ChatGPT"}
}

// agentAppShortcutNames are the Start Menu shortcut names that open the
// desktop app.
//
// Looking the app up by its shortcut is what makes this work on a machine
// FlipAi has never seen. A desktop AI app can be a per-user install, a
// machine-wide install, or a Store package whose executable lives in a folder
// nothing is allowed to run from directly -- and all three put a shortcut with
// the same name in the Start Menu.
func agentAppShortcutNames(agent string) []string {
	if agent == "A" {
		return []string{"Claude"}
	}
	return []string{"Codex", "ChatGPT"}
}

// agentAppExecutables are the direct paths to try before falling back to a
// shortcut, most specific first. The roots are passed in rather than read from
// the environment so the list is testable.
func agentAppExecutables(agent, localAppData, programFiles, programFilesX86 string) []string {
	type layout struct{ parts []string }
	var layouts []layout
	if agent == "A" {
		layouts = []layout{
			{[]string{"Programs", "Claude", "Claude.exe"}},
			{[]string{"AnthropicClaude", "Claude.exe"}},
			{[]string{"Claude", "Claude.exe"}},
		}
	} else {
		layouts = []layout{
			{[]string{"Programs", "Codex", "Codex.exe"}},
			{[]string{"Programs", "codex", "Codex.exe"}},
			{[]string{"OpenAI", "Codex", "Codex.exe"}},
			{[]string{"Codex", "Codex.exe"}},
			{[]string{"Programs", "ChatGPT", "ChatGPT.exe"}},
			{[]string{"OpenAI", "ChatGPT", "ChatGPT.exe"}},
			{[]string{"ChatGPT", "ChatGPT.exe"}},
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, root := range []string{localAppData, programFiles, programFilesX86} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		for _, l := range layouts {
			p := joinPathParts(root, l.parts)
			if key := strings.ToLower(p); !seen[key] {
				seen[key] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// joinPathParts is filepath.Join with the separator fixed to Windows, so the
// candidate list is the same whichever OS the test runs on.
func joinPathParts(root string, parts []string) string {
	all := append([]string{strings.TrimRight(root, `\/`)}, parts...)
	return strings.Join(all, `\`)
}

// googleVoiceAnswerUIAScript presses Answer in the Google Voice window through
// Windows accessibility.
//
// This is the last rung of the answer ladder and the only one that does not go
// through the page at all. A browser exposes its rendered buttons to Windows
// accessibility, so this reaches the same control a screen-reader user would
// press -- which still works when the page has stopped running FlipAi's script
// and when a scripted click is being ignored.
func googleVoiceAnswerUIAScript(hwnd uintptr) (string, error) {
	if hwnd == 0 {
		return "", errors.New("the Google Voice window is not available")
	}
	return strings.ReplaceAll(googleVoiceAnswerUIATemplate, "__HWND__", strconv.FormatUint(uint64(hwnd), 10)), nil
}

const googleVoiceAnswerUIATemplate = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$root = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]__HWND__)
if ($null -eq $root) { Write-Output 'found=0'; Write-Output 'result=no-window'; exit 0 }
Write-Output 'found=1'
$all = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, [System.Windows.Automation.Condition]::TrueCondition)
$answer = $null
$listed = 0
foreach ($e in $all) {
  $name = '' + $e.Current.Name
  $text = ($name + ' ' + ('' + $e.Current.AutomationId) + ' ' + ('' + $e.Current.HelpText)).Trim()
  if ($text -eq '') { continue }
  if ($listed -lt 24 -and $name -ne '' -and $name.Length -le 60) { Write-Output ('control=' + $name); $listed = $listed + 1 }
  if ($null -ne $answer) { continue }
  # Decline, Ignore and Send to voicemail sit right beside Answer. Pressing one
  # of those would be worse than not answering at all.
  if ($text -match '(?i)(decline|reject|ignore|dismiss|voicemail|block|spam)') { continue }
  if ($text -notmatch '(?i)(answer|accept|pick\s*up|take\s+call)') { continue }
  if (-not $e.Current.IsEnabled) { continue }
  $answer = $e
}
if ($null -eq $answer) { Write-Output 'result=not-found'; exit 0 }
$done = $false
try { $answer.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke(); $done = $true } catch {}
if (-not $done) {
  try { $answer.GetCurrentPattern([System.Windows.Automation.LegacyIAccessiblePattern]::Pattern).DoDefaultAction(); $done = $true } catch {}
}
if ($done) { Write-Output 'result=clicked' } else { Write-Output 'result=invoke-failed' }
exit 0
`
