//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmNull          = 0x0000
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmContextMenu   = 0x007B
	wmApp           = 0x8000
	trayMessage     = wmApp + 1

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	idiApplication = 32512
	idcArrow       = 32512
)

type trayPoint struct{ X, Y int32 }
type trayMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      trayPoint
}
type trayWndClassEx struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}
type trayGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}
type trayNotifyIconData struct {
	CbSize           uint32
	Hwnd             uintptr
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             uintptr
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUID             trayGUID
	BalloonIcon      uintptr
}

var (
	trayUser32                 = syscall.NewLazyDLL("user32.dll")
	trayShell32                = syscall.NewLazyDLL("shell32.dll")
	trayKernel32               = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClassExW       = trayUser32.NewProc("RegisterClassExW")
	procCreateWindowExW        = trayUser32.NewProc("CreateWindowExW")
	procDefWindowProcW         = trayUser32.NewProc("DefWindowProcW")
	procDestroyWindow          = trayUser32.NewProc("DestroyWindow")
	procGetMessageW            = trayUser32.NewProc("GetMessageW")
	procTranslateMessage       = trayUser32.NewProc("TranslateMessage")
	procDispatchMessageW       = trayUser32.NewProc("DispatchMessageW")
	procPostQuitMessage        = trayUser32.NewProc("PostQuitMessage")
	procPostMessageW           = trayUser32.NewProc("PostMessageW")
	procLoadIconW              = trayUser32.NewProc("LoadIconW")
	procLoadCursorW            = trayUser32.NewProc("LoadCursorW")
	procCreatePopupMenu        = trayUser32.NewProc("CreatePopupMenu")
	procAppendMenuW            = trayUser32.NewProc("AppendMenuW")
	procDestroyMenu            = trayUser32.NewProc("DestroyMenu")
	procTrackPopupMenu         = trayUser32.NewProc("TrackPopupMenu")
	procGetCursorPos           = trayUser32.NewProc("GetCursorPos")
	procSetForegroundWindow    = trayUser32.NewProc("SetForegroundWindow")
	procRegisterWindowMessageW = trayUser32.NewProc("RegisterWindowMessageW")
	procShellNotifyIconW       = trayShell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW       = trayKernel32.NewProc("GetModuleHandleW")
	procCreateMutexW           = trayKernel32.NewProc("CreateMutexW")
	procReleaseMutex           = trayKernel32.NewProc("ReleaseMutex")
	procCloseHandle            = trayKernel32.NewProc("CloseHandle")
)

var (
	activeTrayActions    trayActionSet
	activeTrayNID        *trayNotifyIconData
	activeTaskbarCreated uint32
)

func showTrayMenu(hwnd uintptr) bool {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return false
	}
	defer procDestroyMenu.Call(menu)
	openText, _ := syscall.UTF16PtrFromString("Open FlipAi Settings")
	quitText, _ := syscall.UTF16PtrFromString("Quit FlipAi")
	procAppendMenuW.Call(menu, mfString, trayOpenID, uintptr(unsafe.Pointer(openText)))
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	procAppendMenuW.Call(menu, mfString, trayQuitID, uintptr(unsafe.Pointer(quitText)))
	var pt trayPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	return handleTrayCommand(cmd, activeTrayActions)
}

func trayWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if activeTaskbarCreated != 0 && message == activeTaskbarCreated {
		if activeTrayNID != nil {
			procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(activeTrayNID)))
		}
		return 0
	}
	switch message {
	case trayMessage:
		event := uint32(lParam & 0xffff)
		switch event {
		case wmLButtonDblClk:
			handleTrayCommand(trayOpenID, activeTrayActions)
		case wmRButtonUp, wmContextMenu:
			if showTrayMenu(hwnd) {
				procDestroyWindow.Call(hwnd)
			}
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func runSystemTray(ctx context.Context, tooltip string, onOpen, onQuit func()) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString(fmt.Sprintf("FlipAiTray_%d", os.Getpid()))
	windowName, _ := syscall.UTF16PtrFromString("FlipAi")
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	icon, _, _ := procLoadIconW.Call(0, idiApplication)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	callback := syscall.NewCallback(trayWindowProc)
	wc := trayWndClassEx{
		CbSize:    uint32(unsafe.Sizeof(trayWndClassEx{})),
		WndProc:   callback,
		Instance:  hInstance,
		Icon:      icon,
		Cursor:    cursor,
		ClassName: className,
		IconSm:    icon,
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return fmt.Errorf("register tray window: %v", err)
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowName)), 0, 0, 0, 0, 0, 0, 0, hInstance, 0)
	if hwnd == 0 {
		return fmt.Errorf("create tray window: %v", err)
	}
	defer procDestroyWindow.Call(hwnd)

	taskbarName, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	taskbarMsg, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(taskbarName)))
	activeTaskbarCreated = uint32(taskbarMsg)
	activeTrayActions = trayActionSet{onOpen: onOpen, onQuit: onQuit}
	defer func() {
		activeTrayActions = trayActionSet{}
		activeTrayNID = nil
		activeTaskbarCreated = 0
	}()

	nid := trayNotifyIconData{CbSize: uint32(unsafe.Sizeof(trayNotifyIconData{})), Hwnd: hwnd, ID: 1, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: trayMessage, Icon: icon}
	copy(nid.Tip[:], syscall.StringToUTF16(tooltip))
	activeTrayNID = &nid
	if ok, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); ok == 0 {
		return fmt.Errorf("add tray icon: %v", err)
	}
	defer procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))

	go func() {
		<-ctx.Done()
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
	}()

	var msg trayMsg
	for {
		r, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) == -1 {
			return fmt.Errorf("tray message loop: %v", err)
		}
		if r == 0 {
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func acquireWatchdogInstance() (func(), bool, error) {
	name, _ := syscall.UTF16PtrFromString(`Local\AISMSBridgeWatchdog`)
	h, _, err := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return func() {}, false, fmt.Errorf("create watchdog mutex: %v", err)
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 183 {
		procCloseHandle.Call(h)
		return func() {}, false, nil
	}
	release := func() { procReleaseMutex.Call(h); procCloseHandle.Call(h) }
	return release, true, nil
}
