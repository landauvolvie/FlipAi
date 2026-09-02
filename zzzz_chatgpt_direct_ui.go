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
          <span>Always-on private browser session</span>
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
            <p>Connect once. FlipAi keeps the same private ChatGPT browser profile running invisibly and restores it automatically after app or Windows restarts.</p>
          </div>
        </div>
        <div class="agent-head-actions chatgpt-actions">
          <form method="post" action="/chatgpt/connect"><button class="btn accent" type="submit">{{icon "bridge"}}Connect ChatGPT</button></form>
          <form method="post" action="/chatgpt/test"><button class="btn" type="submit">{{icon "check"}}Test ChatGPT</button></form>
          <form method="post" action="/chatgpt/disconnect" onsubmit="return confirm('Disconnect ChatGPT from FlipAi and remove its private sign-in profile?')"><button class="btn" type="submit">Disconnect</button></form>
        </div>
      </div>

      <p class="callout"><b>Connect only once.</b> The sign-in window is only for the first login. After ChatGPT verifies the account, you may close that window. FlipAi automatically restores the same persistent WebView2 profile off-screen after the window closes, after FlipAi restarts, and after Windows restarts. It never touches your normal ChatGPT app, moves your mouse, types globally, or depends on accessibility settings.</p>

      <section class="card">
        <div class="card-head divided"><div><h2>Connection</h2><p>Saved connection and live browser readiness are tracked separately so a normal startup delay is never mistaken for a lost login.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">Saved connection<span>Stays connected across FlipAi and Windows restarts until you press Disconnect or ChatGPT expires the account session.</span></div><div class="value"><span class="pill warn" id="chatgpt-saved">Checking</span></div></div>
            <div class="row"><div class="label">Live sign-in<span>Verified from inside the currently running FlipAi-owned ChatGPT page.</span></div><div class="value"><span class="pill warn" id="chatgpt-signin">Checking</span></div></div>
            <div class="row"><div class="label">Browser session<span>Runs off-screen automatically after the one-time sign-in window is closed.</span></div><div class="value"><span class="pill" id="chatgpt-browser">Stopped</span></div></div>
            <div class="row"><div class="label">Current conversation<span>Captured from the normal saved ChatGPT conversation URL after a turn.</span></div><div class="value"><span class="mono" id="chatgpt-conversation">None yet</span></div></div>
            <div class="row"><div class="label">Last browser event<span>Useful with Activity when a turn, restore, or sign-in fails.</span></div><div class="value"><span id="chatgpt-last-event">Not started</span></div></div>
            <div class="row" id="chatgpt-error-row" style="display:none"><div class="label">Last error<span>Also written to Activity with timing where available.</span></div><div class="value"><span class="pill bad" id="chatgpt-last-error"></span></div></div>
          </div>
          <p class="hint" style="margin-top:16px">First time only: press <b>Connect ChatGPT</b> and sign in to your normal ChatGPT account. Once <b>Saved connection</b> says Connected, close the sign-in window if you want. From then on, <b>Test ChatGPT</b> and normal turns wait for the hidden session to finish restoring before they send.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>Chat through FlipAi</h2><p>This uses the always-on background browser session.</p></div></div>
        <div class="card-body">
          <form method="post" action="/chatgpt/chat" class="chatgpt-chat-form">
            <label class="field"><span>Message</span><textarea name="prompt" rows="4" maxlength="12000" placeholder="Ask ChatGPT something..." required></textarea></label>
            <label class="check-row"><input type="checkbox" name="new" value="1"><span>Start a new ChatGPT chat</span></label>
            <div class="actions"><button class="btn accent" type="submit">Send to ChatGPT</button></div>
          </form>
          <p class="hint">Without “Start a new chat,” the WebView stays on the current ChatGPT conversation and the next message continues it. A new chat navigates to ChatGPT home first and waits for the saved session to be ready before sending.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>Activity diagnostics</h2><p>ChatGPT connection events are added to the existing Activity tab instead of creating another log screen.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">Stages logged<span>One-time sign-in, saved-session restore, background start, readiness wait, test, turn, disconnect, and failures.</span></div><div class="value"><span class="pill">Enabled</span></div></div>
            <div class="row"><div class="label">Timing<span>Session readiness plus end-to-end test and turn durations are recorded.</span></div><div class="value"><span class="pill">Enabled</span></div></div>
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
          var connected=!!s.connected||!!s.signedIn;
          var live=!!s.signedIn;
          pill('chatgpt-head-status',connected?'ok':'warn',connected?'Connected':'Not connected');
          var rail=document.getElementById('chatgpt-rail-status');if(rail){rail.textContent=connected?'Connected':'Not connected';rail.className='agent-chip'+(connected?'':' warn')}
          pill('chatgpt-saved',connected?'ok':'warn',connected?'Connected':'Not connected');
          pill('chatgpt-signin',live?'ok':connected?'warn':'warn',live?'Ready':connected?'Restoring':'Not verified');
          var browserLabel='Stopped',browserState='';
          if(s.loginActive)browserLabel='Sign-in window open';
          else if(s.starting)browserLabel='Starting in background';
          else if(s.running)browserLabel=s.visible?'Sign-in window open':(live?'Background ready':'Background loading');
          pill('chatgpt-browser',live?'ok':(s.running||s.starting)?'warn':'',browserLabel);
          text('chatgpt-conversation',s.conversationId||'None yet');
          text('chatgpt-last-event',s.lastEvent||'Not started');
          var er=document.getElementById('chatgpt-error-row');if(er)er.style.display=s.lastError?'flex':'none';text('chatgpt-last-error',s.lastError||'');
        }catch(e){}
      }
      refreshChatGPT();setInterval(refreshChatGPT,1500);
    })();
    </script>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around ChatGPT pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
