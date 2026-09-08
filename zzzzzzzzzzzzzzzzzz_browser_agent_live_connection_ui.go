package main

import "strings"

// The browser-agent runtimes intentionally keep Connected as a saved-profile
// preference so the tray knows which private sessions it should restore after a
// restart. That is not the same thing as a live authenticated session. The
// Agents page used to merge those two concepts, which made an expired or failed
// login still show as Connected and hid the Connect button.
//
// Apply this after the existing final presentation passes so every private
// WebView agent reports the visible connection state from SignedIn only. The
// saved-profile flag remains untouched for background restore logic.
func init() {
	registerPage("agents", browserAgentLiveConnectionUI(smsRouteAgentsUI(museChatDirectUI(copilotChatDirectUI(exactWebAgentsHTML())))))
}

func browserAgentLiveConnectionUI(body string) string {
	const stale = `var s=await r.json();var connected=!!s.connected||!!s.signedIn;var live=!!s.signedIn;`
	const live = `var s=await r.json();var live=!!s.signedIn;var connected=live;`
	return strings.ReplaceAll(body, stale, live)
}
