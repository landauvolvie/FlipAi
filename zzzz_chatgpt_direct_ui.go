package main

import "strings"

// ChatGPT Chat uses a dedicated persistent WebView2 profile owned by FlipAi.
// The normal ChatGPT desktop app remains untouched.
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
          <b>ChatGPT Chat <span class="agent-chip warn" id="chatgpt-rail-status">Checking</span></b>
          <span>Answers {{.ChatGPTAccess.Prefix}}: messages</span>
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
            <p>Regular ChatGPT in FlipAi's private persistent browser session.</p>
          </div>
        </div>
        <div class="agent-head-actions chatgpt-actions">
          <form id="chatgpt-connect-form" method="post" action="/chatgpt/connect"><button class="btn accent" type="submit">{{icon "link"}}Connect</button></form>
          <form id="chatgpt-disconnect-form" method="post" action="/chatgpt/disconnect" style="display:none" onsubmit="return confirm('Disconnect ChatGPT from FlipAi and remove its private sign-in profile?')"><button class="btn" type="submit">{{icon "x-ring"}}Disconnect</button></form>
          <form id="chatgpt-test-form" method="post" action="/chatgpt/test" style="display:none"><button class="btn" type="submit">{{icon "play"}}Test</button></form>
          <button class="btn primary" type="submit" form="chatgpt-settings">Save ChatGPT</button>
        </div>
      </div>

      <form id="chatgpt-settings" method="post" action="/agents/save">
        <section class="card">
          <div class="card-head divided"><div><h2>Routing &amp; conversation</h2><p>The same routing idea as Codex and Claude.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="chatgptPrefix">SMS shortcut</label>
                <input id="chatgptPrefix" type="text" name="chatgptPrefix" value="{{.ChatGPTAccess.Prefix}}" maxlength="24" required>
                <p class="hint">Text <b>{{.ChatGPTAccess.Prefix}}: hello</b> once to select ChatGPT Chat. Unprefixed follow-ups stay here until you switch.</p>
              </div>
              <div class="field">
                <label for="chatgptNewSessionCommand">New conversation word</label>
                <input id="chatgptNewSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
                <p class="hint">Shared by all agents. Example: <b>{{.ChatGPTAccess.Prefix}}: {{.S.NewSessionCommand}}</b>.</p>
              </div>
            </div>
            <p class="callout"><b>No default agent.</b> C:, A:, or {{.ChatGPTAccess.Prefix}}: changes the active agent for that phone. It stays there across follow-up texts and restarts until you select another.</p>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>SMS instruction</h2><p>One shared instruction for Codex, Claude, and ChatGPT Chat. Edit it here and it changes everywhere.</p></div></div>
          <div class="card-body">{{template "promptEditor" .SharedPrompt}}</div>
        </section>

        {{template "agentAccess" .ChatGPTAccess}}
      </form>

      <details class="disclosure" style="margin-top:16px">
        <summary>Connection details</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row"><div class="label">Saved connection</div><div class="value"><span class="pill warn" id="chatgpt-saved">Checking</span></div></div>
            <div class="row"><div class="label">Live session</div><div class="value"><span class="pill warn" id="chatgpt-signin">Checking</span></div></div>
            <div class="row"><div class="label">Browser</div><div class="value"><span class="pill" id="chatgpt-browser">Stopped</span></div></div>
            <div class="row"><div class="label">Current conversation</div><div class="value"><span class="mono" id="chatgpt-conversation">None yet</span></div></div>
            <div class="row" id="chatgpt-error-row" style="display:none"><div class="label">Last error</div><div class="value"><span class="pill bad" id="chatgpt-last-error"></span></div></div>
          </div>
          <div class="actions" style="margin-top:14px"><a class="btn" href="/activity">Open Activity</a><a class="btn" href="/chatgpt-direct/probe">Protocol diagnostic</a></div>
        </div>
      </details>
    </section>

    <style>
    #agent-chatgpt:checked~.agents-shell .agent-item[for="agent-chatgpt"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-chatgpt:checked~.agents-shell .agent-item[for="agent-chatgpt"] b{color:var(--brand-ink)}
    #agent-chatgpt:checked~.agents-shell #chatgpt-pane{display:block}
    .chatgpt-actions{display:flex;gap:8px;flex-wrap:wrap}.chatgpt-actions form{margin:0}.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:12px;max-width:360px;overflow-wrap:anywhere}.input-static{min-height:42px;display:flex;align-items:center;padding:0 12px;border:1px solid var(--line);border-radius:9px;background:var(--soft)}
    </style>
    <script>
    (function(){
      function text(id,v){var e=document.getElementById(id);if(e)e.textContent=v}
      function pill(id,state,label){var e=document.getElementById(id);if(!e)return;e.textContent=label;e.className='pill'+(state==='ok'?' ok':state==='bad'?' bad':state==='warn'?' warn':'')}
      function show(id,on){var e=document.getElementById(id);if(e)e.style.display=on?'':'none'}
      async function refreshChatGPT(){
        try{
          var r=await fetch('/chatgpt/status.json',{cache:'no-store'});if(!r.ok)return;
          var s=await r.json();var connected=!!s.connected||!!s.signedIn;var live=!!s.signedIn;
          pill('chatgpt-head-status',connected?'ok':'warn',connected?'Connected':'Not connected');
          var rail=document.getElementById('chatgpt-rail-status');if(rail){rail.textContent=connected?'Connected':'Not connected';rail.className='agent-chip'+(connected?'':' warn')}
          show('chatgpt-connect-form',!connected);show('chatgpt-disconnect-form',connected);show('chatgpt-test-form',connected);
          pill('chatgpt-saved',connected?'ok':'warn',connected?'Connected':'Not connected');
          pill('chatgpt-signin',live?'ok':'warn',live?'Ready':connected?'Restoring':'Not verified');
          var browserLabel='Stopped',browserState='';if(s.loginActive)browserLabel='Sign-in open';else if(s.starting)browserLabel='Starting';else if(s.running)browserLabel=live?'Ready':'Loading';
          pill('chatgpt-browser',live?'ok':(s.running||s.starting)?'warn':'',browserLabel);
          text('chatgpt-conversation',s.conversationId||'None yet');
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
