package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAgentsFirstRunShowsConnectBeforeTest(t *testing.T) {
	a := newTestApp(t)
	body := a.do(t, http.MethodGet, "/agents", nil).Body.String()

	if strings.Contains(body, `<h2>Connection</h2>`) {
		t.Fatal("Claude still renders the duplicate lower-page Connection card")
	}
	if strings.Contains(body, "Long-lived token — fallback only") {
		t.Fatal("Claude still renders the lower fallback-token connection editor")
	}
	if !strings.Contains(body, `href="/codex/test"`) || !strings.Contains(body, `>Connect</a>`) {
		t.Fatal("fresh Codex pane does not offer Connect in its header")
	}
	if strings.Contains(body, `data-test="/codex/test"`) {
		t.Fatal("fresh Codex pane exposes Test before it has been connected")
	}
	if strings.Contains(body, `data-test="/claude/test"`) {
		t.Fatal("fresh Claude pane exposes Test before it has been connected")
	}
}

func TestAgentsConnectedStateShowsDisconnectAndTest(t *testing.T) {
	a := newTestApp(t)
	a.recordCheck("codex", true, "Codex connected")
	a.recordCheck("claude", true, "Claude connected")

	body := a.do(t, http.MethodGet, "/agents", nil).Body.String()
	for _, want := range []string{
		`data-test="/codex/test"`,
		`data-test="/claude/test"`,
		`name="agent" value="C"`,
		`name="agent" value="A"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("connected Agents page is missing %q", want)
		}
	}
}

func TestTopDisconnectReturnsOnlyThatAgentToConnectState(t *testing.T) {
	a := newTestApp(t)
	a.recordCheck("codex", true, "Codex connected")
	a.recordCheck("claude", true, "Claude connected")

	rr := a.do(t, http.MethodPost, "/claude/disconnect", url.Values{"agent": {"C"}})
	if rr.Code != http.StatusOK {
		t.Fatalf("Codex disconnect returned %d: %s", rr.Code, rr.Body.String())
	}
	st := loadState(a.statePath)
	if st.CodexCheck.OK {
		t.Fatal("Codex stayed connected after its Disconnect button")
	}
	if !st.ClaudeCheck.OK {
		t.Fatal("disconnecting Codex changed Claude's connection state")
	}
}

func TestDefaultClaudePathMayBootstrapButCustomMissingPathMayNot(t *testing.T) {
	for _, v := range []string{"", "claude", "CLAUDE.EXE"} {
		if !claudePathUsesManagedInstall(v) {
			t.Fatalf("%q should use FlipAi's managed Claude discovery/install flow", v)
		}
	}
	if claudePathUsesManagedInstall(`C:\Tools\Claude\claude.exe`) {
		t.Fatal("an explicit custom Claude path must never be replaced by an automatic install")
	}
}
