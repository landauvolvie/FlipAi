package main

import "strings"

// This runs after the existing ChatGPT augmentation and re-registers the Agents
// page with a fifth, independent Gemini Chat pane. Keeping the browser agents
// as augmentations avoids disturbing the mature Codex/Claude CLI layout.
func init() {
	registerPage("agents", geminiChatDirectUI(claudeChatDirectUI(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML)))))
}

func geminiChatDirectUI(body string) string {
	body = strings.Replace(body, `<p>C: selects Codex, A: selects Claude, and G: selects ChatGPT Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, `<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, and M: selects Gemini Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, 1)
	const chatRadio = `<input class="agent-switch" type="radio" name="agent-view" id="agent-chatgpt">`
	body = replaceAgentUIOnce(body, chatRadio, chatRadio+`
<input class="agent-switch" type="radio" name="agent-view" id="agent-gemini-chat">`, "Gemini Chat radio")

	const geminiRailAnchor = `      <label class="agent-item" for="agent-claude-chat">
        <span class="bmark claude">{{brand "claude"}}</span>
        <span class="agent-item-copy">
          <b>Claude Chat <span class="agent-chip warn" id="claude-chat-rail-status">Checking</span></b>
          <span>Answers {{.ClaudeChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	const geminiChatRail = `
      <label class="agent-item" for="agent-gemini-chat">
        <span class="bmark google">{{brand "google"}}</span>
        <span class="agent-item-copy">
          <b>Gemini Chat <span class="agent-chip warn" id="gemini-chat-rail-status">Checking</span></b>
          <span>Answers {{.GeminiChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	body = replaceAgentUIOnce(body, geminiRailAnchor, geminiRailAnchor+geminiChatRail, "Gemini Chat rail item")

	const pane = `

    <!-- ------------------------- Gemini Chat ------------------------- -->
    <section class="agent-pane" id="gemini-chat-pane">
      <div class="agent-head">
        <div class="agent-head-main">
          <span class="bmark lg google">{{brand "google"}}</span>
          <div>
            <h2>Gemini Chat <span class="pill warn" id="gemini-chat-head-status">Checking</span></h2>
            <p>Regular Gemini at gemini.google.com in FlipAi's private persistent browser session. No Gemini API or Gemini CLI is required.</p>
          </div>
        </div>
        <div class="agent-head-actions gemini-chat-actions">
          <form id="gemini-chat-connect-form" method="post" action="/gemini-chat/connect"><button class="btn accent" type="submit">{{icon "link"}}Connect</button></form>
          <form id="gemini-chat-disconnect-form" method="post" action="/gemini-chat/disconnect" style="display:none" onsubmit="return confirm('Disconnect Gemini Chat from FlipAi and remove its private sign-in profile?')"><button class="btn" type="submit">{{icon "x-ring"}}Disconnect</button></form>
          <form id="gemini-chat-test-form" method="post" action="/gemini-chat/test" style="display:none"><button class="btn" type="submit">{{icon "play"}}Test</button></form>
          <button class="btn primary" type="submit" form="gemini-chat-settings">Save Gemini Chat</button>
        </div>
      </div>

      <form id="gemini-chat-settings" method="post" action="/agents/save">
        <section class="card">
          <div class="card-head divided"><div><h2>Routing &amp; conversation</h2><p>Gemini Chat follows the same sticky SMS routing rules as the other agents.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="geminiChatPrefix">SMS shortcut</label>
                <input id="geminiChatPrefix" type="text" name="geminiChatPrefix" value="{{.GeminiChatAccess.Prefix}}" maxlength="24" required>
                <p class="hint">Text <b>{{.GeminiChatAccess.Prefix}}: hello</b> once to select Gemini Chat. Unprefixed follow-ups stay here until you switch.</p>
              </div>
              <div class="field">
                <label for="geminiChatNewSessionCommand">New conversation word</label>
                <input id="geminiChatNewSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
                <p class="hint">Shared by every agent. Example: <b>{{.GeminiChatAccess.Prefix}}: {{.S.NewSessionCommand}}</b>.</p>
              </div>
            </div>
            <p class="callout"><b>No default agent.</b> C:, A:, G:, H:, or {{.GeminiChatAccess.Prefix}}: changes the active agent for that phone. It stays selected across follow-up texts and PC/app restarts.</p>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>SMS instruction</h2><p>The same shared instruction used by Codex, Claude, ChatGPT Chat, Claude Chat, and Gemini Chat.</p></div></div>
          <div class="card-body">{{template "promptEditor" .SharedPrompt}}</div>
        </section>

        {{template "agentAccess" .GeminiChatAccess}}
      </form>

      <details class="disclosure" style="margin-top:16px">
        <summary>Connection details</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row"><div class="label">Saved connection</div><div class="value"><span class="pill warn" id="gemini-chat-saved">Checking</span></div></div>
            <div class="row"><div class="label">Live sign-in</div><div class="value"><span class="pill warn" id="gemini-chat-signin">Checking</span></div></div>
            <div class="row"><div class="label">Browser session</div><div class="value"><span class="pill" id="gemini-chat-browser">Stopped</span></div></div>
            <div class="row"><div class="label">Current conversation</div><div class="value"><span class="mono" id="gemini-chat-conversation">None yet</span></div></div>
            <div class="row" id="gemini-chat-error-row" style="display:none"><div class="label">Last error</div><div class="value"><span class="pill bad" id="gemini-chat-last-error"></span></div></div>
          </div>
          <p class="hint" style="margin-top:14px">FlipAi keeps exactly one dedicated Gemini Chat WebView2 owner. Slow Gemini rendering is never treated as a dead browser, so it cannot spawn duplicate hidden browser trees and fill RAM.</p>
          <div class="actions" style="margin-top:14px"><a class="btn" href="/activity">Open Activity</a></div>
        </div>
      </details>
    </section>

    <style>
    #agent-gemini-chat:checked~.agents-shell .agent-item[for="agent-gemini-chat"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-gemini-chat:checked~.agents-shell .agent-item[for="agent-gemini-chat"] b{color:var(--brand-ink)}
    #agent-gemini-chat:checked~.agents-shell #gemini-chat-pane{display:block}
    .gemini-chat-actions{display:flex;gap:8px;flex-wrap:wrap}.gemini-chat-actions form{margin:0}
    </style>
    <script>
    (function(){
      function text(id,v){var e=document.getElementById(id);if(e)e.textContent=v}
      function pill(id,state,label){var e=document.getElementById(id);if(!e)return;e.textContent=label;e.className='pill'+(state==='ok'?' ok':state==='bad'?' bad':state==='warn'?' warn':'')}
      function show(id,on){var e=document.getElementById(id);if(e)e.style.display=on?'':'none'}
      async function refreshGeminiChat(){
        try{
          var r=await fetch('/gemini-chat/status.json',{cache:'no-store'});if(!r.ok)return;
          var s=await r.json();var connected=!!s.connected||!!s.signedIn;var live=!!s.signedIn;
          pill('gemini-chat-head-status',connected?'ok':'warn',connected?'Connected':'Not connected');
          var rail=document.getElementById('gemini-chat-rail-status');if(rail){rail.textContent=connected?'Connected':'Not connected';rail.className='agent-chip'+(connected?'':' warn')}
          show('gemini-chat-connect-form',!connected);show('gemini-chat-disconnect-form',connected);show('gemini-chat-test-form',connected);
          pill('gemini-chat-saved',connected?'ok':'warn',connected?'Connected':'Not connected');
          pill('gemini-chat-signin',live?'ok':'warn',live?'Ready':connected?'Restoring':'Not verified');
          var browserLabel='Stopped',browserState='';if(s.loginActive)browserLabel='Sign-in open';else if(s.starting)browserLabel='Starting';else if(s.running)browserLabel=live?'Ready':'Loading';
          pill('gemini-chat-browser',live?'ok':(s.running||s.starting)?'warn':'',browserLabel);
          text('gemini-chat-conversation',s.conversationId||'None yet');
          var er=document.getElementById('gemini-chat-error-row');if(er)er.style.display=s.lastError?'flex':'none';text('gemini-chat-last-error',s.lastError||'');
        }catch(e){}
      }
      refreshGeminiChat();setInterval(refreshGeminiChat,1500);
    })();
    </script>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around Gemini Chat pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
