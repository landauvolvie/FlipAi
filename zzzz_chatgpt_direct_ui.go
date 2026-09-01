package main

import "strings"

// ChatGPT Chat is deliberately introduced as a discovery pane before it becomes
// an SMS destination. The user asked for the Codex-like backend path first, not
// accessibility/UI automation. This pane therefore has exactly one job: prove
// what background transport the signed-in ChatGPT desktop app exposes on the
// real machine without moving focus, typing, or reading credentials.
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
          <b>ChatGPT Chat <span class="agent-chip warn">Experimental</span></b>
          <span>Direct backend discovery</span>
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
            <h2>ChatGPT Chat <span class="pill brand">Direct backend experiment</span></h2>
            <p>Find a background path to regular ChatGPT Chat without accessibility, mouse/keyboard automation, or a hidden browser.</p>
          </div>
        </div>
        <div class="agent-head-actions">
          <button class="btn accent" type="button" data-test="/chatgpt-direct/probe" data-test-busy="Probing ChatGPT desktop">{{icon "link"}}Probe direct backend</button>
        </div>
      </div>

      <section class="card">
        <div class="card-head divided"><div><h2>Option 2: direct connection</h2><p>This is the Codex-style route we are testing first.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">Visible UI automation<span>FlipAi must not steal focus or interfere with someone using the PC.</span></div><div class="value"><span class="pill ok">Disabled</span></div></div>
            <div class="row"><div class="label">Browser/WebView automation<span>No built-in ChatGPT browser is used by this experiment.</span></div><div class="value"><span class="pill ok">Disabled</span></div></div>
            <div class="row"><div class="label">Credential capture<span>The probe never reads ChatGPT cookies, tokens, Local Storage, or full process command lines.</span></div><div class="value"><span class="pill ok">Disabled</span></div></div>
            <div class="row"><div class="label">SMS routing<span>We will enable ChatGPT as a real SMS agent only after the backend protocol is proven on your PC.</span></div><div class="value"><span class="pill warn">Not enabled yet</span></div></div>
          </div>
          <p class="callout" style="margin-top:16px"><b>Test it:</b> keep the regular ChatGPT desktop app open and signed in, then press <b>Probe direct backend</b>. FlipAi checks only ChatGPT-owned loopback listeners, relevant named pipes, and safe Chromium debugging metadata. The result is also saved in Activity so we can use it for the next build.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>What success means</h2><p>A candidate is not yet the finished Chat integration; it tells us which transport to implement next.</p></div></div>
        <div class="card-body">
          <p class="hint" style="font-size:12.5px;margin:0">If the probe finds a local listener, named pipe, or Chromium protocol channel owned by ChatGPT, the next step is to identify the message protocol and send a harmless test conversation through it. If it finds none, we continue inspecting desktop IPC — still without accessibility or focus stealing.</p>
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
