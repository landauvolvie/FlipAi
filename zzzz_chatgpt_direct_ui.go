package main

import "strings"

// ChatGPT Chat is deliberately introduced as a discovery pane before it becomes
// an SMS destination. The user asked for the Codex-like backend path first, not
// accessibility/UI automation. This pane checks live local transports, runtime
// architecture, package registration and installed protocol definitions without
// moving focus, typing, or reading credentials.
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
          <span>Full backend architecture test</span>
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
            <p>Determine exactly how regular ChatGPT runs and whether it exposes any safe background path FlipAi can use.</p>
          </div>
        </div>
        <div class="agent-head-actions">
          <button class="btn accent" type="button" data-test="/chatgpt-direct/probe" data-test-busy="Inspecting ChatGPT architecture">{{icon "search"}}Run app bundle deep scan</button>
        </div>
      </div>

      <p class="callout"><b>This is the broadest safe direct-backend test.</b> FlipAi checks every non-invasive path we can use without stealing focus: owned localhost listeners, Chromium debugging channels, process/child-process runtime type, loaded WebView2/Electron/WinUI modules, AppX manifest extensions and AppServices, activation protocols, window classes, active connection metadata, OpenAI/ChatGPT DNS names, package layout, and which exact installed files contain Chat/conversation/IPC markers.</p>

      <section class="card">
        <div class="card-head divided"><div><h2>Direct connection status</h2><p>No accessibility, mouse/keyboard control, or hidden ChatGPT browser is used.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">ChatGPT connection<span>A diagnostic result is not the same as a connected agent.</span></div><div class="value"><span class="pill warn">Not connected</span></div></div>
            <div class="row"><div class="label">Visible UI automation<span>FlipAi does not click, type, focus, or inspect the accessibility tree.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Browser/WebView automation<span>No built-in ChatGPT browser or hidden WebView is used by this experiment.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Runtime architecture<span>Identifies Electron, WebView2, WinUI/native shell, child processes, modules and window classes.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Windows integration<span>Checks AppX extensions, AppServices and registered ChatGPT/OpenAI activation protocols without invoking them.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Network metadata<span>Records only remote address/port and cached OpenAI/ChatGPT host names. It does not capture packet contents or HTTP headers.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Protocol source attribution<span>Separates app-specific Chat/backend markers from generic Playwright, browser, Codex and runtime files.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Credential capture<span>The diagnostic never reads ChatGPT cookies, tokens, Local Storage, IndexedDB, process memory, request bodies, or full process command lines.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">SMS routing<span>FlipAi will only expose Enable after an actual usable ChatGPT request path is proven.</span></div><div class="value"><span class="pill warn">Unavailable until proven</span></div></div>
          </div>
          <p class="hint" style="margin-top:16px">Keep the regular ChatGPT desktop app open and signed in, then press <b>Run full architecture diagnostic</b>. It can take up to about a minute because it checks the Windows package and attributes protocol markers to their source files. Codex pipes remain excluded from ChatGPT connectivity.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>What the result should decide</h2><p>The diagnostic ends with a Direct-backend assessment rather than another ambiguous green/red clue.</p></div></div>
        <div class="card-body">
          <p class="hint" style="font-size:12.5px;margin:0">It will tell us whether ChatGPT exposes a local listener, Windows AppService, activation-only protocol, WebView2/Electron IPC possibility, or only cloud-backed Chat endpoints. It also reports which protocol strings are truly app-specific versus bundled tooling. That gives the next implementation a concrete path—or tells us that option 2 cannot be shipped safely without an official authenticated interface.</p>
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
