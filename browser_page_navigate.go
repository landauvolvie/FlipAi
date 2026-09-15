package main

import "strings"

// browserPageNavigateMarker marks an expression whose whole purpose is to move
// the page to another URL.
//
// Navigating destroys the execution context the expression is running in, so
// WebView2's completion handler for that very call frequently never fires.
// Waiting for it is waiting for nothing: the call spends its entire deadline
// and then keeps the browser's single protocol channel reserved until the
// outstanding cap expires.
//
// That is what stopped ChatGPT. It is the one provider that navigates -- to
// cross between its Chat and Work experiences, and to reach a known-good root
// -- and after each navigation every readiness probe queued behind a callback
// that was never coming. ChatGPT then never saw the message at all, while the
// same build's other agents were fine.
//
// Whether a navigation worked is never decided by its own call. Each caller
// waits for the page it lands on.
const browserPageNavigateMarker = "__FLIPAI_PAGE_NAVIGATE__"

// browserPageNavigateJS moves the page to url. The result is deliberately
// uninteresting: callers ignore it and wait for the page instead.
func browserPageNavigateJS(url string) string {
	return "/*" + browserPageNavigateMarker + "*/(()=>{location.href='" + url + "';return true})()"
}

func isBrowserPageNavigateExpression(expression string) bool {
	return strings.Contains(expression, browserPageNavigateMarker)
}
