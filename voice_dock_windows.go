//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

// Docking puts the Google Voice window inside the FlipAi window, and parking
// puts it out of sight without ever letting it become a window of its own.
//
// The two are separate processes on purpose: Google Voice has to stay signed in
// and listening for a call with the FlipAi window closed, so it cannot be a
// child of a window the user is free to close. What it can be is a borderless
// window with no title bar, no taskbar button and no Alt-Tab entry, placed
// exactly over the empty panel the Connections page reserves for it and owned
// by the FlipAi window so it travels with it. From the user's side that is
// simply Google Voice, inside the app.
//
// When the page stops reporting a rectangle -- it navigated away, the FlipAi
// window closed, the panel scrolled out of view -- the window is moved off the
// desktop entirely rather than being restored as an ordinary window. That is
// the difference between this and what came before, and it is the whole of
// "why is a browser window appearing on my desktop": there is now no state in
// which this window is visible anywhere except inside FlipAi.
//
// It is parked rather than minimized because a minimized browser window is one
// Chromium is entitled to treat as hidden: it backgrounds the renderer and
// throttles its timers, and a Google Voice page in that state can stop
// noticing that the phone is ringing. A window that is off-screen is, as far
// as the page is concerned, a window that is on screen.

var (
	procDockSetWindowPos    = voiceUser32.NewProc("SetWindowPos")
	procDockGetWindowLong   = pointerSizedWindowLong("GetWindowLongPtrW", "GetWindowLongW")
	procDockSetWindowLong   = pointerSizedWindowLong("SetWindowLongPtrW", "SetWindowLongW")
	procDockShowWindowAsync = voiceUser32.NewProc("ShowWindowAsync")
	procDockIsIconic        = voiceUser32.NewProc("IsIconic")
	procDockGetClientRect   = voiceUser32.NewProc("GetClientRect")
	procDockClientToScreen  = voiceUser32.NewProc("ClientToScreen")
	procDockSendMessage     = voiceUser32.NewProc("SendMessageW")
	procDockGetSystemMetric = voiceUser32.NewProc("GetSystemMetrics")
)

const (
	gwlStyle       = -16
	gwlExStyle     = -20
	gwlpHWndParent = -8

	wsChild        = 0x40000000
	wsPopup        = 0x80000000
	wsVisible      = 0x10000000
	wsClipChild    = 0x02000000
	wsOverlapped   = 0x00CF0000 // WS_OVERLAPPEDWINDOW
	wsExToolWin    = 0x00000080
	wsExAppWindow  = 0x00040000
	wsExNoActivate = 0x08000000

	swpNoActivate   = 0x0010
	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002

	hwndTop = 0

	voiceSWShowNoActivate = 4

	wmSize = 0x0005

	// SM_REMOTESESSION: this desktop is being viewed over Remote Desktop.
	smRemoteSession = 0x1000
)

// forceBrowserRelayout makes the browser re-measure the window it is sitting in.
//
// The binding lays the browser out only when it receives WM_SIZE. Taking the
// title bar off a window, or putting it back, changes the client area without
// changing the window size -- so no WM_SIZE arrives, and the browser keeps
// bounds that no longer match the window it is in. What that looks like is the
// page shifted up under the title bar with a blank strip along the bottom, and
// it is why an undocked Google Voice window came back unusable.
func forceBrowserRelayout(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	procDockSendMessage.Call(hwnd, wmSize, 0, 0)
}

// remoteSession reports whether this desktop is being viewed over Remote
// Desktop, where GPU compositing is unavailable and WebView2 is prone to
// painting nothing at all.
func remoteSession() bool {
	r, _, _ := procDockGetSystemMetric.Call(smRemoteSession)
	return r != 0
}

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

// voiceDockController applies at most one window change per state change. The
// dock rectangle arrives several times a second and almost always says the same
// thing; re-styling and re-showing a window that many times a second would make
// the panel flicker and steal focus.
type voiceDockController struct {
	hwnd    uintptr
	docked  bool
	placed  [4]int32 // the screen rectangle the window is currently standing on
	owner   uintptr
	blocked string // why the window is not in the panel, when it is not
	parked  bool   // the window has already been put off the desktop
}

func newVoiceDockController(hwnd uintptr) *voiceDockController {
	return &voiceDockController{hwnd: hwnd}
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
	// Why not, when not. "FlipAi could not put it in this panel" with no cause
	// attached is a dead end for whoever reads it.
	switch {
	case !req.Visible || req.At.IsZero():
		c.blocked = "the Connections page is not asking for the panel; open Connections in FlipAi and leave it on screen."
	case req.Width < voiceDockMinSize || req.Height < voiceDockMinSize:
		c.blocked = "the panel on the page is too small to put a browser in; make the FlipAi window larger."
	case now.Sub(req.At) >= voiceDockTTL:
		c.blocked = "the Connections page stopped reporting where the panel is; it may be scrolled out of view or the FlipAi window may be minimized."
	case owner == 0:
		c.blocked = "FlipAi could not find its own window on screen, so there is nowhere to put the panel. It is minimized, or another copy of FlipAi owns it."
	default:
		c.blocked = ""
	}
	if req.Active(now) && owner != 0 {
		if x, y, w, h, ok := screenRectFor(owner, req); ok {
			want, rect = true, [4]int32{x, y, w, h}
		} else {
			c.blocked = "the FlipAi window could not be measured, so the panel position could not be worked out."
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
			break
		}
		c.keepAbove()
	case !want && c.docked:
		c.undock()
	}
	return c.docked
}

func (c *voiceDockController) dock(rect [4]int32, owner uintptr) {
	applyVoiceWindowChrome(c.hwnd, true)

	c.setOwner(owner)
	c.docked = true
	c.parked = false
	forceBrowserRelayout(c.hwnd)
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

// keepAbove puts the panel directly above the FlipAi window in the stack
// without moving, resizing, or activating it.
//
// Owning the window should be enough on its own: an owned window is always
// kept above its owner. But the owner is set across process boundaries, which
// Windows is entitled to refuse, and a refusal is silent. Without this the
// panel would simply vanish behind the app the first time the user clicked the
// page next to it, with nothing to explain where it went.
func (c *voiceDockController) keepAbove() {
	if c.owner == 0 {
		return
	}
	procDockSetWindowPos.Call(c.hwnd, c.owner, 0, 0, 0, 0,
		swpNoActivate|swpNoMove|swpNoSize)
}

// undock takes the window out of the panel and parks it off the desktop.
//
// It is deliberately not closed and deliberately not restored: the whole point
// of the separate process is that Google Voice keeps running, signed in and
// ready for a call, and the whole point of parking is that it does so without
// ever appearing anywhere the user can see it.
func (c *voiceDockController) undock() {
	c.docked = false
	c.placed = [4]int32{}
	setWindowStyle(c.hwnd, gwlpHWndParent, 0)
	c.owner = 0
	c.park()
}

// park moves the window beyond every display and makes sure it can never take
// focus or show up in the taskbar while it is there.
func (c *voiceDockController) park() {
	if c.parked {
		return
	}
	c.parked = true
	// The window is created at this position already, but its frame still
	// carries the binding's default title bar; taking it off here is what makes
	// the panel borderless the first time it is docked.

	applyVoiceWindowChrome(c.hwnd, false)
	x, y := parkedWindowOrigin()
	procDockSetWindowPos.Call(c.hwnd, hwndTop,
		uintptr(x), uintptr(y), uintptr(voiceParkedWidth), uintptr(voiceParkedHeight),
		swpNoActivate|swpNoZOrder|swpFrameChanged|swpShowWindow)
	forceBrowserRelayout(c.hwnd)
}

// voiceParkedWidth/Height keep the page laid out as a real browser window even
// while nobody can see it. A one-pixel window would make Google Voice render
// its narrow mobile layout, and the ringing card FlipAi has to find is not the
// same card there.
const (
	voiceParkedWidth  = 1180
	voiceParkedHeight = 860
)

// parkedWindowOrigin is a point past the right-hand edge and below the bottom
// of every display, so the window cannot be seen on any monitor arrangement.
func parkedWindowOrigin() (int32, int32) {
	const (
		smXVirtualScreen  = 76
		smYVirtualScreen  = 77
		smCXVirtualScreen = 78
		smCYVirtualScreen = 79
	)
	vx, _, _ := procDockGetSystemMetric.Call(smXVirtualScreen)
	vy, _, _ := procDockGetSystemMetric.Call(smYVirtualScreen)
	vw, _, _ := procDockGetSystemMetric.Call(smCXVirtualScreen)
	vh, _, _ := procDockGetSystemMetric.Call(smCYVirtualScreen)
	x := int32(vx) + int32(vw) + 64
	y := int32(vy) + int32(vh) + 64
	// A machine that reports nothing useful still has to end up somewhere off
	// the primary display rather than at the top-left corner of it.
	if int32(vw) == 0 || int32(vh) == 0 {
		x, y = 32000, 32000
	}
	return x, y
}

// applyVoiceWindowChrome is the one place the Google Voice window's styles are
// decided. There are exactly two: standing in the FlipAi panel, and parked.
// Neither has a title bar, a taskbar button or an Alt-Tab entry, so there is no
// path through this code that produces a second app window on the desktop.
func applyVoiceWindowChrome(hwnd uintptr, docked bool) {
	if hwnd == 0 {
		return
	}
	style := windowStyle(hwnd, gwlStyle)
	style &^= wsOverlapped
	style &^= wsChild
	style |= wsPopup | wsVisible | wsClipChild
	setWindowStyle(hwnd, gwlStyle, style)

	ex := windowStyle(hwnd, gwlExStyle)
	ex &^= wsExAppWindow
	ex |= wsExToolWin
	if docked {
		// Docked, the user types into it: signing in to Google is a real
		// keyboard task and a window that refuses activation cannot be typed
		// into.
		ex &^= wsExNoActivate
	} else {
		// Parked, it must never take focus from whatever the user is doing.
		ex |= wsExNoActivate
	}
	setWindowStyle(hwnd, gwlExStyle, ex)
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
var lastDockBlocked = "\x00"

func mutateVoiceDockState(dataDir string, docked bool, blocked string) {
	want := 0
	if docked {
		want = 1
	}
	if lastDockedState == want && lastDockBlocked == blocked {
		return
	}
	lastDockedState, lastDockBlocked = want, blocked
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.Docked = docked
		s.DockBlocked = blocked
	})
}
