package main

import (
	"strings"
	"testing"
)

func TestBrowserAgentLiveConnectionUIUsesSignedInState(t *testing.T) {
	const stale = `var s=await r.json();var connected=!!s.connected||!!s.signedIn;var live=!!s.signedIn;`
	out := browserAgentLiveConnectionUI("before " + stale + " after")
	if strings.Contains(out, `connected=!!s.connected||!!s.signedIn`) {
		t.Fatal("saved connection flag still drives the visible Connected state")
	}
	if !strings.Contains(out, `var live=!!s.signedIn;var connected=live;`) {
		t.Fatal("visible browser-agent status is not driven by live sign-in")
	}
}
