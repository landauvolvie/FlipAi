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
	case s.StartControl == "" && onlyWindowChrome(appTitle, s.Controls):
		// The scan saw only the title bar and Minimize/Maximize/Close. That is
		// the signature of a Chromium/Electron app with accessibility turned
		// off: its window is readable but its contents are not. Reopening it so
		// it starts with accessibility forced is the fix.
		return fmt.Errorf("%s is only exposing its window frame to Windows accessibility (%s), not its contents, so FlipAi cannot see the voice control. This is a Chromium app with accessibility off. Quit %s completely -- including from the system tray -- and let FlipAi reopen it, so it starts with accessibility enabled", appTitle, truncate(strings.Join(s.Controls, ", "), 120), appTitle)
	case s.StartControl == "":
		return fmt.Errorf("FlipAi could not find the live voice control in %s. It offered: %s. For ChatGPT, FlipAi requires the actual \"Start new voice chat\"/Voice Mode control and deliberately ignores dictation and text-message microphone controls", appTitle, truncate(strings.Join(s.Controls, ", "), 240))
	case s.Result == "invoke-failed":
		return fmt.Errorf("Windows refused to press %q in %s", s.StartControl, appTitle)
	}
	return fmt.Errorf("FlipAi pressed %q in %s but it did not enter live voice mode", s.StartControl, appTitle)
}

// onlyWindowChrome reports whether every control the scan saw is part of the
// native window frame -- the app's own title and the standard caption buttons.
// When that is all Windows accessibility returns for a Chromium/Electron app,
// the app's web contents are not being exposed at all.
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
		if strings.Contains(l, "minimize") || strings.Contains(l, "maximize") ||
			strings.Contains(l, "close") || strings.Contains(l, "restore") {
			continue
		}
		// The window's own title -- the app's name -- is part of the frame too.
		if title != "" && (l == title || strings.Contains(l, title) || strings.Contains(title, l)) {
			continue
		}
		// Anything else is real content, so the app is exposing more than chrome.
		return false
	}
	return true
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
Add-Type -Namespace FlipWin -Name Native -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
[DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, System.IntPtr dwExtraInfo);
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(System.IntPtr hWnd);
'@
$root = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]__HWND__)
if ($null -eq $root) { Write-Output 'found=0'; Write-Output 'result=no-window'; exit 0 }
Write-Output 'found=1'

# A real pointer click on the control's own centre. The Codex and ChatGPT
# desktop apps are Chromium/Electron, and their custom buttons -- the "Start new
# voice chat" control among them -- routinely ignore UI Automation's Invoke: it
# can return success while nothing happens. A synthesized click through the same
# input pipeline a person uses is what they actually respond to. The window is
# already brought to the front before this runs.
function ClickElement($el) {
  try {
    $r = $el.Current.BoundingRectangle
    if ($r.Width -le 0 -or $r.Height -le 0) { return $false }
    $cx = [int]($r.X + $r.Width / 2)
    $cy = [int]($r.Y + $r.Height / 2)
    [FlipWin.Native]::SetForegroundWindow([System.IntPtr]__HWND__) | Out-Null
    [FlipWin.Native]::SetCursorPos($cx, $cy) | Out-Null
    Start-Sleep -Milliseconds 60
    [FlipWin.Native]::mouse_event(0x0002, 0, 0, 0, [System.IntPtr]::Zero)
    Start-Sleep -Milliseconds 40
    [FlipWin.Native]::mouse_event(0x0004, 0, 0, 0, [System.IntPtr]::Zero)
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

# Chromium does not always expose its web content immediately. Keep one UIA
# client alive and re-scan until the REAL live-Voice control appears, voice is
# already active, or the deadline passes. A generic microphone/dictation button
# is not success: that starts text dictation, which is exactly how an answered
# phone call can end up with a chat open but no spoken conversation.
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

    # Only something clickable can be the voice control. Conversation titles,
    # list items and text named "Voice ..." are never candidates.
    $clickable = -not ($ctrlType -eq 50020 -or $ctrlType -eq 50007 -or $ctrlType -eq 50013 -or $ctrlType -eq 50030 -or $ctrlType -eq 50004 -or $ctrlType -eq 50006 -or $ctrlType -eq 50008 -or $ctrlType -eq 50024)
    if (-not $clickable) { continue }

    # A control that ends voice is the clearest sign voice is running.
    if ($text -match '(?i)(end|stop|exit|leave|close|hang)[\s_-]*(the\s+)?(voice|conversation|call|chat|talk)' -or
        $text -match '(?i)(voice|conversation)[\s_-]*(mode\s+)?(end|stop|exit|leave|close)') {
      $active = $true
      if ($null -eq $endEl -and $enabled) { $endEl = $e; $endName = $text }
      continue
    }

    # Rank ALL candidates in the tree instead of pressing the first thing whose
    # name merely contains "voice". ChatGPT exposes text dictation/microphone
    # controls alongside live Voice. The exact live-control wording wins by a
    # wide margin, and microphone/mic/dictation are intentionally not candidates.
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

# A real click first, because that is what an Electron control actually
# responds to. The pattern-based methods are the fallback for a control whose
# rectangle is unusable (off-screen, zero-sized) or for a genuine native button.
$done = ClickElement $target
if (-not $done) {
  try { $target.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke(); $done = $true } catch {}
}
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
// ChatGPT is looked for before Codex, deliberately. The voice a caller talks to
// is ChatGPT Voice (GPT-Live): OpenAI ships it in the ChatGPT desktop app, from
// where it can drive Codex, and it is started by clicking "Start new voice chat"
// -- there is no keyboard shortcut for it. The standalone Codex app's own voice
// is a separate, less reliable dictation path. So the ChatGPT app is the right
// front-end for a spoken call even when the agent behind it is Codex; the Codex
// app is the fallback for a machine that only has it. A configured title always
// wins over this list.
func agentAppTitles(agent string) []string {
	if agent == "A" {
		return []string{"Claude"}
	}
	return []string{"ChatGPT", "Codex"}
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
	// ChatGPT first, for the reason in agentAppTitles: it is the app that
	// carries the voice a caller talks to.
	return []string{"ChatGPT", "Codex"}
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
		// ChatGPT before Codex, for the reason in agentAppTitles: the ChatGPT
		// desktop app is the one that carries the voice a caller talks to.
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

// joinPathParts is filepath.Join with the separator fixed to Windows, so the
// candidate list is the same whichever OS the test runs on.
func joinPathParts(root string, parts []string) string {
	all := append([]string{strings.TrimRight(root, `\\/`)}, parts...)
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
