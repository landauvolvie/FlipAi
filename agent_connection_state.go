package main

import "fmt"

// clearAgentCheck makes Disconnect mean "this agent is no longer connected to
// FlipAi" without signing the user out of the vendor CLI globally. The CLI
// account belongs to Windows/the user; FlipAi only owns whether it is currently
// treating that agent as connected and tested.
func (a *App) clearAgentCheck(which string) error {
	st := loadState(a.statePath)
	switch which {
	case "codex":
		st.CodexCheck = Check{}
	case "claude":
		st.ClaudeCheck = Check{}
	default:
		return fmt.Errorf("unknown agent check %q", which)
	}
	return saveState(a.statePath, st)
}
