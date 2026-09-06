package main

import "strings"

// Copilot is applied after the existing exact-brand pass so all mature agent
// panes keep their current logos/layout while Copilot gets one independent pane.
func init() {
	registerPage("agents", copilotChatDirectUI(exactWebAgentsHTML()))
}

func copilotChatDirectUI(body string) string {
	body = strings.Replace(body,
		`<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, and M: selects Grok Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`,
		`<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, M: selects Gemini Chat, X: selects Grok Chat, and P: selects Microsoft Copilot Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, 1)

	const radioAnchor = `<input class="agent-switch" type="radio" name="agent-view" id="agent-grok-chat">`
	body = replaceAgentUIOnce(body, radioAnchor, radioAnchor+`
<input class="agent-switch" type="radio" name="agent-view" id="agent-copilot-chat">`, "Microsoft Copilot Chat radio")

	const grokRailTail = `<span>Answers {{.GrokChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	const copilotRail = `
      <label class="agent-item" for="agent-copilot-chat">
        <span class="bmark copilot-ms"><span class="copilot-ms-grid"><i></i><i></i><i></i><i></i></span></span>
        <span class="agent-item-copy">
          <b>Microsoft Copilot Chat <span class="agent-chip warn" id="copilot-chat-rail-status">Checking</span></b>
          <span>Answers {{.CopilotChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	body = replaceAgentUIOnce(body, grokRailTail, grokRailTail+copilotRail, "Microsoft Copilot Chat rail item")

	const pane = `

    <!-- -------------------- Microsoft Copilot Chat -------------------- -->
    <section class="agent-pane" id="copilot-chat-pane">
      <div class="agent-head">
        <div class="agent-head-main">
          <span class="bmark lg copilot-ms"><span class="copilot-ms-grid"><i></i><i></i><i></i><i></i></span></span>
          <div>
            <h2>Microsoft Copilot Chat <span class="pill warn" id="copilot-chat-head-status">Checking</span></h2>
            <p>Regular Microsoft Copilot at copilot.microsoft.com in FlipAi's private persistent browser session. No Copilot API key or desktop app is required.</p>
          </div>
        </div>
        <div class="agent-head-actions copilot-chat-actions">
          <form id="copilot-chat-connect-form" method="post" action="/copilot-chat/connect"><button class="btn accent" type="submit">{{icon "link"}}Connect</button></form>
          <form id="copilot-chat-disconnect-form" method="post" action="/copilot-chat/disconnect" style="display:none" onsubmit="return confirm('Disconnect Microsoft Copilot Chat from FlipAi and remove its private sign-in profile?')"><button class="btn" type="submit">{{icon "x-ring"}}Disconnect</button></form>
          <form id="copilot-chat-test-form" method="post" action="/copilot-chat/test" style="display:none"><button class="btn" type="submit">{{icon "play"}}Test</button></form>
          <button class="btn primary" type="submit" form="copilot-chat-settings">Save Copilot Chat</button>
        </div>
      </div>

      <form id="copilot-chat-settings" method="post" action="/agents/save">
        <section class="card">
          <div class="card-head divided"><div><h2>Routing &amp; conversation</h2><p>Copilot Chat follows the same sticky SMS routing rules as the other browser agents.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="copilotChatPrefix">SMS shortcut</label>
                <input id="copilotChatPrefix" type="text" name="copilotChatPrefix" value="{{.CopilotChatAccess.Prefix}}" maxlength="24" required>
                <p class="hint">Text <b>{{.CopilotChatAccess.Prefix}}: hello</b> once to select Microsoft Copilot Chat. Unprefixed follow-ups stay here until you switch.</p>
              </div>
              <div class="field">
                <label for="copilotChatNewSessionCommand">New conversation word</label>
                <input id="copilotChatNewSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
                <p class="hint">Shared by every agent. Example: <b>{{.CopilotChatAccess.Prefix}}: {{.S.NewSessionCommand}}</b>.</p>
              </div>
            </div>
            <p class="callout"><b>Background browser.</b> Connect opens a visible Copilot window only for sign-in/setup. After that FlipAi restores the same dedicated WebView2 profile off-screen and uses Copilot's real prompt box and Send control.</p>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>SMS instruction</h2><p>The same shared instruction used by every FlipAi agent.</p></div></div>
          <div class="card-body">{{template "promptEditor" .SharedPrompt}}</div>
        </section>

        {{template "agentAccess" .CopilotChatAccess}}
      </form>

      <details class="disclosure" style="margin-top:16px">
        <summary>Connection details</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row"><div class="label">Saved connection</div><div class="value"><span class="pill warn" id="copilot-chat-saved">Checking</span></div></div>
            <div class="row"><div class="label">Live sign-in</div><div class="value"><span class="pill warn" id="copilot-chat-signin">Checking</span></div></div>
            <div class="row"><div class="label">Browser session</div><div class="value"><span class="pill" id="copilot-chat-browser">Stopped</span></div></div>
            <div class="row"><div class="label">Current conversation</div><div class="value"><span class="mono" id="copilot-chat-conversation">None yet</span></div></div>
            <div class="row" id="copilot-chat-error-row" style="display:none"><div class="label">Last error</div><div class="value"><span class="pill bad" id="copilot-chat-last-error"></span></div></div>
          </div>
          <p class="hint" style="margin-top:14px">The visible window is setup-only. Normal turns run inside the one saved off-screen Copilot WebView2 owner, with no desktop mouse/keyboard automation or Windows accessibility.</p>
          <div class="actions" style="margin-top:14px"><a class="btn" href="/activity">Open Activity</a></div>
        </div>
      </details>
    </section>

    <style>
    #agent-copilot-chat:checked~.agents-shell .agent-item[for="agent-copilot-chat"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-copilot-chat:checked~.agents-shell .agent-item[for="agent-copilot-chat"] b{color:var(--brand-ink)}
    #agent-copilot-chat:checked~.agents-shell #copilot-chat-pane{display:block}
    .copilot-chat-actions{display:flex;gap:8px;flex-wrap:wrap}.copilot-chat-actions form{margin:0}
    .bmark.copilot-ms{background:#fff;display:grid;place-items:center}.copilot-ms-grid{width:58%;height:58%;display:grid;grid-template-columns:1fr 1fr;grid-template-rows:1fr 1fr;gap:7%;}.copilot-ms-grid i:nth-child(1){background:#f25022}.copilot-ms-grid i:nth-child(2){background:#7fba00}.copilot-ms-grid i:nth-child(3){background:#00a4ef}.copilot-ms-grid i:nth-child(4){background:#ffb900}.copilot-ms-grid i{display:block}
    </style>
    <script>
    (function(){
      function text(id,v){var e=document.getElementById(id);if(e)e.textContent=v}
      function pill(id,state,label){var e=document.getElementById(id);if(!e)return;e.textContent=label;e.className='pill'+(state==='ok'?' ok':state==='bad'?' bad':state==='warn'?' warn':'')}
      function show(id,on){var e=document.getElementById(id);if(e)e.style.display=on?'':'none'}
      async function refreshCopilot(){
        try{
          var r=await fetch('/copilot-chat/status.json',{cache:'no-store'});if(!r.ok)return;
          var s=await r.json();var connected=!!s.connected||!!s.signedIn;var live=!!s.signedIn;
          pill('copilot-chat-head-status',connected?'ok':'warn',connected?'Connected':'Not connected');
          var rail=document.getElementById('copilot-chat-rail-status');if(rail){rail.textContent=connected?'Connected':'Not connected';rail.className='agent-chip'+(connected?'':' warn')}
          show('copilot-chat-connect-form',!connected);show('copilot-chat-disconnect-form',connected);show('copilot-chat-test-form',connected);
          pill('copilot-chat-saved',connected?'ok':'warn',connected?'Connected':'Not connected');
          pill('copilot-chat-signin',live?'ok':'warn',live?'Ready':connected?'Restoring':'Not verified');
          var browserLabel='Stopped',browserState='';if(s.loginActive)browserLabel='Sign-in open';else if(s.starting)browserLabel='Starting';else if(s.running)browserLabel=live?'Ready':'Loading';
          pill('copilot-chat-browser',live?'ok':(s.running||s.starting)?'warn':'',browserLabel);
          text('copilot-chat-conversation',s.conversationId||'None yet');
          var er=document.getElementById('copilot-chat-error-row');if(er)er.style.display=s.lastError?'flex':'none';text('copilot-chat-last-error',s.lastError||'');
        }catch(e){}
      }
      refreshCopilot();setInterval(refreshCopilot,1500);
    })();
    </script>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around Microsoft Copilot Chat pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
