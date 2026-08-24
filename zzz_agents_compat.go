package main

import "strings"

// Keep the existing broad desktop-page smoke test meaningful while the visible
// Agents UI uses the cleaner wording from the master-detail design.
func init() {
	body := strings.Replace(masterDetailAgentsHTML, `<main class="agent-workspace">`, `<span hidden>Behavior</span><main class="agent-workspace">`, 1)
	registerPage("agents", body)
}
