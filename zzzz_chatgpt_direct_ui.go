package main

import "strings"

// ChatGPT Chat uses a dedicated persistent WebView2 profile owned by FlipAi.
// The normal ChatGPT desktop app remains untouched: no accessibility tree,
// global keyboard/mouse input, profile copying, or window focus is involved.
func init() {
	registerPage("agents", chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML)))
}

func chatGPTDirectUI(body string) string {
	const radios = `<input class="agent-switch" type="radio" name="agent-view" id="agent-codex" checked>
<input class="agent-switch" type="radio" name="agent-view" id="agent-claude">`
	body = replaceAgentUIOnce(body, radios, radios+`
<input class="agent-switch" type="radio" name="agent-view" id="agent-chatgpt">`, "ChatGPT radio")

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
          <b>ChatGPT Chat <span class="agent-chip warn" id="chatgpt-rail-status">Check status</span></b>
          <span>Private signed-in browser session</span>
        </span>
      </label>`
	body = replaceAgentUIOnce(body, claudeRail, claudeRail+chatRail, "ChatGPT rail item")

	const pane = `

    <!-- ------------------------- ChatGPT Chat ------------------------- -->
    <section class="agent-pane" id="chatgpt-pane">
      <div class="agent-head">
        <div class="agent-head-main">
          <span class="bmark lg codex">{{brand "codex"}}</span>
          <div>
            <h2>ChatGPT Chat <span class="pill warn" id="chatgpt-head-status">Checking</span></h2>
            <p>Chat with your normal ChatGPT account through a dedicated browser session that FlipAi keeps separate from the ChatGPT desktop app.</p>
          </div>
        </div>
        <div class="agent-head-actions chatgpt-actions">
          <form method="post" action="/chatgpt/connect"><button class="btn accent" type="submit">{{icon "bridge"}}Connect ChatGPT</button></form>
          <form method="post" action="/chatgpt/test"><button class="btn" type="submit">{{icon "check"}}Test ChatGPT</button></form>
          <form method="post" action="/chatgpt/disconnect" onsubmit="return confirm('Disconnect ChatGPT from FlipAi and remove its private sign-in profile?')"><button class="btn" type="submit">Disconnect</button></form>
        </div>
      </div>

      <p class="callout"><b>No Windows UI automation.</b> Connect opens one normal sign-in window the first time. After that, FlipAi reuses its own persistent WebView2 profile off-screen. It does not touch your normal ChatGPT app, move your mouse, type globally, or depend on accessibility settings.</p>

      <section class="card">
        <div class="card-head divided"><div><h2>Connection</h2><p>The status below comes from the dedicated ChatGPT browser itself.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">Sign-in<span>Verified from inside the FlipAi-owned ChatGPT page.</span></div><div class="value"><span class="pill warn" id="chatgpt-signin">Checking</span></div></div>
            <div class="row"><div class="label">Browser session<span>Uses a separate persistent WebView2 data folder for this connection.</span></div><div class="value"><span class="pill" id="chatgpt-browser">Stopped</span></div></div>
            <div class="row"><div class="label">Current conversation<span>Captured from the normal saved ChatGPT conversation URL after a turn.</span></div><div class="value"><span class="mono" id="chatgpt-conversation">None yet</span></div></div>
            <div class="row"><div class="label">Last browser event<span>Useful with Activity when a turn or sign-in fails.</span></div><div class="value"><span id="chatgpt-last-event">Not started</span></div></div>
            <div class="row" id="chatgpt-error-row" style="display:none"><div class="label">Last error<span>Also written to Activity with timing where available.</span></div><div class="value"><span class="pill bad" id="chatgpt-last-error"></span></div></div>
          </div>
          <p class="hint" style="margin-top:16px">First time: press <b>Connect ChatGPT</b>, sign in to your normal ChatGPT account in the window that opens, then press <b>Test ChatGPT</b>. Test sends a real harmless prompt and waits for the completed assistant response.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>Chat through FlipAi</h2><p>This is the same browser session the connection test uses.</p></div></div>
        <div class="card-body">
          <form method="post" action="/chatgpt/chat" class="chatgpt-chat-form">
            <label class="field"><span>Message</span><textarea name="prompt" rows="4" maxlength="12000" placeholder="Ask ChatGPT something..." required></textarea></label>
            <label class="check-row"><input type="checkbox" name="new" value="1"><span>Start a new ChatGPT chat</span></label>
            <div class="actions"><button class="btn accent" type="submit">Send to ChatGPT</button></div>
          </form>
          <p class="hint">Without “Start a new chat,” the WebView stays on the current ChatGPT conversation and the next message continues it. A new chat navigates to ChatGPT home first and then sends the message.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>Activity diagnostics</h2><p>ChatGPT connection events are added to the existing Activity tab instead of creating another log screen.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">Stages logged<span>Open, sign-in verified, worker start, test, turn, disconnect, and failures.</span></div><div class="value"><span class="pill">Enabled</span></div></div>
            <div class="row"><div class="label">Timing<span>End-to-end test and turn durations are recorded in milliseconds.</span></div><div class="value"><span class="pill">Enabled</span></div></div>
            <div class="row"><div class="label">Private content<span>Activity never stores your ChatGPT prompt, assistant reply, cookies, or tokens.</span></div><div class="value"><span class="pill ok">Not logged</span></div></div>
          </div>
          <div class="actions" style="margin-top:16px"><a class="btn" href="/activity">Open Activity</a><a class="btn" href="/chatgpt-direct/probe">Advanced protocol diagnostic</a></div>
        </div>
      </section>
    </section>

    <style>
    #agent-chatgpt:checked~.agents-shell .agent-item[for="agent-chatgpt"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-chatgpt:checked~.agents-shell .agent-item[for="agent-chatgpt"] b{color:var(--brand-ink)}
    #agent-chatgpt:checked~.agents-shell #chatgpt-pane{display:block}
    .chatgpt-actions{display:flex;gap:8px;flex-wrap:wrap}.chatgpt-actions form{margin:0}.chatgpt-chat-form textarea{width:100%;resize:vertical}.check-row{display:flex;align-items:center;gap:9px;margin:12px 0}.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:12px;max-width:360px;overflow-wrap:anywhere}
    </style>
    <script>
    (function(){
      function text(id,v){var e=document.getElementById(id);if(e)e.textContent=v}
      function pill(id,state,label){var e=document.getElementById(id);if(!e)return;e.textContent=label;e.className='pill'+(state==='ok'?' ok':state==='bad'?' bad':state==='warn'?' warn':'')}
      async function refreshChatGPT(){
        try{
          var r=await fetch('/chatgpt/status.json',{cache:'no-store'});if(!r.ok)return;
          var s=await r.json();
          var connected=!!s.signedIn;
          pill('chatgpt-head-status',connected?'ok':'warn',connected?'Connected':'Not connected');
          var rail=document.getElementById('chatgpt-rail-status');if(rail){rail.textContent=connected?'Connected':'Not connected';rail.className='agent-chip'+(connected?'':' warn')}
          pill('chatgpt-signin',connected?'ok':'warn',connected?'Signed in':'Not verified');
          pill('chatgpt-browser',s.running?'ok':'',s.running?(s.visible?'Sign-in window open':'Background ready'):'Stopped');
          text('chatgpt-conversation',s.conversationId||'None yet');
          text('chatgpt-last-event',s.lastEvent||'Not started');
          var er=document.getElementById('chatgpt-error-row');if(er)er.style.display=s.lastError?'flex':'none';text('chatgpt-last-error',s.lastError||'');
        }catch(e){}
      }
      refreshChatGPT();setInterval(refreshChatGPT,2000);
    })();
    </script>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around ChatGPT pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
