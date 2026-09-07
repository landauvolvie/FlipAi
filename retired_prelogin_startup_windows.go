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
var retiredBootKernel32 = syscall.NewLazyDLL("kernel32.dll")
var retiredBootProcessIDToSessionID = retiredBootKernel32.NewProc("ProcessIdToSessionId")
var retiredBootActiveConsoleSessionID = retiredBootKernel32.NewProc("WTSGetActiveConsoleSessionId")

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

	// Normal HKCU Run startup happens after the user signs in and therefore
	// shares the active console session. The retired S4U boot task runs outside
	// that interactive session; stop it before it can start the host, tray, or
	// any persistent WebView2 profile.
	if !watchdogInActiveConsoleSession() {
		bestEffortDeleteRetiredBootTask()
		os.Exit(0)
	}

	// An upgraded interactive install can also remove a leftover task quietly.
	// Failure is harmless: the guard above still makes that task inert next boot.
	bestEffortDeleteRetiredBootTask()
}

func watchdogInActiveConsoleSession() bool {
	var processSession uint32
	r, _, _ := retiredBootProcessIDToSessionID.Call(
		uintptr(os.Getpid()),
		uintptr(unsafe.Pointer(&processSession)),
	)
	if r == 0 {
		return false
	}
	active, _, _ := retiredBootActiveConsoleSessionID.Call()
	activeSession := uint32(active)
	if activeSession == 0xffffffff {
		return false
	}
	return processSession == activeSession
}

func bestEffortDeleteRetiredBootTask() {
	cmd := exec.Command("schtasks.exe", "/Delete", "/TN", bootTaskName, "/F")
	hideWindow(cmd)
	_ = cmd.Run()
}
