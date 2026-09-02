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
          <span>Chat request protocol map</span>
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
            <p>Map electronBridge, IPC handlers, Chat backend routes, request schema and authentication-flow markers without controlling the visible app.</p>
          </div>
        </div>
        <div class="agent-head-actions">
          <button class="btn accent" type="button" data-test="/chatgpt-direct/probe" data-test-busy="Mapping ChatGPT request protocol">{{icon "search"}}Map Chat request protocol</button>
        </div>
      </div>

      <p class="callout"><b>This is the full read-only protocol map.</b> FlipAi checks the Windows runtime and then maps the real Electron app.asar: contextBridge exposure, bridge properties, ipcRenderer/ipcMain directions, regular Chat backend routes, conversation request field names, OAuth/PKCE markers, streaming transports, and any server/pipe primitives. Values and credentials are never returned.</p>

      <section class="card">
        <div class="card-head divided"><div><h2>Direct connection status</h2><p>No accessibility, mouse/keyboard control, or hidden ChatGPT browser is used.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">ChatGPT connection<span>A diagnostic result is not the same as a connected agent.</span></div><div class="value"><span class="pill warn">Not connected</span></div></div>
            <div class="row"><div class="label">Visible UI automation<span>FlipAi does not click, type, focus, or inspect the accessibility tree.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Browser/WebView automation<span>No built-in ChatGPT browser or hidden WebView is used by this experiment.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Runtime architecture<span>Identifies Electron, WebView2, WinUI/native shell, child processes, modules and window classes.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Windows integration<span>Checks AppX extensions, AppServices and registered ChatGPT/OpenAI activation protocols without invoking them.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Electron app.asar<span>Maps regular app-code files, electronBridge/IPC directions, backend routes, request key names, auth-flow markers and external transport primitives.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Network metadata<span>Records only remote address/port and cached OpenAI/ChatGPT host names. It does not capture packet contents or HTTP headers.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Credential capture<span>The diagnostic never reads ChatGPT cookies, tokens, Local Storage, IndexedDB, process memory, request bodies, or full process command lines.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">SMS routing<span>FlipAi will only expose Enable after an actual usable ChatGPT request path is proven.</span></div><div class="value"><span class="pill warn">Unavailable until proven</span></div></div>
          </div>
          <p class="hint" style="margin-top:16px">Keep the regular ChatGPT desktop app open and signed in, then press <b>Map Chat request protocol</b>. Copy the full result, especially BACKEND ROUTE, REQUEST KEY, AUTH FLOW, BRIDGE EXPOSURE, BRIDGE METHOD, IPC BINDING, EXTERNAL TRANSPORT SIGNAL, the ASAR scan note, and Direct-backend assessment.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>What the result should decide</h2><p>The diagnostic ends with a Direct-backend assessment rather than another ambiguous green/red clue.</p></div></div>
        <div class="card-body">
          <p class="hint" style="font-size:12.5px;margin:0">This pass distinguishes internal Electron IPC from anything externally callable and maps the cloud request/auth shape at the same time. If no external transport exists, we should know whether Option 2 can only work through an independent authenticated ChatGPT cloud session rather than a local desktop bridge.</p>
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
