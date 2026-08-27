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

// Some desktop releases render Voice as a custom/icon accessibility element
// rather than a plain Button. The original launcher deliberately only searched
// buttons, which is safe but can leave a real Google Voice call connected while
// ChatGPT merely comes to the foreground. If normal activation reports an
// agent-voice-error during a live call, retry once through a broader UI
// Automation search using Name + AutomationId + HelpText and Invoke/legacy
// default-action patterns. No coordinates are used.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "--google-voice" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	go runAgentVoiceActivationGuard(dataDir)
}

func runAgentVoiceActivationGuard(dataDir string) {
	t := time.NewTicker(900 * time.Millisecond)
	defer t.Stop()
	var lastTry time.Time
	for range t.C {
		if quitRequested(dataDir) {
			return
		}
		rt := loadVoiceRuntime(dataDir)
		if !rt.InCall || (rt.Agent != "C" && rt.Agent != "A") || rt.LastEvent != "agent-voice-error" {
			continue
		}
		if time.Since(lastTry) < 3*time.Second {
			continue
		}
		lastTry = time.Now()
		cfg := loadVoiceCallConfig(dataDir)
		target := voiceAgentConfig(cfg, rt.Agent)
		hwnd, err := ensureAgentAppWindow(target)
		if err == nil {
			err = invokeAgentVoiceControlBroad(hwnd)
		}
		if err != nil {
			mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
				if s.InCall && s.Agent == rt.Agent {
					s.LastError = "Google Voice is connected, but desktop Voice has not started yet: " + err.Error()
				}
			})
			continue
		}
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			if s.InCall && s.Agent == rt.Agent {
				s.LastEvent = "call-bridged"
				if currentVoiceCablePlan(dataDir).Warning == "" {
					s.LastError = ""
				}
			}
		})
	}
}

func invokeAgentVoiceControlBroad(hwnd uintptr) error {
	if hwnd == 0 {
		return errors.New("desktop agent window was not found")
	}
	script := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
$root=[System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]%d)
if($null -eq $root){exit 2}
$all=$root.FindAll([System.Windows.Automation.TreeScope]::Descendants,[System.Windows.Automation.Condition]::TrueCondition)
foreach($e in $all){
  if(-not $e.Current.IsEnabled){continue}
  $text=((''+$e.Current.Name)+' '+(''+$e.Current.AutomationId)+' '+(''+$e.Current.HelpText)).Trim()
  if($text -notmatch '(?i)(start\s+voice|voice\s+mode|voice\s+conversation|talk\s+to|microphone|\bvoice\b|\bmic\b)'){continue}
  if($text -match '(?i)(end|stop|leave|hang\s*up|settings|input|output|device)'){continue}
  try{$p=$e.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern);$p.Invoke();Write-Output 'clicked';exit 0}catch{}
  try{$p=$e.GetCurrentPattern([System.Windows.Automation.LegacyIAccessiblePattern]::Pattern);$p.DoDefaultAction();Write-Output 'clicked';exit 0}catch{}
  try{$p=$e.GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern);$p.Select();Write-Output 'clicked';exit 0}catch{}
}
Write-Output 'not-found';exit 3
`, hwnd)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil || text != "clicked" {
		if text == "" {
			text = "no accessible Voice control was found"
		}
		return fmt.Errorf("%s", text)
	}
	return nil
}
