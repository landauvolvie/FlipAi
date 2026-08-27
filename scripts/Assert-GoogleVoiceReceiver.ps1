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
  5. FlipAi's own endpoint inside that process answers, and refuses a request
     that does not carry its token. That endpoint is how the FlipAi host asks
     this process to send an image through the signed-in Google Voice session
     it owns. (The DevTools channel FlipAi uses to press Answer is
     in-process and opens no port, so there is nothing there to check from
     outside.)

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

function Show-Diagnostics {
  param([string]$Why)
  Write-Host "--- $Why ---"
  Write-Host 'FlipAi processes:'
  Get-CimInstance Win32_Process -Filter "Name='FlipAi.exe'" -ErrorAction SilentlyContinue |
    Select-Object ProcessId, CommandLine | Format-Table -AutoSize -Wrap | Out-String | Write-Host
  Write-Host "Data folder ($appData):"
  Get-ChildItem $appData -ErrorAction SilentlyContinue | Select-Object Name, Length |
    Format-Table -AutoSize | Out-String | Write-Host
  foreach ($f in @('voice-call.json', 'voice-call-state.json')) {
    $path = Join-Path $appData $f
    Write-Host "${f}:"
    if (Test-Path $path) { Write-Host (Get-Content $path -Raw) } else { Write-Host '  (not written)' }
  }
}

try {
  # --visible is the product's own "open it so I can sign in" path, which runs
  # whether or not calling has been switched on. Starting it that way keeps
  # this check about the receiver rather than about a settings file.
  Start-Process $Exe -ArgumentList '--google-voice', '--visible'

  $state = $null
  $deadline = (Get-Date).AddSeconds($StartTimeoutSeconds)
  do {
    if (Test-Path $stateFile) { try { $state = Get-Content $stateFile -Raw | ConvertFrom-Json } catch {} }
    if ($state -and $state.browserRunning) { break }
    if ($state -and $state.lastOpenError) {
      Show-Diagnostics 'the Google Voice view reported a failure'
      throw "The Google Voice view failed to start: $($state.lastOpenError)"
    }
    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)
  if (-not ($state -and $state.browserRunning)) {
    Show-Diagnostics 'the Google Voice view never reported that it was running'
    throw 'The Google Voice view never reported that it was running'
  }
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
  Start-Process $Exe -ArgumentList '--google-voice', '--visible'
  Start-Process $Exe -ArgumentList '--google-voice', '--visible'
  Start-Sleep -Seconds 10

  $windows = Get-GoogleVoiceWindows -Title $googleVoiceWindowTitle
  Write-Host "Google Voice windows found: $($windows.Count)"
  if ($windows.Count -eq 0) {
    Show-Diagnostics 'the Google Voice window was not found by title'
    throw 'The Google Voice window was not found by title'
  }
  if ($windows.Count -gt 1) {
    throw "There are $($windows.Count) Google Voice windows. Asking for Google Voice repeatedly must never open a second one."
  }

  # 4. It is a tool window: nothing the user sees as a second application.
  $w = $windows[0]
  if (-not $w.ToolWindow) { throw 'The Google Voice window is not a tool window, so it appears in Alt-Tab' }
  if ($w.AppWindow) { throw 'The Google Voice window claims a taskbar button' }

  # 5. FlipAi's own endpoint is up and is not open to anything else.
  $state = Get-Content $stateFile -Raw | ConvertFrom-Json
  if (-not $state.controlPort) {
    Show-Diagnostics 'the Google Voice process opened no local endpoint'
    throw 'The Google Voice process opened no local endpoint, so an image could never be sent through it'
  }
  $endpoint = "http://127.0.0.1:$($state.controlPort)/health"
  $token = $state.controlToken
  if (-not $token) { throw 'The Google Voice endpoint has no token, so it can never be reached' }

  $answered = $false
  $deadline = (Get-Date).AddSeconds(30)
  do {
    try {
      if (Invoke-RestMethod $endpoint -Headers @{ 'X-FlipAi-Token' = $token } -TimeoutSec 2) { $answered = $true; break }
    } catch {}
    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)
  if (-not $answered) {
    Show-Diagnostics "the local endpoint on port $($state.controlPort) did not answer"
    throw "FlipAi's Google Voice endpoint on port $($state.controlPort) did not answer"
  }
  Write-Host "FlipAi's Google Voice endpoint answered on 127.0.0.1:$($state.controlPort)"

  # An endpoint that would answer without the token would let anything on this
  # machine drive a signed-in Google Voice session.
  $refused = $false
  try { Invoke-RestMethod $endpoint -TimeoutSec 2 | Out-Null } catch { $refused = $true }
  if (-not $refused) { throw "FlipAi's Google Voice endpoint answered a request with no token" }
  Write-Host 'The endpoint refused a request with no token.'
  Write-Host 'Google Voice receiver checks passed.'
}
finally {
  Get-Process -Name FlipAi -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
  Get-Process -Name msedgewebview2 -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
}
