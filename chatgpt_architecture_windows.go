//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// augmentChatGPTDirectProbe performs the broad architecture survey requested
// for the direct ChatGPT experiment. It deliberately gathers metadata only:
// process topology, module names, package manifest extensions, activation
// protocols, window class names, endpoint metadata and static package strings.
// It never reads cookies, tokens, request/response bodies, process memory,
// browser storage, credential vaults, or full command lines.
func augmentChatGPTDirectProbe(ctx context.Context, p *chatGPTDirectProbeResult) error {
	const script = `$ErrorActionPreference='SilentlyContinue'
$packages = @(Get-AppxPackage -ErrorAction SilentlyContinue | Where-Object {
  $_.Name -match '(?i)(ChatGPT|OpenAI)' -or $_.PackageFullName -match '(?i)(ChatGPT|OpenAI)'
})
$installLocations = @($packages | ForEach-Object { [string]$_.InstallLocation } | Where-Object { $_ } | Sort-Object -Unique)
$packageIdentity = @($packages | ForEach-Object {
  ('{0} v{1} arch={2} family={3}' -f $_.Name,$_.Version,$_.Architecture,$_.PackageFamilyName)
} | Sort-Object -Unique)

$all = @(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue)
$procs = @($all | Where-Object {
  $_.Name -match '^(ChatGPT|OpenAI).*\.exe$' -or
  ($_.ExecutablePath -and $_.ExecutablePath -match '(?i)\\(ChatGPT|OpenAI)\\')
})
$pids = @($procs | ForEach-Object { [int]$_.ProcessId })
$exePaths = @($procs | ForEach-Object { [string]$_.ExecutablePath } | Where-Object { $_ } | Sort-Object -Unique)
$processInventory = @($procs | ForEach-Object {
  $exe = if ($_.ExecutablePath) { [IO.Path]::GetFileName([string]$_.ExecutablePath) } else { '?' }
  ('pid={0} parent={1} name={2} exe={3}' -f $_.ProcessId,$_.ParentProcessId,$_.Name,$exe)
} | Sort-Object -Unique)

# Walk three generations. WebView2/Electron network and renderer helpers often
# live below the top-level ChatGPT process, and their names are useful without
# reading any command line or process memory.
$desc = @()
$front = @($pids)
for ($depth=0; $depth -lt 3 -and $front.Count -gt 0; $depth++) {
  $next = @($all | Where-Object { $front -contains [int]$_.ParentProcessId })
  foreach ($n in $next) {
    if (-not ($desc | Where-Object { $_.ProcessId -eq $n.ProcessId })) { $desc += $n }
  }
  $front = @($next | ForEach-Object { [int]$_.ProcessId })
}
$childInventory = @($desc | ForEach-Object {
  $exe = if ($_.ExecutablePath) { [IO.Path]::GetFileName([string]$_.ExecutablePath) } else { '?' }
  ('pid={0} parent={1} name={2} exe={3}' -f $_.ProcessId,$_.ParentProcessId,$_.Name,$exe)
} | Sort-Object -Unique)
$ownedPids = @($pids + @($desc | ForEach-Object { [int]$_.ProcessId }) | Sort-Object -Unique)

$moduleSignals = @()
foreach ($pidValue in $ownedPids) {
  try {
    $gp = Get-Process -Id $pidValue -ErrorAction Stop
    foreach ($m in @($gp.Modules)) {
      $mn = [string]$m.ModuleName
      if ($mn -match '(?i)(WebView2|chrome_elf|libEGL|libGLES|Microsoft\.UI\.Xaml|WebView2Loader|msedge|electron|WinUI|CoreWebView)') {
        $moduleSignals += ('pid={0} module={1}' -f $pidValue,$mn)
      }
    }
  } catch {}
}
$moduleSignals = @($moduleSignals | Sort-Object -Unique | Select-Object -First 60)

# Main-window class names distinguish common native/WebView/Electron shells and
# do not use UI Automation or interact with the window.
$windowClasses = @()
try {
  Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Runtime.InteropServices;
public static class FlipAiWindowClassProbe {
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr hWnd, StringBuilder text, int maxCount);
}
'@ -ErrorAction SilentlyContinue
  foreach ($pidValue in $ownedPids) {
    try {
      $gp = Get-Process -Id $pidValue -ErrorAction Stop
      if ($gp.MainWindowHandle -ne 0) {
        $sb = New-Object System.Text.StringBuilder 260
        [void][FlipAiWindowClassProbe]::GetClassName($gp.MainWindowHandle,$sb,$sb.Capacity)
        if ($sb.Length -gt 0) { $windowClasses += ('pid={0} class={1}' -f $pidValue,$sb.ToString()) }
      }
    } catch {}
  }
} catch {}
$windowClasses = @($windowClasses | Sort-Object -Unique)

# Connection metadata only: remote address/port. No packet payloads, HTTP
# headers, TLS secrets, URLs, or authentication state are inspected.
$networkPeers = @()
if ($ownedPids.Count -gt 0) {
  $networkPeers = @(Get-NetTCPConnection -State Established -ErrorAction SilentlyContinue | Where-Object {
    $ownedPids -contains [int]$_.OwningProcess
  } | ForEach-Object {
    ('pid={0} remote={1}:{2}' -f $_.OwningProcess,$_.RemoteAddress,$_.RemotePort)
  } | Sort-Object -Unique | Select-Object -First 30)
}
$dnsNames = @(Get-DnsClientCache -ErrorAction SilentlyContinue | ForEach-Object { [string]$_.Entry } | Where-Object {
  $_ -match '(?i)(chatgpt|openai|oaistatic|oaiusercontent|statsig)'
} | Sort-Object -Unique | Select-Object -First 50)

$appExtensions = @()
$protocolSchemes = @()
$packageTopLevel = @()
foreach ($pkg in $packages) {
  $loc = [string]$pkg.InstallLocation
  if (-not $loc) { continue }
  try {
    $packageTopLevel += @(Get-ChildItem -LiteralPath $loc -Force -ErrorAction SilentlyContinue | ForEach-Object {
      if ($_.PSIsContainer) { 'dir:' + $_.Name } else { 'file:' + $_.Name }
    })
  } catch {}
  $manifestPath = Join-Path $loc 'AppxManifest.xml'
  if (Test-Path -LiteralPath $manifestPath) {
    try {
      [xml]$xml = Get-Content -LiteralPath $manifestPath -Raw -ErrorAction Stop
      foreach ($a in @($xml.SelectNodes("//*[local-name()='Application']"))) {
        $ep = $a.GetAttribute('EntryPoint')
        $ex = $a.GetAttribute('Executable')
        if ($ep -or $ex) {
          $exName = if ($ex) { [IO.Path]::GetFileName($ex) } else { '' }
          $appExtensions += ('Application entry={0} exe={1}' -f $ep,$exName)
        }
      }
      foreach ($e in @($xml.SelectNodes("//*[local-name()='Extension']"))) {
        $cat = $e.GetAttribute('Category')
        $ep = $e.GetAttribute('EntryPoint')
        $ex = $e.GetAttribute('Executable')
        $exName = if ($ex) { [IO.Path]::GetFileName($ex) } else { '' }
        if ($cat -or $ep -or $ex) { $appExtensions += ('{0} entry={1} exe={2}' -f $cat,$ep,$exName) }
      }
      foreach ($n in @($xml.SelectNodes("//*[local-name()='Protocol']"))) {
        $name = $n.GetAttribute('Name')
        if ($name) { $protocolSchemes += ($name + '://') }
      }
      foreach ($n in @($xml.SelectNodes("//*[local-name()='AppService']"))) {
        $name = $n.GetAttribute('Name')
        if ($name) { $appExtensions += ('windows.appService name=' + $name) }
      }
    } catch {}
  }
}

# Also report explicit URL-protocol registrations whose key names identify
# ChatGPT/OpenAI. We do not invoke them.
foreach ($root in @('Registry::HKEY_CURRENT_USER\Software\Classes','Registry::HKEY_LOCAL_MACHINE\Software\Classes')) {
  if (-not (Test-Path $root)) { continue }
  try {
    foreach ($k in @(Get-ChildItem $root -ErrorAction SilentlyContinue | Where-Object { $_.PSChildName -match '(?i)(chatgpt|openai)' })) {
      try {
        $v = (Get-ItemProperty -LiteralPath $k.PSPath -Name 'URL Protocol' -ErrorAction Stop).'URL Protocol'
        if ($null -ne $v) { $protocolSchemes += ($k.PSChildName + '://') }
      } catch {}
    }
  } catch {}
}

[pscustomobject]@{
  installLocations=@($installLocations)
  executablePaths=@($exePaths)
  packageIdentity=@($packageIdentity | Sort-Object -Unique)
  processInventory=@($processInventory)
  childInventory=@($childInventory)
  moduleSignals=@($moduleSignals)
  windowClasses=@($windowClasses)
  networkPeers=@($networkPeers)
  dnsNames=@($dnsNames)
  appExtensions=@($appExtensions | Sort-Object -Unique | Select-Object -First 60)
  protocolSchemes=@($protocolSchemes | Sort-Object -Unique | Select-Object -First 40)
  packageTopLevel=@($packageTopLevel | Sort-Object -Unique | Select-Object -First 80)
} | ConvertTo-Json -Compress -Depth 6`

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("Windows ChatGPT architecture survey: %s", msg)
	}
	var raw struct {
		InstallLocations  []string `json:"installLocations"`
		ExecutablePaths   []string `json:"executablePaths"`
		PackageIdentity   []string `json:"packageIdentity"`
		ProcessInventory  []string `json:"processInventory"`
		ChildInventory    []string `json:"childInventory"`
		ModuleSignals     []string `json:"moduleSignals"`
		WindowClasses     []string `json:"windowClasses"`
		NetworkPeers      []string `json:"networkPeers"`
		DNSNames          []string `json:"dnsNames"`
		AppExtensions     []string `json:"appExtensions"`
		ProtocolSchemes   []string `json:"protocolSchemes"`
		PackageTopLevel   []string `json:"packageTopLevel"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return fmt.Errorf("decode Windows ChatGPT architecture survey: %w", err)
	}

	p.PackageIdentity = append(p.PackageIdentity, raw.PackageIdentity...)
	p.ProcessInventory = append(p.ProcessInventory, raw.ProcessInventory...)
	p.ChildProcesses = append(p.ChildProcesses, raw.ChildInventory...)
	p.RuntimeSignals = append(p.RuntimeSignals, raw.ModuleSignals...)
	p.WindowClasses = append(p.WindowClasses, raw.WindowClasses...)
	p.NetworkPeers = append(p.NetworkPeers, raw.NetworkPeers...)
	p.OpenAIDNSNames = append(p.OpenAIDNSNames, raw.DNSNames...)
	p.AppExtensions = append(p.AppExtensions, raw.AppExtensions...)
	p.ProtocolSchemes = append(p.ProtocolSchemes, raw.ProtocolSchemes...)
	p.PackageTopLevel = append(p.PackageTopLevel, raw.PackageTopLevel...)

	goodSources, noisySources := attributeChatGPTMarkerSources(ctx, raw.InstallLocations, raw.ExecutablePaths)
	p.MarkerSources = append(p.MarkerSources, goodSources...)
	p.NoisyMarkerSources = append(p.NoisyMarkerSources, noisySources...)

	p.RuntimeArchitecture = classifyChatGPTRuntime(p.RuntimeSignals, p.ChildProcesses, p.PackageTopLevel, p.WindowClasses)
	p.DirectAssessment = assessChatGPTDirectPath(*p)
	return nil
}

type chatGPTMarkerCandidate struct {
	path     string
	display  string
	priority int
	noisy    bool
	size     int64
}

func attributeChatGPTMarkerSources(ctx context.Context, installLocations, executablePaths []string) ([]string, []string) {
	roots := []string{}
	seenRoots := map[string]bool{}
	addRoot := func(v string) {
		v = filepath.Clean(strings.TrimSpace(v))
		if v == "" || v == "." {
			return
		}
		key := strings.ToLower(v)
		if !seenRoots[key] {
			seenRoots[key] = true
			roots = append(roots, v)
		}
	}
	for _, v := range installLocations {
		addRoot(v)
	}
	for _, exe := range executablePaths {
		if strings.TrimSpace(exe) != "" {
			addRoot(filepath.Dir(exe))
		}
	}

	var candidates []chatGPTMarkerCandidate
	seenFiles := map[string]bool{}
	for _, root := range roots {
		if ctx.Err() != nil {
			break
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			lower := strings.ToLower(filepath.Clean(path))
			for _, privatePart := range []string{"\\user data\\", "\\local storage\\", "\\indexeddb\\", "\\session storage\\", "\\network\\", "\\cache\\", "\\gpucache\\"} {
				if strings.Contains(lower, privatePart) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e != nil || info.Size() <= 0 || info.Size() > 160<<20 {
				return nil
			}
			base := strings.ToLower(d.Name())
			ext := strings.ToLower(filepath.Ext(base))
			allowed := base == "app.asar" || base == "appxmanifest.xml" || ext == ".js" || ext == ".mjs" || ext == ".cjs" || ext == ".json" || ext == ".html" || ext == ".xml" || ext == ".exe" || ext == ".dll" || ext == ".pak"
			if !allowed {
				return nil
			}
			key := strings.ToLower(filepath.Clean(path))
			if seenFiles[key] {
				return nil
			}
			seenFiles[key] = true
			rel, _ := filepath.Rel(root, path)
			display := filepath.ToSlash(rel)
			if display == "." || display == "" {
				display = d.Name()
			}
			noisy := false
			for _, part := range []string{"\\node_modules\\", "\\cua_node\\", "\\pdfjs", "\\playwright", "\\plugins\\codex-app-tools\\", "\\plugins\\browser\\", "\\plugins\\chrome\\", "\\resources\\default_app\\"} {
				if strings.Contains(lower, part) {
					noisy = true
					break
				}
			}
			priority := 4
			switch {
			case base == "app.asar" || base == "appxmanifest.xml":
				priority = 0
			case strings.Contains(base, "chatgpt") || strings.Contains(base, "openai"):
				priority = 1
			case !noisy && (ext == ".js" || ext == ".mjs" || ext == ".cjs" || ext == ".json" || ext == ".html"):
				priority = 2
			case !noisy && (ext == ".exe" || ext == ".dll" || ext == ".pak"):
				priority = 3
			}
			candidates = append(candidates, chatGPTMarkerCandidate{path: path, display: display, priority: priority, noisy: noisy, size: info.Size()})
			return nil
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		if candidates[i].noisy != candidates[j].noisy {
			return !candidates[i].noisy
		}
		if candidates[i].size != candidates[j].size {
			return candidates[i].size < candidates[j].size
		}
		return candidates[i].display < candidates[j].display
	})
	if len(candidates) > 90 {
		candidates = candidates[:90]
	}

	strong := func(m string) bool {
		low := strings.ToLower(m)
		for _, s := range []string{"backend-api", "chatgpt.com", "openai.com", "conversation", "ipcrenderer", "ipcmain", "websocket", "chatgpt://", "openai://"} {
			if strings.Contains(low, s) {
				return true
			}
		}
		return false
	}
	good := []string{}
	noisy := []string{}
	var total int64
	for _, c := range candidates {
		if ctx.Err() != nil || total >= 128<<20 || (len(good) >= 30 && len(noisy) >= 20) {
			break
		}
		f, err := os.Open(c.path)
		if err != nil {
			continue
		}
		limit := int64(20 << 20)
		if strings.EqualFold(filepath.Base(c.path), "app.asar") {
			limit = 64 << 20
		}
		if remain := int64(128<<20) - total; remain < limit {
			limit = remain
		}
		b, err := io.ReadAll(io.LimitReader(f, limit))
		_ = f.Close()
		if err != nil {
			continue
		}
		total += int64(len(b))
		markers := []string{}
		for _, m := range extractChatGPTProtocolMarkers(b) {
			if strong(m) {
				markers = append(markers, m)
			}
			if len(markers) >= 6 {
				break
			}
		}
		if len(markers) == 0 {
			continue
		}
		entry := c.display + " -> " + strings.Join(markers, ", ")
		if len(entry) > 500 {
			entry = entry[:500]
		}
		if c.noisy {
			if len(noisy) < 20 {
				noisy = append(noisy, entry)
			}
		} else if len(good) < 30 {
			good = append(good, entry)
		}
	}
	return good, noisy
}

func classifyChatGPTRuntime(signals, children, topLevel, windowClasses []string) string {
	all := make([]string, 0, len(signals)+len(children)+len(topLevel)+len(windowClasses))
	all = append(all, signals...)
	all = append(all, children...)
	all = append(all, topLevel...)
	all = append(all, windowClasses...)
	joined := strings.ToLower(strings.Join(all, " "))
	hasWebView2 := strings.Contains(joined, "msedgewebview2") || strings.Contains(joined, "webview2loader") || strings.Contains(joined, "corewebview")
	hasElectron := strings.Contains(joined, "chrome_elf") || strings.Contains(joined, "electron") || strings.Contains(joined, "resources.pak")
	hasWinUI := strings.Contains(joined, "microsoft.ui.xaml") || strings.Contains(joined, "winui")
	switch {
	case hasWinUI && hasWebView2:
		return "WinUI/native Windows shell with WebView2 content"
	case hasWebView2:
		return "WebView2/Edge-hosted desktop client"
	case hasElectron:
		return "Electron/Chromium-style desktop client"
	case hasWinUI:
		return "WinUI/native Windows desktop client"
	default:
		return "Packaged Windows client; rendering engine not yet proven"
	}
}

func assessChatGPTDirectPath(p chatGPTDirectProbeResult) string {
	if len(p.ASARIPCCandidates) > 0 {
		return "The real Electron app.asar contains IPC/bridge channel names. That is now the strongest direct-path evidence: the next build should map those exact channels to their main/preload/renderer handlers and test only a harmless background invocation if the channel is owned by regular ChatGPT Chat."
	}
	if len(p.ASARMarkerSources) > 0 {
		return "The real Electron app.asar contains attributable Chat/backend markers. FlipAi can now distinguish regular ChatGPT application code from bundled Codex/browser tooling; the next build should trace those exact app-bundle call sites before attempting any request."
	}
	joinedExt := strings.ToLower(strings.Join(p.AppExtensions, " "))
	if strings.Contains(joinedExt, "windows.appservice") {
		return "The ChatGPT package declares a Windows AppService. That is the strongest supported-looking local backend candidate and should be protocol-tested next before any cloud/private endpoint."
	}
	if p.provenTransport() {
		return "A ChatGPT-owned live local transport exists. The next implementation should fingerprint that owned transport and test a harmless request before enabling SMS routing."
	}
	appSpecific := strings.ToLower(strings.Join(p.MarkerSources, " "))
	if appSpecific != "" && strings.Contains(appSpecific, "backend-api") {
		return "No externally callable local API is exposed, but app-specific installed code references ChatGPT backend-api/conversation machinery. This points toward a cloud-backed in-app client; a shippable direct integration would still need a supported authentication/session interface rather than copying private ChatGPT credentials."
	}
	if len(p.MarkerSources) == 0 && len(p.NoisyMarkerSources) > 0 {
		return "The protocol strings found earlier are confined to bundled browser/Codex/runtime tooling, not proven regular ChatGPT Chat code. There is still no safe direct request interface to call."
	}
	if len(p.ProtocolSchemes) > 0 {
		return "The package exposes activation protocol scheme(s), but no local chat backend. Those schemes may open/navigate the app; they are not evidence of a message-send API."
	}
	if len(p.OpenAIDNSNames) > 0 || len(p.NetworkPeers) > 0 {
		return "The desktop client has cloud network activity but no owned local API/IPC endpoint was found. Option 2 is therefore not yet callable without an official local or authenticated cloud interface."
	}
	return "No ChatGPT-owned local API, app service, debugging channel, or attributable regular-Chat protocol was found. Do not enable SMS routing from this evidence alone."
}
