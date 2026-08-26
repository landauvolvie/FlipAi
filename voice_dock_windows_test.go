//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// "Open it in its own window" is a one-shot. Docking the window again settles
// where it belongs when it is next put away -- in the background.
//
// The flag used to be permanent: one pop-out and every later undock, on a page
// navigation or an expired dock, restored Google Voice on top of whatever the
// user had moved on to.
func TestPoppingOutOnceDoesNotOutliveTheNextDock(t *testing.T) {
	// hwnd 0 is not a window, so every Win32 call below is a harmless no-op and
	// what is under test is the state machine that decides them.
	c := newVoiceDockController(0, false)

	c.Apply(VoiceDockRequest{PopOut: true, At: time.Now()}, time.Now(), 0)
	if !c.restore {
		t.Fatal("a pop-out request did not ask for the window to be shown")
	}
	if c.docked {
		t.Fatal("a pop-out request docked the window")
	}

	c.dock([4]int32{10, 20, 900, 700}, 0)
	if !c.docked {
		t.Fatal("the window did not dock")
	}
	if c.restore {
		t.Error("docking left the earlier pop-out standing, so the next undock will float the window")
	}

	c.undock()
	if c.docked {
		t.Error("the window is still docked after undocking")
	}
}

// A window the user opened deliberately, and never docked, is still theirs to
// keep on screen.
func TestAWindowOpenedOnPurposeStaysOnScreenUntilItIsDocked(t *testing.T) {
	c := newVoiceDockController(0, true)
	if !c.restore {
		t.Fatal("a window opened visibly should not be put away on the first undock")
	}
	c.dock([4]int32{0, 0, 900, 700}, 0)
	if c.restore {
		t.Error("docking should settle the window into the app")
	}
}

// Taking the title bar off a window, or putting it back, changes the client
// area without changing the window size. The browser only lays itself out on
// WM_SIZE, so a style change with no size change left it measuring a window it
// was no longer in: the page came back shifted up under the title bar with a
// blank strip along the bottom. Every style change has to be followed by a
// re-layout.
func TestEveryFrameChangeReLaysOutTheBrowser(t *testing.T) {
	source, err := os.ReadFile("voice_dock_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, fn := range []string{"func (c *voiceDockController) dock(", "func (c *voiceDockController) undock("} {
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
	// And undocking must resize rather than keep the docked geometry, or the
	// browser is never told at all.
	i := strings.Index(body, "func (c *voiceDockController) undock(")
	end := strings.Index(body[i:], "\n}\n")
	if strings.Contains(body[i:i+end], "swpNoSize") {
		t.Error("undocking still asks Windows not to resize, so no WM_SIZE reaches the browser")
	}
}

// Not being able to place the window has to come with a reason. "FlipAi could
// not put it in this panel" on its own is a dead end for whoever reads it.
func TestBeingUnableToDockSaysWhy(t *testing.T) {
	c := newVoiceDockController(0, false)

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

	c.Apply(VoiceDockRequest{PopOut: true, At: time.Now()}, time.Now(), 0)
	if !strings.Contains(c.blocked, "own window") {
		t.Errorf("a popped-out window said: %q", c.blocked)
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
