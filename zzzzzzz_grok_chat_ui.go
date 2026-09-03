package main

import "strings"

// This runs after the existing ChatGPT augmentation and re-registers the Agents
// page with a fifth, independent Grok Chat pane. Keeping the browser agents
// as augmentations avoids disturbing the mature Codex/Claude CLI layout.
func init() {
	registerPage("agents", grokChatDirectUI(geminiChatDirectUI(claudeChatDirectUI(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))))))
}

func grokChatDirectUI(body string) string {
	body = strings.Replace(body, `<p>C: selects Codex, A: selects Claude, and G: selects ChatGPT Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, `<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, and M: selects Grok Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, 1)
	const chatRadio = `<input class="agent-switch" type="radio" name="agent-view" id="agent-chatgpt">`
	body = replaceAgentUIOnce(body, chatRadio, chatRadio+`
<input class="agent-switch" type="radio" name="agent-view" id="agent-grok-chat">`, "Grok Chat radio")

	const geminiRailAnchor = `      <label class="agent-item" for="agent-claude-chat">
        <span class="bmark claude">{{brand "claude"}}</span>
        <span class="agent-item-copy">
          <b>Claude Chat <span class="agent-chip warn" id="claude-chat-rail-status">Checking</span></b>
          <span>Answers {{.ClaudeChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	const grokChatRail = `
      <label class="agent-item" for="agent-grok-chat">
        <span class="bmark grok">𝕏</span>
        <span class="agent-item-copy">
          <b>Grok Chat <span class="agent-chip warn" id="grok-chat-rail-status">Checking</span></b>
          <span>Answers {{.GrokChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	body = replaceAgentUIOnce(body, geminiRailAnchor, geminiRailAnchor+grokChatRail, "Grok Chat rail item")

	const pane = `

    <!-- ------------------------- Grok Chat ------------------------- -->
    <section class="agent-pane" id="grok-chat-pane">
      <div class="agent-head">
        <div class="agent-head-main">
          <span class="bmark lg grok">𝕏</span>
          <div>
            <h2>Grok Chat <span class="pill warn" id="grok-chat-head-status">Checking</span></h2>
            <p>Regular Grok at grok.com in FlipAi's private persistent browser session. No Grok API or Grok CLI is required.</p>
          </div>
        </div>
        <div class="agent-head-actions grok-chat-actions">
          <form id="grok-chat-connect-form" method="post" action="/grok-chat/connect"><button class="btn accent" type="submit">{{icon "link"}}Connect</button></form>
          <form id="grok-chat-disconnect-form" method="post" action="/grok-chat/disconnect" style="display:none" onsubmit="return confirm('Disconnect Grok Chat from FlipAi and remove its private sign-in profile?')"><button class="btn" type="submit">{{icon "x-ring"}}Disconnect</button></form>
          <form id="grok-chat-test-form" method="post" action="/grok-chat/test" style="display:none"><button class="btn" type="submit">{{icon "play"}}Test</button></form>
          <button class="btn primary" type="submit" form="grok-chat-settings">Save Grok Chat</button>
        </div>
      </div>

      <form id="grok-chat-settings" method="post" action="/agents/save">
        <section class="card">
          <div class="card-head divided"><div><h2>Routing &amp; conversation</h2><p>Grok Chat follows the same sticky SMS routing rules as the other agents.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="grokChatPrefix">SMS shortcut</label>
                <input id="grokChatPrefix" type="text" name="grokChatPrefix" value="{{.GrokChatAccess.Prefix}}" maxlength="24" required>
                <p class="hint">Text <b>{{.GrokChatAccess.Prefix}}: hello</b> once to select Grok Chat. Unprefixed follow-ups stay here until you switch.</p>
              </div>
              <div class="field">
                <label for="grokChatNewSessionCommand">New conversation word</label>
                <input id="grokChatNewSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
                <p class="hint">Shared by every agent. Example: <b>{{.GrokChatAccess.Prefix}}: {{.S.NewSessionCommand}}</b>.</p>
              </div>
            </div>
            <p class="callout"><b>No default agent.</b> C:, A:, G:, H:, M:, or {{.GrokChatAccess.Prefix}}: changes the active agent for that phone. It stays selected across follow-up texts and PC/app restarts.</p>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>SMS instruction</h2><p>The same shared instruction used by Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat.</p></div></div>
          <div class="card-body">{{template "promptEditor" .SharedPrompt}}</div>
        </section>

        {{template "agentAccess" .GrokChatAccess}}
      </form>

      <details class="disclosure" style="margin-top:16px">
        <summary>Connection details</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row"><div class="label">Saved connection</div><div class="value"><span class="pill warn" id="grok-chat-saved">Checking</span></div></div>
            <div class="row"><div class="label">Live sign-in</div><div class="value"><span class="pill warn" id="grok-chat-signin">Checking</span></div></div>
            <div class="row"><div class="label">Browser session</div><div class="value"><span class="pill" id="grok-chat-browser">Stopped</span></div></div>
            <div class="row"><div class="label">Current conversation</div><div class="value"><span class="mono" id="grok-chat-conversation">None yet</span></div></div>
            <div class="row" id="grok-chat-error-row" style="display:none"><div class="label">Last error</div><div class="value"><span class="pill bad" id="grok-chat-last-error"></span></div></div>
          </div>
          <p class="hint" style="margin-top:14px">FlipAi keeps exactly one dedicated Grok Chat WebView2 owner. Slow Grok rendering is never treated as a dead browser, so it cannot spawn duplicate hidden browser trees and fill RAM.</p>
          <div class="actions" style="margin-top:14px"><a class="btn" href="/activity">Open Activity</a></div>
        </div>
      </details>
    </section>

    <style>
    #agent-grok-chat:checked~.agents-shell .agent-item[for="agent-grok-chat"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-grok-chat:checked~.agents-shell .agent-item[for="agent-grok-chat"] b{color:var(--brand-ink)}
    #agent-grok-chat:checked~.agents-shell #grok-chat-pane{display:block}
    .bmark.grok{font-family:Segoe UI Symbol,Segoe UI,sans-serif;font-weight:800}
    .grok-chat-actions{display:flex;gap:8px;flex-wrap:wrap}.grok-chat-actions form{margin:0}
    </style>
    <script>
    (function(){
      function text(id,v){var e=document.getElementById(id);if(e)e.textContent=v}
      function pill(id,state,label){var e=document.getElementById(id);if(!e)return;e.textContent=label;e.className='pill'+(state==='ok'?' ok':state==='bad'?' bad':state==='warn'?' warn':'')}
      function show(id,on){var e=document.getElementById(id);if(e)e.style.display=on?'':'none'}
      async function refreshGrokChat(){
        try{
          var r=await fetch('/grok-chat/status.json',{cache:'no-store'});if(!r.ok)return;
          var s=await r.json();var connected=!!s.connected||!!s.signedIn;var live=!!s.signedIn;
          pill('grok-chat-head-status',connected?'ok':'warn',connected?'Connected':'Not connected');
          var rail=document.getElementById('grok-chat-rail-status');if(rail){rail.textContent=connected?'Connected':'Not connected';rail.className='agent-chip'+(connected?'':' warn')}
          show('grok-chat-connect-form',!connected);show('grok-chat-disconnect-form',connected);show('grok-chat-test-form',connected);
          pill('grok-chat-saved',connected?'ok':'warn',connected?'Connected':'Not connected');
          pill('grok-chat-signin',live?'ok':'warn',live?'Ready':connected?'Restoring':'Not verified');
          var browserLabel='Stopped',browserState='';if(s.loginActive)browserLabel='Sign-in open';else if(s.starting)browserLabel='Starting';else if(s.running)browserLabel=live?'Ready':'Loading';
          pill('grok-chat-browser',live?'ok':(s.running||s.starting)?'warn':'',browserLabel);
          text('grok-chat-conversation',s.conversationId||'None yet');
          var er=document.getElementById('grok-chat-error-row');if(er)er.style.display=s.lastError?'flex':'none';text('grok-chat-last-error',s.lastError||'');
        }catch(e){}
      }
      refreshGrokChat();setInterval(refreshGrokChat,1500);
    })();
    </script>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around Grok Chat pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
