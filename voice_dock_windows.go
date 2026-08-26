//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

// Docking puts the Google Voice window inside the FlipAi window.
//
// The two are separate processes on purpose: Google Voice has to stay signed in
// and listening for a call with the FlipAi window closed, so it cannot be a
// child of a window the user is free to close. What it can be is a borderless
// window placed exactly over the empty panel the Connections page reserves for
// it, and owned by the FlipAi window so it travels with it and never falls
// behind it. From the user's side that is simply Google Voice, side by side
// with the rest of the app -- no second window to find, and no popup.
//
// When the page stops reporting a rectangle -- it navigated away, the FlipAi
// window closed, the panel scrolled out of view -- the dock expires and the
// window goes back to being an ordinary background window, still running and
// still able to answer the phone.

var (
	procDockSetWindowPos    = voiceUser32.NewProc("SetWindowPos")
	procDockGetWindowLong   = pointerSizedWindowLong("GetWindowLongPtrW", "GetWindowLongW")
	procDockSetWindowLong   = pointerSizedWindowLong("SetWindowLongPtrW", "SetWindowLongW")
	procDockShowWindowAsync = voiceUser32.NewProc("ShowWindowAsync")
	procDockIsIconic        = voiceUser32.NewProc("IsIconic")
	procDockGetClientRect   = voiceUser32.NewProc("GetClientRect")
	procDockClientToScreen  = voiceUser32.NewProc("ClientToScreen")
)

const (
	gwlStyle       = -16
	gwlExStyle     = -20
	gwlpHWndParent = -8

	wsChild       = 0x40000000
	wsPopup       = 0x80000000
	wsVisible     = 0x10000000
	wsClipChild   = 0x02000000
	wsOverlapped  = 0x00CF0000 // WS_OVERLAPPEDWINDOW
	wsExToolWin   = 0x00000080
	wsExAppWindow = 0x00040000

	swpNoActivate   = 0x0010
	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002

	hwndTop = 0

	voiceSWShowNoActivate = 4
	voiceSWHide           = 0
)

// pointerSizedWindowLong picks the window-long call this Windows build has.
// Only 64-bit Windows exports the ...Ptr forms; asking for one on 32-bit
// panics inside syscall rather than returning an error, which would take the
// Google Voice window down with it.
func pointerSizedWindowLong(wide, narrow string) *syscall.LazyProc {
	p := voiceUser32.NewProc(wide)
	if p.Find() == nil {
		return p
	}
	return voiceUser32.NewProc(narrow)
}

// voiceDockController applies at most one window change per state change. The
// dock rectangle arrives several times a second and almost always says the same
// thing; re-styling and re-showing a window that many times a second would make
// the panel flicker and steal focus.
type dockRect struct{ Left, Top, Right, Bottom int32 }
type dockPoint struct{ X, Y int32 }

// screenRectFor turns one panel offset into the screen rectangle to place the
// window at, clipped to the FlipAi window's client area so a page that reports
// more than it can see cannot hang the panel over the rest of the app.
//
// It answers ok=false when the owner window cannot be measured, which is the
// same thing as having nowhere to dock.
func screenRectFor(owner uintptr, req VoiceDockRequest) (x, y, w, h int32, ok bool) {
	var client dockRect
	if r, _, _ := procDockGetClientRect.Call(owner, uintptr(unsafe.Pointer(&client))); r == 0 {
		return 0, 0, 0, 0, false
	}
	origin := dockPoint{}
	if r, _, _ := procDockClientToScreen.Call(owner, uintptr(unsafe.Pointer(&origin))); r == 0 {
		return 0, 0, 0, 0, false
	}
	left, top := int32(req.X), int32(req.Y)
	right, bottom := left+int32(req.Width), top+int32(req.Height)
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	if right > client.Right {
		right = client.Right
	}
	if bottom > client.Bottom {
		bottom = client.Bottom
	}
	if right-left < voiceDockMinSize || bottom-top < voiceDockMinSize {
		return 0, 0, 0, 0, false
	}
	return origin.X + left, origin.Y + top, right - left, bottom - top, true
}

type voiceDockController struct {
	hwnd    uintptr
	docked  bool
	placed  [4]int32 // the screen rectangle the window is currently standing on
	owner   uintptr
	restore bool // the undocked window should be shown rather than minimized
}

func newVoiceDockController(hwnd uintptr, visible bool) *voiceDockController {
	return &voiceDockController{hwnd: hwnd, restore: visible}
}

func windowStyle(hwnd uintptr, index int) uintptr {
	v, _, _ := procDockGetWindowLong.Call(hwnd, uintptr(index))
	return v
}

func setWindowStyle(hwnd uintptr, index int, value uintptr) {
	procDockSetWindowLong.Call(hwnd, uintptr(index), value)
}

// Apply moves the window to match one dock request. It returns whether the
// window is currently docked.
func (c *voiceDockController) Apply(req VoiceDockRequest, now time.Time, owner uintptr) bool {
	want := false
	var rect [4]int32
	if req.Active(now) && owner != 0 {
		if x, y, w, h, ok := screenRectFor(owner, req); ok {
			want, rect = true, [4]int32{x, y, w, h}
		}
	}
	switch {
	case want && !c.docked:
		c.dock(rect, owner)
	case want && c.docked:
		if owner != c.owner {
			c.setOwner(owner)
		}
		if rect != c.placed {
			c.place(rect)
			break
		}
		// The owner was minimized and restored, which takes the owned window
		// with it. Put it back where the page says it belongs.
		if iconic, _, _ := procDockIsIconic.Call(c.hwnd); iconic != 0 {
			c.place(rect)
		}
	case !want && c.docked:
		c.undock()
	}
	return c.docked
}

func (c *voiceDockController) dock(rect [4]int32, owner uintptr) {
	// A borderless window: no title bar to look like a second app, no resize
	// frame to drag it off the panel it is standing in for.
	style := windowStyle(c.hwnd, gwlStyle)
	style &^= wsOverlapped
	style &^= wsChild
	style |= wsPopup | wsVisible | wsClipChild
	setWindowStyle(c.hwnd, gwlStyle, style)

	// Out of the taskbar and out of Alt-Tab while it is part of the app window.
	ex := windowStyle(c.hwnd, gwlExStyle)
	ex &^= wsExAppWindow
	ex |= wsExToolWin
	setWindowStyle(c.hwnd, gwlExStyle, ex)

	c.setOwner(owner)
	c.docked = true
	c.place(rect)
}

// setOwner makes the FlipAi window this window's owner. An owned window is kept
// above its owner and is hidden and restored with it, which is what keeps the
// panel from being buried the moment the user clicks the app behind it.
func (c *voiceDockController) setOwner(owner uintptr) {
	setWindowStyle(c.hwnd, gwlpHWndParent, owner)
	c.owner = owner
}

func (c *voiceDockController) place(rect [4]int32) {
	c.placed = rect
	procDockShowWindowAsync.Call(c.hwnd, voiceSWShowNoActivate)
	procDockSetWindowPos.Call(c.hwnd, hwndTop,
		uintptr(rect[0]), uintptr(rect[1]), uintptr(rect[2]), uintptr(rect[3]),
		swpNoActivate|swpFrameChanged|swpShowWindow)
}

// undock returns the window to an ordinary one and puts it away again. It is
// deliberately not closed: the whole point of the separate process is that
// Google Voice keeps running, signed in, ready for a call.
func (c *voiceDockController) undock() {
	c.docked = false
	c.placed = [4]int32{}
	setWindowStyle(c.hwnd, gwlpHWndParent, 0)
	c.owner = 0

	style := windowStyle(c.hwnd, gwlStyle)
	style &^= wsPopup
	style |= wsOverlapped
	setWindowStyle(c.hwnd, gwlStyle, style)

	ex := windowStyle(c.hwnd, gwlExStyle)
	ex &^= wsExToolWin
	ex |= wsExAppWindow
	setWindowStyle(c.hwnd, gwlExStyle, ex)

	procDockSetWindowPos.Call(c.hwnd, hwndTop, 0, 0, 0, 0,
		swpNoActivate|swpNoMove|swpNoSize|swpNoZOrder|swpFrameChanged)
	if c.restore {
		procDockShowWindowAsync.Call(c.hwnd, voiceSWRestore)
		return
	}
	procDockShowWindowAsync.Call(c.hwnd, voiceSWMinimize)
}

// PopOut is the "open it in its own window" path: stop docking and hand the
// user a normal window they can move to another monitor.
func (c *voiceDockController) PopOut() {
	c.restore = true
	if c.docked {
		c.undock()
		return
	}
	procDockShowWindowAsync.Call(c.hwnd, voiceSWRestore)
}

// voiceDockOwner is the FlipAi window the panel belongs to, or 0 when there is
// no FlipAi window on screen to dock into.
func voiceDockOwner() uintptr {
	h := flipAiWindowHWND()
	if h == 0 {
		return 0
	}
	if iconic, _, _ := procDockIsIconic.Call(h); iconic != 0 {
		return 0
	}
	return h
}

// mutateVoiceDockState records whether the window is standing inside the FlipAi
// window right now, but only when the answer changes: this is asked several
// times a second and the runtime file is read by the page just as often.
var lastDockedState = -1

func mutateVoiceDockState(dataDir string, docked bool) {
	want := 0
	if docked {
		want = 1
	}
	if lastDockedState == want {
		return
	}
	lastDockedState = want
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) { s.Docked = docked })
}
