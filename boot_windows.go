//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Signing in is normally what starts FlipAi: the per-user Run entry fires when
// the account logs on. That leaves a gap after a reboot — nothing runs until
// somebody signs in. This file adds the other option: a Task Scheduler entry
// with a boot trigger, which Windows only allows an administrator to create.
// FlipAi asks for that approval exactly once, when the switch is turned on, and
// never during installation.

const bootTaskName = "FlipAi Boot"

var (
	procShellExecuteEx     = platformShell32.NewProc("ShellExecuteExW")
	procWaitForSingleObj   = kernel32.NewProc("WaitForSingleObject")
	procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
)

// bootStartupEnabled reports whether the boot task exists and is enabled. The
// task itself is the source of truth, so the UI cannot drift from Windows.
func bootStartupEnabled() bool {
	cmd := exec.Command("schtasks.exe", "/Query", "/TN", bootTaskName)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	text := string(out)
	return strings.Contains(text, bootTaskName) && !strings.Contains(strings.ToLower(text), "disabled")
}

// enableBootStartup and disableBootStartup re-enter FlipAi elevated. Creating a
// task that runs before sign-in needs administrator rights; nothing else in
// FlipAi does, so elevation is confined to this one child process.
func enableBootStartup(dataDir string) error  { return runBootHelper(dataDir, "install") }
func disableBootStartup(dataDir string) error { return runBootHelper(dataDir, "remove") }

func runBootHelper(dataDir, action string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	resultPath := bootResultPath(dataDir)
	_ = os.Remove(resultPath)
	code, err := runElevated(exe, "--boot-task", action)
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	if detail, readErr := os.ReadFile(resultPath); readErr == nil && len(detail) > 0 {
		return errors.New(strings.TrimSpace(string(detail)))
	}
	return fmt.Errorf("the elevated FlipAi helper exited with code %d", code)
}

func bootResultPath(dataDir string) string { return filepath.Join(dataDir, "boot-task-result.txt") }

// runBootTaskCommand is the elevated child. It performs one scheduled-task
// change, records any error where the parent can read it, and exits.
func runBootTaskCommand(dataDir string, args []string) int {
	action := ""
	if len(args) > 2 {
		action = args[2]
	}
	var err error
	switch action {
	case "install":
		err = createBootTask()
	case "remove":
		err = deleteBootTask()
	default:
		err = errors.New("unknown boot task action")
	}
	if err != nil {
		_ = os.WriteFile(bootResultPath(dataDir), []byte(err.Error()), 0o600)
		return 1
	}
	_ = os.Remove(bootResultPath(dataDir))
	return 0
}

func createBootTask() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	user := currentUserName()
	if user == "" {
		return errors.New("could not determine the Windows account to run FlipAi as")
	}
	xmlPath := filepath.Join(os.TempDir(), "flipai-boot-task.xml")
	defer os.Remove(xmlPath)
	if err := os.WriteFile(xmlPath, utf16LEWithBOM(bootTaskXML(exe, user)), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("schtasks.exe", "/Create", "/TN", bootTaskName, "/XML", xmlPath, "/F")
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Windows refused to create the startup task: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func deleteBootTask() error {
	cmd := exec.Command("schtasks.exe", "/Delete", "/TN", bootTaskName, "/F")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "cannot find") {
		return fmt.Errorf("Windows refused to remove the startup task: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func currentUserName() string {
	domain := os.Getenv("USERDOMAIN")
	user := os.Getenv("USERNAME")
	if user == "" {
		return ""
	}
	if domain != "" {
		return domain + `\` + user
	}
	return user
}

// bootTaskXML describes a task that starts FlipAi's watchdog at power-on under
// the same Windows account, without a stored password. S4U is what makes the
// no-password part possible; it is also why the credentials FlipAi keeps for
// this account are re-protected for the machine when this option is on.
func bootTaskXML(exe, user string) string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Author>FlipAi</Author>
    <Description>Starts the FlipAi SMS bridge when this PC powers on, before anyone signs in.</Description>
    <URI>\` + bootTaskName + `</URI>
  </RegistrationInfo>
  <Triggers>
    <BootTrigger>
      <Enabled>true</Enabled>
      <Delay>PT30S</Delay>
    </BootTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + xmlEscape(user) + `</UserId>
      <LogonType>S4U</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>6</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + xmlEscape(exe) + `</Command>
      <Arguments>--watchdog</Arguments>
      <WorkingDirectory>` + xmlEscape(filepath.Dir(exe)) + `</WorkingDirectory>
    </Exec>
  </Actions>
</Task>`
}

func xmlEscape(v string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(v)
}

// utf16LEWithBOM encodes the task definition the way schtasks /XML expects it.
func utf16LEWithBOM(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(units)*2+2)
	out = append(out, 0xFF, 0xFE)
	var buf [2]byte
	for _, u := range units {
		binary.LittleEndian.PutUint16(buf[:], u)
		out = append(out, buf[0], buf[1])
	}
	return out
}

// ---------------------------------------------------------------------------
// Elevation
// ---------------------------------------------------------------------------

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	swHide                = 0
)

// runElevated starts one child process with the "runas" verb, which is what
// produces the Windows consent prompt, and waits for it. FlipAi itself keeps
// running unelevated.
func runElevated(exe string, args ...string) (int, error) {
	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	file, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	params, err := syscall.UTF16PtrFromString(strings.Join(args, " "))
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		nShow:        swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		// ERROR_CANCELLED (1223) is the user declining the consent prompt.
		if errno, ok := callErr.(syscall.Errno); ok && errno == 1223 {
			return 0, errors.New("Windows administrator approval was declined")
		}
		return 0, fmt.Errorf("could not start the elevated FlipAi helper: %v", callErr)
	}
	if info.hProcess == 0 {
		return 0, errors.New("Windows did not return a handle for the elevated helper")
	}
	defer procCloseHandle.Call(info.hProcess)
	// 60s is far longer than schtasks needs and still bounded, so a stuck
	// helper cannot hang the settings page.
	procWaitForSingleObj.Call(info.hProcess, uintptr(60*time.Second/time.Millisecond))
	var code uint32
	ok, _, _ := procGetExitCodeProcess.Call(info.hProcess, uintptr(unsafe.Pointer(&code)))
	if ok == 0 {
		return 0, errors.New("could not read the elevated helper's result")
	}
	return int(code), nil
}
