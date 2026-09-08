package main

import "html/template"

// Several provider UI files intentionally replace pages late during package
// initialization. Re-apply the updater shell after all of those page-specific
// passes so no late registration can resurrect the retired download-arrow
// shell. This is a compatibility guard for the existing layered UI system;
// updaterShellHTML remains the single definition of the updater chrome.
func init() {
	updatedShell := updaterShellHTML()
	for _, page := range uiPages {
		page.Funcs(template.FuncMap{"updaterState": updaterUIState, "updaterPercent": updaterUIPercent})
		if _, err := page.Parse(updatedShell); err != nil {
			panic(err)
		}
	}
}
