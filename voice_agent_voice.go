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
	// Result is what an action did: read, not-found, already-active, or the
	// concrete activation method that Windows accepted/refused.
	Result string
	// Controls is a short list of what the window offered, for the status page
	// when nothing matched.
	Controls []string
}

// voiceAgentActions are the only actions the script accepts. Nothing outside
// this list is ever interpolated into PowerShell. The separate start actions
// matter: a successful Win32 mouse call only means Windows accepted mouse input,
// not that Chromium handled the button. The caller verifies each method before
// moving to the next one.
var voiceAgentActions = map[string]bool{
	"state":          true,
	"start":          true, // compatibility alias for start-invoke
	"start-invoke":   true,
	"start-keyboard": true,
	"start-legacy":   true,
	"start-pointer":  true,
	"stop":           true,
}

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
	case s.StartControl == "" && onlyWindowChrome(appTitle, s.Controls):
		return fmt.Errorf("%s is only exposing its window frame to Windows accessibility (%s), not its contents, so FlipAi cannot see the voice control. This is a Chromium app with accessibility off. Quit %s completely -- including from the system tray -- and let FlipAi reopen it, so it starts with accessibility enabled", appTitle, truncate(strings.Join(s.Controls, ", "), 120), appTitle)
	case s.StartControl == "":
		return fmt.Errorf("FlipAi could not find the voice control in %s. It offered: %s. FlipAi now requires the actual live Voice control such as \"Start new voice chat\" or Voice Mode and deliberately ignores dictation and text-message microphone controls", appTitle, truncate(strings.Join(s.Controls, ", "), 240))
	case strings.HasSuffix(s.Result, "-failed"):
		return fmt.Errorf("Windows could see %q in %s, but none of FlipAi's verified activation methods started live Voice (last result: %s)", s.StartControl, appTitle, s.Result)
	}
	return fmt.Errorf("FlipAi activated %q in %s (%s) but it did not enter voice mode; live Voice never became active", s.StartControl, appTitle, s.Result)
}

func onlyWindowChrome(appTitle string, controls []string) bool {
	if len(controls) == 0 {
		return false
	}
	title := strings.ToLower(strings.TrimSpace(appTitle))
	for _, c := range controls {
		l := strings.ToLower(strings.TrimSpace(c))
		switch l {
		case "minimize", "maximize", "restore", "close", "system", "application", "more options":
			continue
		}
		if strings.Contains(l, "minimize") || strings.Contains(l, "maximize") || strings.Contains(l, "close") || strings.Contains(l, "restore") {
			continue
		}
		if title != "" && (l == title || strings.Contains(l, title) || strings.Contains(title, l)) {
			continue
		}
		return false
	}
	return true
}

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

const voiceAgentUIATemplate = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -AssemblyName System.Windows.Forms
Add-Type -Namespace FlipWin -Name Native -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
[DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, System.IntPtr dwExtraInfo);
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(System.IntPtr hWnd);
'@
$root = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]__HWND__)
if ($null -eq $root) { Write-Output 'found=0'; Write-Output 'result=no-window'; exit 0 }
Write-Output 'found=1'

function PointerClickElement($el) {
  try {
    $r = $el.Current.BoundingRectangle
    if ($r.Width -le 0 -or $r.Height -le 0) { return $false }
    $cx = [int]($r.X + $r.Width / 2)
    $cy = [int]($r.Y + $r.Height / 2)
    [FlipWin.Native]::SetForegroundWindow([System.IntPtr]__HWND__) | Out-Null
    [FlipWin.Native]::SetCursorPos($cx, $cy) | Out-Null
    Start-Sleep -Milliseconds 80
    [FlipWin.Native]::mouse_event(0x0002, 0, 0, 0, [System.IntPtr]::Zero)
    Start-Sleep -Milliseconds 60
    [FlipWin.Native]::mouse_event(0x0004, 0, 0, 0, [System.IntPtr]::Zero)
    return $true
  } catch { return $false }
}

function InvokeElement($el) {
  try {
    $el.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
    return $true
  } catch { return $false }
}

function LegacyElement($el) {
  try {
    $el.GetCurrentPattern([System.Windows.Automation.LegacyIAccessiblePattern]::Pattern).DoDefaultAction()
    return $true
  } catch { return $false }
}

function KeyboardElement($el) {
  try {
    [FlipWin.Native]::SetForegroundWindow([System.IntPtr]__HWND__) | Out-Null
    $el.SetFocus()
    Start-Sleep -Milliseconds 100
    [System.Windows.Forms.SendKeys]::SendWait('{ENTER}')
    return $true
  } catch { return $false }
}

$startEl = $null
$endEl = $null
$startName = ''
$endName = ''
$startScore = -1
$active = $false
$controls = New-Object System.Collections.Generic.List[string]

# Chromium may expose text input/dictation before its live Voice control. Keep
# scanning one UIA client until actual live Voice is visible or the deadline ends.
$deadline = (Get-Date).AddSeconds(12)
while ($true) {
  $startEl = $null
  $endEl = $null
  $startName = ''
  $endName = ''
  $startScore = -1
  $active = $false
  $controls.Clear()
  $all = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, [System.Windows.Automation.Condition]::TrueCondition)
  foreach ($e in $all) {
    try {
      $name = '' + $e.Current.Name
      $id = '' + $e.Current.AutomationId
      $help = '' + $e.Current.HelpText
      $enabled = $e.Current.IsEnabled
      $ctrlType = $e.Current.ControlType.Id
    } catch { continue }
    $text = ($name + ' ' + $id + ' ' + $help).Trim()
    if ($text -eq '') { continue }
    if ($controls.Count -lt 24 -and $name -ne '' -and $name.Length -le 60) { $controls.Add($name) }

    $clickable = -not ($ctrlType -eq 50020 -or $ctrlType -eq 50007 -or $ctrlType -eq 50013 -or $ctrlType -eq 50030 -or $ctrlType -eq 50004 -or $ctrlType -eq 50006 -or $ctrlType -eq 50008 -or $ctrlType -eq 50024)
    if (-not $clickable) { continue }

    if ($text -match '(?i)(end|stop|exit|leave|close|hang)[\s_-]*(the\s+)?(voice|conversation|call|chat|talk)' -or
        $text -match '(?i)(voice|conversation)[\s_-]*(mode\s+)?(end|stop|exit|leave|close)') {
      $active = $true
      if ($null -eq $endEl -and $enabled) { $endEl = $e; $endName = $text }
      continue
    }

    # Scan every candidate and keep the best one. Do not treat the message-box
    # microphone, mic/dictation or an arbitrary element containing "voice" as
    # the live Voice control.
    $score = -1
    if ($text -match '(?i)\bstart\s+(a\s+)?new\s+voice\s+chat\b|\bnew\s+voice\s+chat\b') {
      $score = 100
    } elseif ($text -match '(?i)\bstart\s+(the\s+)?voice(\s+mode|\s+chat|\s+conversation)?\b|\badvanced\s+voice\b|\bgpt-live\b|\bgo\s+live\b|\blive\s+voice\b') {
      $score = 90
    } elseif ($text -match '(?i)\bvoice\s+(mode|chat|conversation)\b|\buse\s+voice\b') {
      $score = 80
    } elseif ($text -match '(?i)\bheadphones?\b|\bheadset\b') {
      $score = 60
    }
    if ($score -lt 0) { continue }
    if ($text -match '(?i)(setting|settings|input|output|device|volume|permission|help|learn|mute|unmute|summary|topic|title|history|rename|delete)') { continue }
    if ($enabled -and $score -gt $startScore) {
      $startEl = $e
      $startName = $text
      $startScore = $score
    }
  }
  if ($active -or $null -ne $startEl) { break }
  if ((Get-Date) -ge $deadline) { break }
  Start-Sleep -Milliseconds 700
}

foreach ($c in $controls) { Write-Output ('control=' + $c) }
if ($active) { Write-Output 'active=1' } else { Write-Output 'active=0' }
Write-Output ('start=' + $startName)
Write-Output ('end=' + $endName)

$action = '__ACTION__'
if ($action -eq 'state') { Write-Output 'result=read'; exit 0 }

$isStart = $action.StartsWith('start')
$target = $null
if ($isStart) {
  if ($active) { Write-Output 'result=already-active'; exit 0 }
  $target = $startEl
} else {
  if (-not $active) { Write-Output 'result=already-stopped'; exit 0 }
  $target = $endEl
}
if ($null -eq $target) { Write-Output 'result=not-found'; exit 0 }

if ($isStart) {
  $done = $false
  $result = 'invoke-failed'
  if ($action -eq 'start' -or $action -eq 'start-invoke') {
    $done = InvokeElement $target
    if ($done) { $result = 'invoke-sent' }
  } elseif ($action -eq 'start-keyboard') {
    $done = KeyboardElement $target
    if ($done) { $result = 'keyboard-sent' } else { $result = 'keyboard-failed' }
  } elseif ($action -eq 'start-legacy') {
    $done = LegacyElement $target
    if ($done) { $result = 'legacy-sent' } else { $result = 'legacy-failed' }
  } elseif ($action -eq 'start-pointer') {
    $done = PointerClickElement $target
    if ($done) { $result = 'pointer-sent' } else { $result = 'pointer-failed' }
  }
  Write-Output ('result=' + $result)
  exit 0
}

# Ending voice is less ambiguous than starting it, so one action may use the
# normal UIA fallbacks. Start is intentionally one method per action because the
# Go caller verifies that live Voice appeared before trying another method.
$done = InvokeElement $target
if (-not $done) { $done = LegacyElement $target }
if (-not $done) { $done = PointerClickElement $target }
if (-not $done) {
  try { $target.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern).Toggle(); $done = $true } catch {}
}
if (-not $done) {
  try { $target.GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern).Select(); $done = $true } catch {}
}
if ($done) { Write-Output 'result=clicked' } else { Write-Output 'result=invoke-failed' }
exit 0
`

func agentAppTitles(agent string) []string {
	if agent == "A" {
		return []string{"Claude"}
	}
	return []string{"ChatGPT", "Codex"}
}

func agentAppShortcutNames(agent string) []string {
	if agent == "A" {
		return []string{"Claude"}
	}
	return []string{"ChatGPT", "Codex"}
}

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
			{[]string{"Programs", "ChatGPT", "ChatGPT.exe"}},
			{[]string{"OpenAI", "ChatGPT", "ChatGPT.exe"}},
			{[]string{"ChatGPT", "ChatGPT.exe"}},
			{[]string{"Programs", "Codex", "Codex.exe"}},
			{[]string{"Programs", "codex", "Codex.exe"}},
			{[]string{"OpenAI", "Codex", "Codex.exe"}},
			{[]string{"Codex", "Codex.exe"}},
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

func joinPathParts(root string, parts []string) string {
	all := append([]string{strings.TrimRight(root, `\\/`)}, parts...)
	return strings.Join(all, `\`)
}

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