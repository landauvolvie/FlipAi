//go:build windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// The old "FlipAi Boot" task used an S4U BootTrigger to run --watchdog before
// Windows sign-in. Browser-backed providers cannot safely restore their
// WebView2 credentials in that non-interactive session, so the feature is
// retired. Keep this guard in the executable so upgraded PCs are safe even if
// Windows still has an old task registered from a previous release.
var retiredBootUser32 = syscall.NewLazyDLL("user32.dll")
var retiredBootGetProcessWindowStation = retiredBootUser32.NewProc("GetProcessWindowStation")
var retiredBootGetUserObjectInformationW = retiredBootUser32.NewProc("GetUserObjectInformationW")

const retiredBootUOIName = 2

func init() {
	if len(os.Args) < 2 {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(os.Args[1]))

	// The helper that used to create/remove the elevated BootTrigger is no
	// longer a supported command. Old callers get a harmless no-op.
	if mode == "--boot-task" {
		bestEffortDeleteRetiredBootTask()
		os.Exit(0)
	}
	if mode != "--watchdog" {
		return
	}

	// A real console OR RDP sign-in runs on the interactive WinSta0 window
	// station. The retired S4U BootTrigger runs on a non-interactive service
	// window station. Checking the station instead of comparing against the
	// physical console session is important: an RDP user is interactive even
	// though its session id differs from the machine's physical console id.
	if !watchdogHasInteractiveWindowStation() {
		bestEffortDeleteRetiredBootTask()
		os.Exit(0)
	}

	// An upgraded interactive install can also remove a leftover task quietly.
	// Failure is harmless: the guard above still makes that task inert next boot.
	bestEffortDeleteRetiredBootTask()
}

func watchdogHasInteractiveWindowStation() bool {
	station, _, _ := retiredBootGetProcessWindowStation.Call()
	if station == 0 {
		return false
	}
	var name [256]uint16
	var needed uint32
	r, _, _ := retiredBootGetUserObjectInformationW.Call(
		station,
		uintptr(retiredBootUOIName),
		uintptr(unsafe.Pointer(&name[0])),
		uintptr(len(name)*2),
		uintptr(unsafe.Pointer(&needed)),
	)
	if r == 0 {
		return false
	}
	return strings.EqualFold(syscall.UTF16ToString(name[:]), "WinSta0")
}

func bestEffortDeleteRetiredBootTask() {
	cmd := exec.Command("schtasks.exe", "/Delete", "/TN", bootTaskName, "/F")
	hideWindow(cmd)
	_ = cmd.Run()
}
