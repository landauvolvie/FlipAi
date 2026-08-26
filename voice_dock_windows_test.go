//go:build windows

package main

import (
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
