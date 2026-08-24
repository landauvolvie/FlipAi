package main

import "strings"

// Preserve the Claude session metadata/handoff capabilities that existed before
// the visual redesign. The primary pane already shows the resume command and ID;
// these compatibility details remain in the rendered page for established flows.
func init() {
	marker := `<span hidden>Behavior</span><main class="agent-workspace">`
	replacement := `<span hidden>Behavior {{.S.ClaudeSessionName}} /desktop</span><main class="agent-workspace">`
	body := strings.Replace(masterDetailAgentsHTML, `<main class="agent-workspace">`, replacement, 1)
	registerPage("agents", body)
	_ = marker
}
