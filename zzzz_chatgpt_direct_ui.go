package main

import "strings"

// ChatGPT Chat is deliberately introduced as a discovery pane before it becomes
// an SMS destination. The user asked for the Codex-like backend path first, not
// accessibility/UI automation. This pane therefore has exactly one job: check
// whether the signed-in ChatGPT desktop app exposes a background transport on
// the real machine without moving focus, typing, or reading credentials.
func init() {
	registerPage("agents", chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML)))
}

func chatGPTDirectUI(body string) string {
	const radios = `<input class="agent-switch" type="radio" name="agent-view" id="agent-codex" checked>
<input class="agent-switch" type="radio" name="agent-view" id="agent-claude">`
	body = replaceAgentUIOnce(body, radios, radios+`
<input class="agent-switch" type="radio" name="agent-view" id="agent-chatgpt">`, "ChatGPT direct radio")

	const claudeRail = `      <label class="agent-item" for="agent-claude">
        <span class="bmark claude">{{brand "claude"}}</span>
        <span class="agent-item-copy">
          <b>Claude <span class="agent-chip{{if not .S.ClaudeCheck.Ready}} warn{{end}}">{{if not .S.ClaudeCheck.Known}}Untested{{else if .S.ClaudeCheck.OK}}Ready{{else}}Attention{{end}}</span></b>
          <span>Answers {{.S.ClaudePrefix}}: messages</span>
        </span>
      </label>`
	const chatRail = `
      <label class="agent-item" for="agent-chatgpt">
        <span class="bmark codex">{{brand "codex"}}</span>
        <span class="agent-item-copy">
          <b>ChatGPT Chat <span class="agent-chip warn">Not connected</span></b>
          <span>Direct backend diagnostic</span>
        </span>
      </label>`
	body = replaceAgentUIOnce(body, claudeRail, claudeRail+chatRail, "ChatGPT direct rail item")

	const pane = `

    <!-- ------------------------- ChatGPT Chat ------------------------- -->
    <section class="agent-pane" id="chatgpt-pane">
      <div class="agent-head">
        <div class="agent-head-main">
          <span class="bmark lg codex">{{brand "codex"}}</span>
          <div>
            <h2>ChatGPT Chat <span class="pill warn">Not connected</span></h2>
            <p>Check whether regular ChatGPT exposes a direct background connection that FlipAi can safely use.</p>
          </div>
        </div>
        <div class="agent-head-actions">
          <button class="btn accent" type="button" data-test="/chatgpt-direct/probe" data-test-busy="Checking ChatGPT desktop">{{icon "search"}}Run backend diagnostic</button>
        </div>
      </div>

      <p class="callout"><b>This button does not enable ChatGPT.</b> It only checks whether the desktop app exposes a usable direct backend. ChatGPT SMS routing stays off until FlipAi can prove and test that backend.</p>

      <section class="card">
        <div class="card-head divided"><div><h2>Direct connection status</h2><p>No visible UI control and no hidden ChatGPT browser are used.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">ChatGPT connection<span>A successful diagnostic is not the same as a connected agent.</span></div><div class="value"><span class="pill warn">Not connected</span></div></div>
            <div class="row"><div class="label">Visible UI automation<span>FlipAi must not steal focus or interfere with someone using the PC.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Browser/WebView automation<span>No built-in ChatGPT browser is used by this experiment.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Credential capture<span>The diagnostic never reads ChatGPT cookies, tokens, Local Storage, or full process command lines.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">SMS routing<span>There is no Enable button yet because a usable ChatGPT backend has not been proven.</span></div><div class="value"><span class="pill warn">Unavailable in this build</span></div></div>
          </div>
          <p class="hint" style="margin-top:16px">Keep the regular ChatGPT desktop app open and signed in, then run the diagnostic. If it finds only Codex pipes, those are ignored and do not count as ChatGPT connectivity.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>What the diagnostic means</h2><p>It checks for a direct transport; it never turns ChatGPT on by itself.</p></div></div>
        <div class="card-body">
          <p class="hint" style="font-size:12.5px;margin:0">If FlipAi proves a ChatGPT-owned listener or protocol channel, the next build can test a harmless background conversation through it. A globally visible pipe name by itself is not enough, because Codex and other apps can create pipes on the same PC.</p>
        </div>
      </section>
    </section>

    <style>
    #agent-chatgpt:checked~.agents-shell .agent-item[for="agent-chatgpt"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-chatgpt:checked~.agents-shell .agent-item[for="agent-chatgpt"] b{color:var(--brand-ink)}
    #agent-chatgpt:checked~.agents-shell #chatgpt-pane{display:block}
    </style>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around ChatGPT direct pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
