<#
.SYNOPSIS
Checks that Google Voice comes up inside FlipAi and nowhere else.

.DESCRIPTION
This is the regression guard for the failure that this feature kept hitting in
the real world. It cannot make a phone ring, and it does not pretend to: what it
proves is the part CI can prove.

  1. FlipAi's own WebView2 view is hosting Google Voice.
  2. No Microsoft Edge application window was started. Google Voice used to run
     in a browser FlipAi launched and then moved around, which is why Edge
     windows appeared on the desktop.
  3. Asking for Google Voice repeatedly produces exactly one window. A second
     window is the duplicate-window bug.
  4. That window is a tool window: no taskbar button, no Alt-Tab entry. It has
     nowhere to be except inside the FlipAi panel.
  5. Its loopback control channel answers. That channel is how FlipAi presses
     Answer when a scripted click is ignored, and how it sends an MMS; without
     it an incoming call has one fewer way to be answered.

Everything past this -- a real Google Voice ring, a real answered call, a real
Codex desktop voice session, real audio over the cables -- needs a signed-in
Google Voice account, an installed desktop app and virtual audio devices, and
can only be verified on the user's own Windows PC.
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][string]$Exe,
  [Parameter(Mandatory = $true)][string]$DataRoot,
  [int]$StartTimeoutSeconds = 120
)

$ErrorActionPreference = 'Stop'

$googleVoiceWindowTitle = "FlipAi $([char]0x2014) Google Voice"

Add-Type -Namespace FlipAi -Name Win -MemberDefinition @"
[DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc cb, IntPtr p);
[DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int GetWindowTextW(IntPtr h, System.Text.StringBuilder s, int n);
[DllImport("user32.dll", EntryPoint = "GetWindowLongPtrW")] public static extern IntPtr GetWindowLongPtr(IntPtr h, int i);
public delegate bool EnumWindowsProc(IntPtr h, IntPtr p);
"@

function Get-GoogleVoiceWindows {
  param([string]$Title)
  $found = New-Object System.Collections.ArrayList
  $callback = [FlipAi.Win+EnumWindowsProc] {
    param($hwnd, $lparam)
    $sb = New-Object System.Text.StringBuilder 512
    [void][FlipAi.Win]::GetWindowTextW($hwnd, $sb, $sb.Capacity)
    if ($sb.ToString() -eq $Title) {
      $ex = [int][FlipAi.Win]::GetWindowLongPtr($hwnd, -20)  # GWL_EXSTYLE
      [void]$found.Add([pscustomobject]@{
        Handle    = $hwnd
        ToolWindow = ($ex -band 0x00000080) -ne 0            # WS_EX_TOOLWINDOW
        AppWindow  = ($ex -band 0x00040000) -ne 0            # WS_EX_APPWINDOW
      })
    }
    return $true
  }
  [void][FlipAi.Win]::EnumWindows($callback, [IntPtr]::Zero)
  return $found
}

Remove-Item $DataRoot -Recurse -Force -ErrorAction SilentlyContinue
$appData = Join-Path $DataRoot 'AISMSBridge'
New-Item -ItemType Directory -Force $appData | Out-Null
$env:LOCALAPPDATA = $DataRoot
$env:AISMSBRIDGE_NO_BROWSER = '1'
$stateFile = Join-Path $appData 'voice-call-state.json'
$edgeBefore = @(Get-Process -Name msedge -ErrorAction SilentlyContinue).Id

# Calling is off in a fresh install, and with it off the receiver is entitled to
# stand down. Turning it on is what makes every invocation below take the real
# path -- including the two extra ones, which is the whole point of the
# duplicate-window check.
@{
  enabled        = $true
  defaultAgent   = 'C'
  googleVoiceUrl = 'https://voice.google.com/'
  codex          = @{ appTitle = 'Codex' }
  claude         = @{ appTitle = 'Claude' }
} | ConvertTo-Json | Set-Content (Join-Path $appData 'voice-call.json') -Encoding utf8

try {
  Start-Process $Exe -ArgumentList '--google-voice'

  $state = $null
  $deadline = (Get-Date).AddSeconds($StartTimeoutSeconds)
  do {
    if (Test-Path $stateFile) { try { $state = Get-Content $stateFile -Raw | ConvertFrom-Json } catch {} }
    if ($state -and $state.browserRunning) { break }
    if ($state -and $state.lastOpenError) { throw "The Google Voice view failed to start: $($state.lastOpenError)" }
    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)
  if (-not ($state -and $state.browserRunning)) { throw 'The Google Voice view never reported that it was running' }
  Write-Host "Google Voice is running; render mode: $($state.renderMode)"

  # 1 + 2. FlipAi's own view, not a browser FlipAi launched.
  if (@(Get-Process -Name msedgewebview2 -ErrorAction SilentlyContinue).Count -eq 0) {
    throw 'No WebView2 process is hosting Google Voice'
  }
  $edgeNew = @(Get-Process -Name msedge -ErrorAction SilentlyContinue | Where-Object { $_.Id -notin $edgeBefore })
  if ($edgeNew.Count -gt 0) {
    @($edgeNew | Select-Object Id, MainWindowTitle | Format-Table | Out-String) | Write-Host
    throw 'FlipAi started Microsoft Edge as a separate application. Google Voice must live in FlipAi own WebView2 view.'
  }

  # 3. Asking again must never open a second window.
  Start-Process $Exe -ArgumentList '--google-voice'
  Start-Process $Exe -ArgumentList '--google-voice'
  Start-Sleep -Seconds 8

  $windows = Get-GoogleVoiceWindows -Title $googleVoiceWindowTitle
  Write-Host "Google Voice windows found: $($windows.Count)"
  if ($windows.Count -eq 0) { throw 'The Google Voice window was not found by title' }
  if ($windows.Count -gt 1) {
    throw "There are $($windows.Count) Google Voice windows. Asking for Google Voice repeatedly must never open a second one."
  }

  # 4. It is a tool window: nothing the user sees as a second application.
  $w = $windows[0]
  if (-not $w.ToolWindow) { throw 'The Google Voice window is not a tool window, so it appears in Alt-Tab' }
  if ($w.AppWindow) { throw 'The Google Voice window claims a taskbar button' }

  # 5. The loopback control channel is open.
  $state = Get-Content $stateFile -Raw | ConvertFrom-Json
  if (-not $state.controlPort) { throw 'The Google Voice view opened no loopback control channel' }
  $answered = $false
  $deadline = (Get-Date).AddSeconds(30)
  do {
    try {
      if (Invoke-RestMethod "http://127.0.0.1:$($state.controlPort)/json/version" -TimeoutSec 2) { $answered = $true; break }
    } catch {}
    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)
  if (-not $answered) { throw "The Google Voice control channel on port $($state.controlPort) did not answer" }
  Write-Host "Control channel answered on 127.0.0.1:$($state.controlPort)"
  Write-Host 'Google Voice receiver checks passed.'
}
finally {
  Get-Process -Name FlipAi -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
  Get-Process -Name msedgewebview2 -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
}
