package main

const (
	trayOpenID = 1001
	trayQuitID = 1002
)

type trayActionSet struct {
	onOpen func()
	onQuit func()
}

// handleTrayCommand is intentionally platform-neutral so its semantics can be
// tested even on CI machines that do not have an interactive Windows shell.
func handleTrayCommand(id uintptr, actions trayActionSet) bool {
	switch id {
	case trayOpenID:
		if actions.onOpen != nil {
			actions.onOpen()
		}
		return false
	case trayQuitID:
		if actions.onQuit != nil {
			actions.onQuit()
		}
		return true
	default:
		return false
	}
}
