package main

// ui_pages.go registers the legacy page templates during package init. This
// file sorts after it, so the redesigned Agents and Advanced templates are the
// final registrations used by the desktop UI.
func init() {
	registerPage("agents", organizedAgentsHTML)
	registerPage("advanced", organizedAdvancedHTML)
}
