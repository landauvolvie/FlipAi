package main

import "strings"

// museChatDirectUI adds Muse to the mature browser-agent workbench. The final
// SMS-routing presentation pass wraps this function so public shortcuts remain
// fixed (MU:) while the stored internal prefix stays migration-compatible.
func museChatDirectUI(body string) string {
	const radioAnchor = `<input class="agent-switch" type="radio" name="agent-view" id="agent-copilot-chat">`
	body = replaceAgentUIOnce(body, radioAnchor, radioAnchor+`
<input class="agent-switch" type="radio" name="agent-view" id="agent-muse-chat">`, "Muse radio")

	const copilotRailTail = `<span>Answers {{.CopilotChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	const museRail = `
      <label class="agent-item" for="agent-muse-chat">
        <span class="bmark muse-mark"><span>MU</span></span>
        <span class="agent-item-copy">
          <b>Muse <span class="agent-chip warn" id="muse-chat-rail-status">Checking</span></b>
          <span>Answers {{.MuseChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	body = replaceAgentUIOnce(body, copilotRailTail, copilotRailTail+museRail, "Muse rail item")

	const pane = `

    <!-- -------------------- Muse -------------------- -->
    <section class="agent-pane" id="muse-chat-pane">
      <div class="agent-head">
        <div class="agent-head-main">
          <span class="bmark lg muse-mark"><span>MU</span></span>
          <div>
            <h2>Muse <span class="pill warn" id="muse-chat-head-status">Checking</span></h2>
            <p>Muse at muse.ai in FlipAi's own persistent browser session. Sign in once; normal SMS turns use the saved session in the background without an API key.</p>
          </div>
        </div>
        <div class="agent-head-actions muse-chat-actions">
          <form id="muse-chat-connect-form" method="post" action="/muse-chat/connect"><button class="btn accent" type="submit">{{icon "link"}}Connect</button></form>
          <form id="muse-chat-disconnect-form" method="post" action="/muse-chat/disconnect" style="display:none" onsubmit="return confirm('Disconnect Muse from FlipAi and remove its private sign-in profile?')"><button class="btn" type="submit">{{icon "x-ring"}}Disconnect</button></form>
          <form id="muse-chat-test-form" method="post" action="/muse-chat/test" style="display:none"><button class="btn" type="submit">{{icon "play"}}Test</button></form>
          <button class="btn primary" type="submit" form="muse-chat-settings">Save Muse</button>
        </div>
      </div>

      <form id="muse-chat-settings" method="post" action="/agents/save">
        <section class="card">
          <div class="card-head divided"><div><h2>Routing &amp; conversation</h2><p>Muse uses the same sticky SMS routing and NEW-conversation behavior as the other browser agents.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="museChatPrefix">SMS shortcut</label>
                <input id="museChatPrefix" type="text" name="museChatPrefix" value="{{.MuseChatAccess.Prefix}}" maxlength="24" required>
                <p class="hint">Text <b>MU: hello</b> once to select Muse. Unprefixed follow-ups stay with Muse until you switch.</p>
              </div>
              <div class="field">
                <label for="museChatNewSessionCommand">New conversation word</label>
                <input id="museChatNewSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
                <p class="hint">Example: <b>MU {{.S.NewSessionCommand}}: start fresh</b>.</p>
              </div>
            </div>
            <p class="callout"><b>Dedicated browser profile.</b> Muse cookies and sign-in data live only in FlipAi's Muse profile. Connecting or disconnecting Muse does not touch ChatGPT, Gemini, Copilot, Edge, or your normal browser profiles.</p>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>SMS instruction</h2><p>Muse receives the same shared text-message instruction as the other FlipAi agents.</p></div></div>
          <div class="card-body">{{template "promptEditor" .SharedPrompt}}</div>
        </section>

        {{template "agentAccess" .MuseChatAccess}}
      </form>

      <details class="disclosure" style="margin-top:16px">
        <summary>Connection details</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row"><div class="label">Saved connection</div><div class="value"><span class="pill warn" id="muse-chat-saved">Checking</span></div></div>
            <div class="row"><div class="label">Live sign-in</div><div class="value"><span class="pill warn" id="muse-chat-signin">Checking</span></div></div>
            <div class="row"><div class="label">Browser session</div><div class="value"><span class="pill" id="muse-chat-browser">Stopped</span></div></div>
            <div class="row"><div class="label">Current conversation</div><div class="value"><span class="mono" id="muse-chat-conversation">None yet</span></div></div>
            <div class="row" id="muse-chat-error-row" style="display:none"><div class="label">Last error</div><div class="value"><span class="pill bad" id="muse-chat-last-error"></span></div></div>
          </div>
          <p class="hint" style="margin-top:14px">If Muse redirects to its authentication site, FlipAi reports it as not signed in rather than falsely showing Connected. Complete sign-in in the visible setup window, then normal turns stay off-screen.</p>
          <div class="actions" style="margin-top:14px"><a class="btn" href="/activity">Open Activity</a></div>
        </div>
      </details>
    </section>

    <style>
    #agent-muse-chat:checked~.agents-shell .agent-item[for="agent-muse-chat"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-muse-chat:checked~.agents-shell .agent-item[for="agent-muse-chat"] b{color:var(--brand-ink)}
    #agent-muse-chat:checked~.agents-shell #muse-chat-pane{display:block}
    .muse-chat-actions{display:flex;gap:8px;flex-wrap:wrap}.muse-chat-actions form{margin:0}
    .bmark.muse-mark{background:linear-gradient(145deg,#fff,#eee9ff);color:#5636a8;display:grid;place-items:center;font-weight:850;letter-spacing:-.04em}.bmark.muse-mark span{font-size:.72em}
    </style>
    <script>
    (function(){
      function text(id,v){var e=document.getElementById(id);if(e)e.textContent=v}
      function pill(id,state,label){var e=document.getElementById(id);if(!e)return;e.textContent=label;e.className='pill'+(state==='ok'?' ok':state==='bad'?' bad':state==='warn'?' warn':'')}
      function show(id,on){var e=document.getElementById(id);if(e)e.style.display=on?'':'none'}
      async function refreshMuse(){
        try{
          var r=await fetch('/muse-chat/status.json',{cache:'no-store'});if(!r.ok)return;
          var s=await r.json();var connected=!!s.connected||!!s.signedIn;var live=!!s.signedIn;
          pill('muse-chat-head-status',connected?'ok':'warn',connected?'Connected':'Not connected');
          var rail=document.getElementById('muse-chat-rail-status');if(rail){rail.textContent=connected?'Connected':'Not connected';rail.className='agent-chip'+(connected?'':' warn')}
          show('muse-chat-connect-form',!connected);show('muse-chat-disconnect-form',connected);show('muse-chat-test-form',connected);
          pill('muse-chat-saved',connected?'ok':'warn',connected?'Connected':'Not connected');
          pill('muse-chat-signin',live?'ok':'warn',live?'Ready':connected?'Restoring':'Not verified');
          var browserLabel='Stopped',browserState='';if(s.loginActive)browserLabel='Sign-in open';else if(s.starting)browserLabel='Starting';else if(s.running)browserLabel=live?'Ready':'Loading';
          pill('muse-chat-browser',live?'ok':(s.running||s.starting)?'warn':'',browserLabel);
          text('muse-chat-conversation',s.conversationId||'None yet');
          var er=document.getElementById('muse-chat-error-row');if(er)er.style.display=s.lastError?'flex':'none';text('muse-chat-last-error',s.lastError||'');
        }catch(e){}
      }
      refreshMuse();setInterval(refreshMuse,1500);
    })();
    </script>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around Muse pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
