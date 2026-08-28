package main

// shouldRestartAgentForAccessibility identifies the one desktop-app state that
// FlipAi can repair by restarting the app: Windows can read the native window
// frame, but Chromium/Electron is exposing none of its renderer content.
//
// This is intentionally much narrower than "voice control not found". A real
// app page that simply renamed its Voice control must stay open so its controls
// can be diagnosed; only the title-bar-only signature is safe to treat as the
// renderer accessibility switch being off.
func shouldRestartAgentForAccessibility(appTitle string, state agentVoiceState) bool {
	return state.Found &&
		!state.Active &&
		state.StartControl == "" &&
		onlyWindowChrome(appTitle, state.Controls)
}
