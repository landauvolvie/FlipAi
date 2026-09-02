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
          <span>Cloud auth + Chat protocol map</span>
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
            <p>Map the independent OAuth/PKCE path and regular Chat request protocol without borrowing the desktop app's signed-in session.</p>
          </div>
        </div>
        <div class="agent-head-actions">
          <button class="btn accent" type="button" data-test="/chatgpt-direct/probe" data-test-busy="Mapping ChatGPT cloud auth and request protocol">{{icon "search"}}Map Chat request protocol</button>
        </div>
      </div>

      <p class="callout"><b>This is the full read-only protocol map.</b> FlipAi still maps the Windows runtime and Electron app.asar, and now adds an <b>Independent cloud auth map</b>: public OAuth client configuration, redirect URI, scopes, PKCE mechanics, regular Chat conversation endpoint/state fields, header names, stream framing, and browser/device-challenge dependencies. Authenticated values and credentials are never returned.</p>

      <section class="card">
        <div class="card-head divided"><div><h2>Direct connection status</h2><p>No accessibility, mouse/keyboard control, hidden ChatGPT browser, or desktop-session credential copying is used.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">ChatGPT connection<span>A diagnostic result is not the same as a connected agent.</span></div><div class="value"><span class="pill warn">Not connected</span></div></div>
            <div class="row"><div class="label">Visible UI automation<span>FlipAi does not click, type, focus, or inspect the accessibility tree.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Browser/WebView automation<span>No built-in ChatGPT browser or hidden WebView is used by this experiment.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">Runtime architecture<span>Identifies Electron, WebView2, WinUI/native shell, child processes, modules and window classes.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Windows integration<span>Checks AppX extensions, AppServices and registered ChatGPT/OpenAI activation protocols without invoking them.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Electron app.asar<span>Maps regular app-code files, electronBridge/IPC directions, backend routes, request key names, auth-flow markers and external transport primitives.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Independent cloud auth map<span>Maps public OAuth client id, callback, scopes, PKCE mechanics, Chat endpoint/schema, stream format and session/challenge requirements.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Network metadata<span>Records only remote address/port and cached OpenAI/ChatGPT host names. It does not capture packet contents or HTTP headers.</span></div><div class="value"><span class="pill">Included</span></div></div>
            <div class="row"><div class="label">Credential capture<span>The diagnostic never reads ChatGPT cookies, tokens, Local Storage, IndexedDB, process memory, request bodies, credential values, or the desktop app's private session.</span></div><div class="value"><span class="pill">Not used</span></div></div>
            <div class="row"><div class="label">SMS routing<span>FlipAi will only expose Enable after an actual independently authenticated ChatGPT request path is proven.</span></div><div class="value"><span class="pill warn">Unavailable until proven</span></div></div>
          </div>
          <p class="hint" style="margin-top:16px">Keep the regular ChatGPT desktop app installed, then press <b>Map Chat request protocol</b>. Copy the full result. The new decisive lines begin with <b>CLOUD 01 PUBLIC CLIENT ID</b>, <b>CLOUD 02 REDIRECT URI</b>, <b>CLOUD 03 OAUTH ENDPOINT</b>, <b>CLOUD 04 OAUTH SCOPE</b>, <b>CLOUD 06 CONVERSATION ENDPOINT</b>, <b>CLOUD 07 HEADER NAME</b>, <b>CLOUD 08 REQUEST FIELD</b>, <b>CLOUD 10 STREAM FORMAT</b>, <b>CLOUD 11 SESSION DEPENDENCY</b>, and <b>CLOUD 12 PATH ASSESSMENT</b>. BACKEND ROUTE, REQUEST KEY, AUTH FLOW, IPC BINDING and the ASAR scan note remain useful too.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>What this result should decide</h2><p>This pass is designed to choose the implementation path, not produce another ambiguous green/red clue.</p></div></div>
        <div class="card-body">
          <p class="hint" style="font-size:12.5px;margin:0">If the package contains a public OAuth client id + redirect + PKCE flow and regular Chat protocol without mandatory browser-session credentials, the next build can test an explicit user-authorized independent login. If the Chat request is inseparable from cookies/device challenges held by a browser session, FlipAi will say so and the dedicated signed-in WebView becomes the practical fallback instead of harvesting the desktop app's profile.</p>
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
