//go:build windows

package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// FlipAi needs two independent speaker/microphone pairs to move a phone call
// between Google Voice and a native desktop AI app without touching the PC's
// physical microphone or speakers. Rather than requiring a paid cable package,
// FlipAi can install two instances of the MIT-licensed Virtual Audio Driver by
// MikeTheTech/VirtualDrivers. The package is downloaded from the upstream
// signed GitHub release, pinned by SHA-256, and its Authenticode signature is
// verified before elevation. FlipAi never enables Windows test-signing, never
// disables Secure Boot, and never adds a publisher to a trust store.
const (
	voiceAudioInstallListen = "127.0.0.1:8772"
	vadTag                  = "25.7.14"
	vadURL                  = "https://github.com/VirtualDrivers/Virtual-Audio-Driver/releases/download/25.7.14/Virtual.Audio.Driver.Signed.-.25.7.14.zip"
	vadSHA256               = "dd10560994de65a7e587fb8b93c0d7e9838292d9c3566a0976c2786d727292bd"
	nefconURL               = "https://github.com/nefarius/nefcon/releases/download/v1.17.40/nefcon_v1.17.40.zip"
	nefconSHA256            = "812bae7ed7dfb7d6d2284bc7de2f8ccebc92ed2a0b1ae893c53b337096e50c1a"
)

var voiceAudioInstallMu sync.Mutex

func init() {
	if len(os.Args) < 2 || os.Args[1] != "--host" {
		return
	}
	dataDir, cfgPath, _, _, err := appPaths()
	if err != nil {
		return
	}
	cfg, err := loadConfig(cfgPath, dataDir)
	if err != nil {
		cfg = defaultConfig(dataDir)
	}
	go startVoiceAudioInstallServer(dataDir, cfg.Listen)
}

type voiceAudioInstallResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Reboot  bool   `json:"reboot,omitempty"`
}

func startVoiceAudioInstallServer(dataDir, mainListen string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/install", func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !voiceOriginAllowed(origin, mainListen) {
			http.Error(w, "FlipAi audio installer is local-only", http.StatusForbidden)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		voiceAudioInstallMu.Lock()
		defer voiceAudioInstallMu.Unlock()
		result := installFlipAiAudioBridge(dataDir)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if !result.OK {
			w.WriteHeader(http.StatusInternalServerError)
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	server := &http.Server{Addr: voiceAudioInstallListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	_ = server.ListenAndServe()
}

func installFlipAiAudioBridge(dataDir string) voiceAudioInstallResult {
	if runtime.GOARCH != "amd64" {
		return voiceAudioInstallResult{Message: "The built-in audio bridge installer currently supports Windows x64 only."}
	}
	root := filepath.Join(dataDir, "audio-bridge", vadTag)
	if err := os.MkdirAll(root, 0700); err != nil {
		return voiceAudioInstallResult{Message: "Could not create the audio-bridge folder: " + err.Error()}
	}
	vadZip := filepath.Join(root, "VirtualAudioDriver.zip")
	nefZip := filepath.Join(root, "nefcon.zip")
	vadDir := filepath.Join(root, "vad")
	nefDir := filepath.Join(root, "nefcon")

	if err := downloadPinnedFile(vadURL, vadZip, vadSHA256); err != nil {
		return voiceAudioInstallResult{Message: "Could not download/verify the free virtual audio driver: " + err.Error()}
	}
	if err := downloadPinnedFile(nefconURL, nefZip, nefconSHA256); err != nil {
		return voiceAudioInstallResult{Message: "Could not download/verify the Windows device installer helper: " + err.Error()}
	}
	_ = os.RemoveAll(vadDir)
	_ = os.RemoveAll(nefDir)
	if err := unzipSafe(vadZip, vadDir); err != nil {
		return voiceAudioInstallResult{Message: "Could not unpack the virtual audio driver: " + err.Error()}
	}
	if err := unzipSafe(nefZip, nefDir); err != nil {
		return voiceAudioInstallResult{Message: "Could not unpack the device installer helper: " + err.Error()}
	}
	inf, err := findNamedFile(vadDir, "VirtualAudioDriver.inf")
	if err != nil {
		return voiceAudioInstallResult{Message: err.Error()}
	}
	sys, err := findNamedFile(vadDir, "VirtualAudioDriver.sys")
	if err != nil {
		return voiceAudioInstallResult{Message: err.Error()}
	}
	nefcon, err := findNefconX64(nefDir)
	if err != nil {
		return voiceAudioInstallResult{Message: err.Error()}
	}
	if err := verifyAuthenticodeValid(sys); err != nil {
		return voiceAudioInstallResult{Message: "Windows did not validate the virtual audio driver's signature, so FlipAi refused to install it: " + err.Error()}
	}

	script := filepath.Join(root, "install-audio-bridge.ps1")
	logPath := filepath.Join(root, "install-audio-bridge.log")
	if err := os.WriteFile(script, []byte(audioDriverInstallPS(inf, sys, nefcon, logPath)), 0600); err != nil {
		return voiceAudioInstallResult{Message: "Could not prepare the audio driver installer: " + err.Error()}
	}
	exitCode, runErr := runElevatedPowerShell(script)
	logBytes, _ := os.ReadFile(logPath)
	logText := strings.TrimSpace(string(logBytes))
	if runErr != nil {
		msg := runErr.Error()
		if logText != "" {
			msg += ": " + truncate(logText, 900)
		}
		return voiceAudioInstallResult{Message: msg}
	}
	if exitCode != 0 && exitCode != 3010 {
		msg := fmt.Sprintf("Windows could not start the free audio bridge (installer exit %d)", exitCode)
		if logText != "" {
			msg += ": " + truncate(logText, 900)
		}
		return voiceAudioInstallResult{Message: msg}
	}

	// Edge enumerates devices on every control tick; once Windows publishes the
	// two new pairs the normal cable planner sees them without a restart of
	// FlipAi. Record a useful interim status while Windows is doing that.
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.RoutingNote = "The free FlipAi audio bridge was installed. Waiting for Windows and Google Voice to publish both virtual speaker/microphone pairs."
		s.LastEvent = "audio-bridge-installed"
	})
	return voiceAudioInstallResult{
		OK:      true,
		Message: "Free virtual audio bridge installed with two speaker/microphone pairs. FlipAi will wire them automatically as soon as Windows exposes them.",
		Reboot:  exitCode == 3010,
	}
}

func downloadPinnedFile(url, destination, wantSHA string) error {
	if b, err := os.ReadFile(destination); err == nil {
		sum := sha256.Sum256(b)
		if strings.EqualFold(hex.EncodeToString(sum[:]), wantSHA) {
			return nil
		}
		_ = os.Remove(destination)
	}
	client := &http.Client{Timeout: 90 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "FlipAi-audio-bridge")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := destination + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 50<<20))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantSHA) {
		_ = os.Remove(tmp)
		return fmt.Errorf("SHA-256 mismatch (got %s)", got)
	}
	return os.Rename(tmp, destination)
}

func unzipSafe(zipPath, destination string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	base, _ := filepath.Abs(destination)
	for _, zf := range r.File {
		clean := filepath.Clean(filepath.FromSlash(zf.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return errors.New("archive contains an unsafe path")
		}
		out := filepath.Join(base, clean)
		absOut, _ := filepath.Abs(out)
		if absOut != base && !strings.HasPrefix(absOut, base+string(os.PathSeparator)) {
			return errors.New("archive path escaped the destination")
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		wf, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(wf, io.LimitReader(rc, 20<<20))
		rc.Close()
		wf.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func findNamedFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), name) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("downloaded package did not contain %s", name)
	}
	return found, nil
}

func findNefconX64(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(filepath.ToSlash(path))
		if strings.HasSuffix(lower, "/x64/nefconc.exe") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.New("nefcon package did not contain x64/nefconc.exe")
	}
	return found, nil
}

func verifyAuthenticodeValid(path string) error {
	quoted := strings.ReplaceAll(path, "'", "''")
	script := `$s=Get-AuthenticodeSignature -LiteralPath '` + quoted + `'; if($s.Status -ne 'Valid'){Write-Output ($s.Status.ToString()+': '+$s.StatusMessage); exit 2}; Write-Output $s.SignerCertificate.Subject`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return errors.New(text)
	}
	return nil
}

func runElevatedPowerShell(scriptPath string) (int, error) {
	p := strings.ReplaceAll(scriptPath, "'", "''")
	launcher := `$p=Start-Process powershell.exe -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList @('-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File','` + p + `'); Write-Output $p.ExitCode; exit 0`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", launcher)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(text+" "+err.Error()), "cancel") {
			return -1, errors.New("Administrator approval was canceled; the audio driver was not installed")
		}
		return -1, fmt.Errorf("could not start the elevated audio installer: %v %s", err, text)
	}
	text := strings.TrimSpace(string(out))
	var exit int
	if _, err := fmt.Sscanf(text, "%d", &exit); err != nil {
		return -1, fmt.Errorf("audio installer returned an unreadable result: %q", text)
	}
	return exit, nil
}

func audioDriverInstallPS(inf, sys, nefcon, logPath string) string {
	q := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	return fmt.Sprintf(`
$ErrorActionPreference='Stop'
$inf='%s'
$sys='%s'
$nefcon='%s'
$log='%s'
Start-Transcript -Path $log -Force | Out-Null
try {
  $sig=Get-AuthenticodeSignature -LiteralPath $sys
  if($sig.Status -ne 'Valid') { throw "Driver signature is $($sig.Status): $($sig.StatusMessage)" }
  Write-Output ("Validated driver signer: " + $sig.SignerCertificate.Subject)

  $hardware='Root\VirtualAudioDriver'
  $pattern='^Root\\VirtualAudioDriver$'
  $devices=@(Get-PnpDevice -ErrorAction SilentlyContinue | Where-Object { $_.HardwareID -match $pattern })
  if($devices.Count -eq 0) {
    & $nefcon install $inf $hardware --no-duplicates 2>&1 | ForEach-Object { Write-Output $_ }
    if($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne 3010) { throw "nefcon install failed with exit code $LASTEXITCODE" }
    Start-Sleep -Seconds 1
  }

  $devices=@(Get-PnpDevice -ErrorAction SilentlyContinue | Where-Object { $_.HardwareID -match $pattern })
  while($devices.Count -lt 2) {
    & $nefcon --create-device-node --hardware-id $hardware --class-name MEDIA --class-guid '4D36E96C-E325-11CE-BFC1-08002BE10318' 2>&1 | ForEach-Object { Write-Output $_ }
    if($LASTEXITCODE -ne 0) { throw "creating the second virtual audio device failed with exit code $LASTEXITCODE" }
    & $nefcon --install-driver --inf-path $inf 2>&1 | ForEach-Object { Write-Output $_ }
    if($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne 3010) { throw "binding the virtual audio driver failed with exit code $LASTEXITCODE" }
    Start-Sleep -Seconds 1
    $devices=@(Get-PnpDevice -ErrorAction SilentlyContinue | Where-Object { $_.HardwareID -match $pattern })
  }

  # Re-bind all instances to the verified package in case an older/bad package
  # had created a device node on this PC.
  & $nefcon --install-driver --inf-path $inf 2>&1 | ForEach-Object { Write-Output $_ }
  Start-Sleep -Seconds 3
  $devices=@(Get-PnpDevice -ErrorAction SilentlyContinue | Where-Object { $_.HardwareID -match $pattern })
  if($devices.Count -lt 2) { throw "Windows exposed only $($devices.Count) virtual audio device(s); FlipAi needs two" }
  foreach($d in $devices) {
    $problem=(Get-PnpDeviceProperty -InstanceId $d.InstanceId -KeyName 'DEVPKEY_Device_ProblemCode' -ErrorAction SilentlyContinue).Data
    Write-Output ("Device " + $d.InstanceId + " status=" + $d.Status + " problem=" + $problem)
    if($null -ne $problem -and [int]$problem -ne 0) { throw "Windows rejected a virtual audio device (problem code $problem). FlipAi did not change test-signing or Secure Boot; use a production-signed driver on this PC." }
  }
  Write-Output 'RESULT: two virtual audio device instances are installed and accepted by Windows.'
  Stop-Transcript | Out-Null
  exit 0
} catch {
  Write-Output ('ERROR: '+$_.Exception.Message)
  try { Stop-Transcript | Out-Null } catch {}
  exit 1
}
`, q(inf), q(sys), q(nefcon), q(logPath))
}
