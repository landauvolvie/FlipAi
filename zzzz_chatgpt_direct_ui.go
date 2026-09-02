package main

import "strings"

// ChatGPT Chat is deliberately introduced as a discovery pane before it becomes
// an SMS destination. The user asked for the Codex-like backend path first, not
// accessibility/UI automation. This pane checks live local transports and the
// installed desktop application's static protocol definitions without moving
// focus, typing, or reading credentials.
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
          <span>Deep backend discovery</span>
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
            <p>Find the background protocol used by regular ChatGPT without controlling the visible app.</p>
          </div>
        </div>
        <div class="agent-head-actions">
          <button class="btn accent" type="button" data-test="/chatgpt-direct/probe" data-test-busy="Inspecting ChatGPT desktop">{{icon "search"}}Run deep backend diagnostic</button>
        </div>
      </div>

      <p class="callout"><b>This still does not enable ChatGPT.</b> FlipAi first checks live ChatGPT-owned transports, then safely inspects the installed ChatGPT program package for protocol/IPC clues. It never reads your signed-in profile or controls the visible window.</p>

      <section class="card">
        <div class="card-head divided"><div><h2>Direct connection status</h2><p>No accessibility, mouse/keyboard control, or hidden ChatGPT browser is used.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">ChatGPT connection<span>A diagnostic result is not the same as a connected agent.</span></div><div class="value"><span class="pill warn">Not connected</span></div></div>
            <div class="row"><div class="label">Visible UI automation<span>FlipAi must not steal focus or interfere with someone using the PC.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Browser/WebView automation<span>No built-in ChatGPT browser is used by this experiment.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Static package inspection<span>Reads installed app code such as app.asar/JavaScript for route and IPC names; user-data folders are skipped.</span></div><div class="value"><span class="pill">Diagnostic only</span></div></div>
            <div class="row"><div class="label">Credential capture<span>The diagnostic never reads ChatGPT cookies, tokens, Local Storage, process memory, or full process command lines.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">SMS routing<span>There is no Enable button yet because a usable ChatGPT backend has not been proven.</span></div><div class="value"><span class="pill warn">Unavailable in this build</span></div></div>
          </div>
          <p class="hint" style="margin-top:16px">Keep the regular ChatGPT desktop app open and signed in, then run the deep diagnostic. Codex pipes remain ignored. If ChatGPT exposes no live transport, FlipAi will report static endpoint/protocol markers from the installed application package instead.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>What we are looking for</h2><p>The goal is the same background request path regular ChatGPT itself uses.</p></div></div>
        <div class="card-body">
          <p class="hint" style="font-size:12.5px;margin:0">Useful clues include a ChatGPT-owned IPC name, custom protocol, conversation endpoint, websocket route, preload bridge, or other request identifier. Finding one still does not send a message; it tells the next build what exact background interface to test.</p>
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
