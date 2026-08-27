//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The whole complaint this rewrite answers is "a browser window keeps appearing
// on my desktop". There are now exactly two places the Google Voice window can
// be -- inside the FlipAi panel, or parked off every display -- and this is the
// test that says so. A window that is not docked is parked; it is never
// restored, never shown, and never given a title bar back.
func TestAnUndockedGoogleVoiceWindowIsParkedRatherThanShown(t *testing.T) {
	body := dockSource(t)
	i := strings.Index(body, "func (c *voiceDockController) undock(")
	if i < 0 {
		t.Fatal("undock is gone")
	}
	end := strings.Index(body[i:], "\n}\n")
	if end < 0 {
		t.Fatal("could not read the body of undock")
	}
	undock := body[i : i+end]
	if !strings.Contains(undock, "c.park()") {
		t.Error("undocking no longer parks the window, so Google Voice can appear as a window of its own")
	}
	for _, forbidden := range []string{"voiceSWRestore", "voiceSWMinimize", "wsExAppWindow", "wsOverlapped"} {
		if strings.Contains(undock, forbidden) {
			t.Errorf("undocking still uses %s, which turns Google Voice back into an ordinary desktop window", forbidden)
		}
	}
}

// Parking must put the window somewhere no monitor can show it, and must take
// away its ability to steal focus while it is there.
func TestParkingPutsTheWindowBeyondEveryDisplay(t *testing.T) {
	x, y := parkedWindowOrigin()
	if x <= 0 || y <= 0 {
		t.Fatalf("the parked position (%d,%d) is not past the desktop", x, y)
	}
	body := dockSource(t)
	i := strings.Index(body, "func (c *voiceDockController) park(")
	if i < 0 {
		t.Fatal("park is gone")
	}
	end := strings.Index(body[i:], "\n}\n")
	park := body[i : i+end]
	if !strings.Contains(park, "swpNoActivate") {
		t.Error("parking the window can take focus from whatever the user is doing")
	}
	if !strings.Contains(park, "applyVoiceWindowChrome(c.hwnd, false)") {
		t.Error("parking does not re-apply the no-activate, no-taskbar chrome")
	}
}

// Whichever of the two states the window is in, it is never in the taskbar and
// never in Alt-Tab. Those are the two places a user would see "a second app".
func TestTheGoogleVoiceWindowIsNeverAnAppWindow(t *testing.T) {
	body := dockSource(t)
	i := strings.Index(body, "func applyVoiceWindowChrome(")
	if i < 0 {
		t.Fatal("applyVoiceWindowChrome is gone")
	}
	end := strings.Index(body[i:], "\n}\n")
	chrome := body[i : i+end]
	if !strings.Contains(chrome, "ex &^= wsExAppWindow") {
		t.Error("the Google Voice window can still claim a taskbar button")
	}
	if !strings.Contains(chrome, "ex |= wsExToolWin") {
		t.Error("the Google Voice window can still appear in Alt-Tab")
	}
	if !strings.Contains(chrome, "style &^= wsOverlapped") {
		t.Error("the Google Voice window can still have a title bar")
	}
	// Docked it must be typeable -- signing in to Google is a keyboard task.
	if !strings.Contains(chrome, "ex &^= wsExNoActivate") {
		t.Error("a docked Google Voice panel that refuses activation cannot be signed in to")
	}
}

// Taking the title bar off a window, or putting it back, changes the client
// area without changing the window size. The browser only lays itself out on
// WM_SIZE, so a style change with no size change left it measuring a window it
// was no longer in: the page came back shifted up under the title bar with a
// blank strip along the bottom. Every style change has to be followed by a
// re-layout.
func TestEveryFrameChangeReLaysOutTheBrowser(t *testing.T) {
	body := dockSource(t)
	for _, fn := range []string{"func (c *voiceDockController) dock(", "func (c *voiceDockController) park("} {
		i := strings.Index(body, fn)
		if i < 0 {
			t.Fatalf("%s is gone", fn)
		}
		end := strings.Index(body[i:], "\n}\n")
		if end < 0 {
			t.Fatalf("could not read the body of %s", fn)
		}
		if !strings.Contains(body[i:i+end], "forceBrowserRelayout") {
			t.Errorf("%s changes the window frame without telling the browser to re-measure", fn)
		}
	}
	i := strings.Index(body, "func (c *voiceDockController) park(")
	end := strings.Index(body[i:], "\n}\n")
	if strings.Contains(body[i:i+end], "swpNoSize") {
		t.Error("parking still asks Windows not to resize, so no WM_SIZE reaches the browser")
	}
}

// A parked window keeps a real browser size. At one pixel Google Voice renders
// its narrow layout, whose ringing card is not the one FlipAi looks for.
func TestAParkedWindowKeepsADesktopSizedViewport(t *testing.T) {
	if voiceParkedWidth < 900 || voiceParkedHeight < 600 {
		t.Fatalf("a parked window of %dx%d is small enough to change how Google Voice lays itself out", voiceParkedWidth, voiceParkedHeight)
	}
}

// Not being able to place the window has to come with a reason. "FlipAi could
// not put it in this panel" on its own is a dead end for whoever reads it.
func TestBeingUnableToDockSaysWhy(t *testing.T) {
	// hwnd 0 is not a window, so every Win32 call below is a harmless no-op and
	// what is under test is the state machine that decides them.
	c := newVoiceDockController(0)

	c.Apply(VoiceDockRequest{}, time.Now(), 0)
	if !strings.Contains(c.blocked, "not asking for the panel") {
		t.Errorf("a page that is not asking said: %q", c.blocked)
	}

	c.Apply(VoiceDockRequest{Visible: true, Width: 900, Height: 700, At: time.Now().Add(-time.Minute)}, time.Now(), 0)
	if !strings.Contains(c.blocked, "stopped reporting") {
		t.Errorf("an expired panel said: %q", c.blocked)
	}

	c.Apply(VoiceDockRequest{Visible: true, Width: 20, Height: 20, At: time.Now()}, time.Now(), 0)
	if !strings.Contains(c.blocked, "too small") {
		t.Errorf("a sliver of a panel said: %q", c.blocked)
	}

	c.Apply(VoiceDockRequest{Visible: true, Width: 900, Height: 700, At: time.Now()}, time.Now(), 0)
	if !strings.Contains(c.blocked, "could not find its own window") {
		t.Errorf("a missing FlipAi window said: %q", c.blocked)
	}
}

// Over Remote Desktop there is no GPU to composite with, and a hardware-drawn
// WebView2 paints black. Software rendering has to be what is tried first
// there, and Retry has to move along the list rather than repeat itself.
func TestRemoteDesktopDrawsInSoftwareFirst(t *testing.T) {
	modes := googleVoiceRenderModes()
	if len(modes) < 2 {
		t.Fatal("there is no second way to draw the window")
	}
	names := make([]string, 0, len(modes))
	for _, m := range modes {
		names = append(names, m.name)
	}
	want := "hardware"
	if remoteSession() {
		want = "software"
	}
	if names[0] != want {
		t.Errorf("on this session the first way to draw should be %q, got %v", want, names)
	}
	seen := map[string]bool{}
	for _, m := range modes {
		if seen[m.name] {
			t.Errorf("%q is offered twice, so Retry would repeat itself: %v", m.name, names)
		}
		seen[m.name] = true
	}
	var software googleVoiceRenderMode
	for _, m := range modes {
		if m.name == "software" {
			software = m
		}
	}
	if !strings.Contains(software.args, "--disable-gpu") {
		t.Errorf("software drawing does not turn the graphics card off: %q", software.args)
	}
}

func dockSource(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("voice_dock_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	// Checked out on Windows this file has CRLF line endings, so anything
	// looking for a bare newline finds nothing at all.
	return strings.ReplaceAll(string(source), "\r\n", "\n")
}
