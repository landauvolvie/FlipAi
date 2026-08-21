package main

import "testing"

func TestTrayOpenDoesNotQuit(t *testing.T) {
	opened, quit := false, false
	shouldClose := handleTrayCommand(trayOpenID, trayActionSet{
		onOpen: func() { opened = true },
		onQuit: func() { quit = true },
	})
	if !opened {
		t.Fatal("Open Settings callback was not called")
	}
	if quit {
		t.Fatal("Open Settings must not call Quit")
	}
	if shouldClose {
		t.Fatal("Open Settings must keep the tray process alive")
	}
}

func TestTrayQuitStopsBridge(t *testing.T) {
	opened, quit := false, false
	shouldClose := handleTrayCommand(trayQuitID, trayActionSet{
		onOpen: func() { opened = true },
		onQuit: func() { quit = true },
	})
	if opened {
		t.Fatal("Quit must not open settings")
	}
	if !quit {
		t.Fatal("Quit callback was not called")
	}
	if !shouldClose {
		t.Fatal("Quit must close the tray process")
	}
}
