package main

import "strings"

// This runs after the existing ChatGPT augmentation and re-registers the Agents
// page with a fourth, independent Claude Chat pane. Keeping the browser agents
// as augmentations avoids disturbing the mature Codex/Claude CLI layout.
func init() {
	registerPage("agents", claudeChatDirectUI(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))))
}

func claudeChatDirectUI(body string) string {
	const chatRadio = `<input class="agent-switch" type="radio" name="agent-view" id="agent-chatgpt">`
	body = replaceAgentUIOnce(body, chatRadio, chatRadio+`
<input class="agent-switch" type="radio" name="agent-view" id="agent-claude-chat">`, "Claude Chat radio")

	const chatRail = `      <label class="agent-item" for="agent-chatgpt">
        <span class="bmark codex">{{brand "codex"}}</span>
        <span class="agent-item-copy">
          <b>ChatGPT Chat <span class="agent-chip warn" id="chatgpt-rail-status">Checking</span></b>
          <span>Answers {{.ChatGPTAccess.Prefix}}: messages</span>
        </span>
      </label>`
	const claudeChatRail = `
      <label class="agent-item" for="agent-claude-chat">
        <span class="bmark claude">{{brand "claude"}}</span>
        <span class="agent-item-copy">
          <b>Claude Chat <span class="agent-chip warn" id="claude-chat-rail-status">Checking</span></b>
          <span>Answers {{.ClaudeChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
	body = replaceAgentUIOnce(body, chatRail, chatRail+claudeChatRail, "Claude Chat rail item")

	const pane = `

    <!-- ------------------------- Claude Chat ------------------------- -->
    <section class="agent-pane" id="claude-chat-pane">
      <div class="agent-head">
        <div class="agent-head-main">
          <span class="bmark lg claude">{{brand "claude"}}</span>
          <div>
            <h2>Claude Chat <span class="pill warn" id="claude-chat-head-status">Checking</span></h2>
            <p>Regular Claude at claude.ai in FlipAi's private persistent browser session. No Claude Desktop app or Claude Code CLI is required.</p>
          </div>
        </div>
        <div class="agent-head-actions claude-chat-actions">
          <form id="claude-chat-connect-form" method="post" action="/claude-chat/connect"><button class="btn accent" type="submit">{{icon "link"}}Connect</button></form>
          <form id="claude-chat-disconnect-form" method="post" action="/claude-chat/disconnect" style="display:none" onsubmit="return confirm('Disconnect Claude Chat from FlipAi and remove its private sign-in profile?')"><button class="btn" type="submit">{{icon "x-ring"}}Disconnect</button></form>
          <form id="claude-chat-test-form" method="post" action="/claude-chat/test" style="display:none"><button class="btn" type="submit">{{icon "play"}}Test</button></form>
          <button class="btn primary" type="submit" form="claude-chat-settings">Save Claude Chat</button>
        </div>
      </div>

      <form id="claude-chat-settings" method="post" action="/agents/save">
        <section class="card">
          <div class="card-head divided"><div><h2>Routing &amp; conversation</h2><p>Claude Chat follows the same sticky SMS routing rules as the other agents.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="claudeChatPrefix">SMS shortcut</label>
                <input id="claudeChatPrefix" type="text" name="claudeChatPrefix" value="{{.ClaudeChatAccess.Prefix}}" maxlength="24" required>
                <p class="hint">Text <b>{{.ClaudeChatAccess.Prefix}}: hello</b> once to select Claude Chat. Unprefixed follow-ups stay here until you switch.</p>
              </div>
              <div class="field">
                <label for="claudeChatNewSessionCommand">New conversation word</label>
                <input id="claudeChatNewSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
                <p class="hint">Shared by every agent. Example: <b>{{.ClaudeChatAccess.Prefix}}: {{.S.NewSessionCommand}}</b>.</p>
              </div>
            </div>
            <p class="callout"><b>No default agent.</b> C:, A:, G:, or {{.ClaudeChatAccess.Prefix}}: changes the active agent for that phone. It stays selected across follow-up texts and PC/app restarts.</p>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>SMS instruction</h2><p>The same shared instruction used by Codex, Claude, ChatGPT Chat, and Claude Chat.</p></div></div>
          <div class="card-body">{{template "promptEditor" .SharedPrompt}}</div>
        </section>

        {{template "agentAccess" .ClaudeChatAccess}}
      </form>

      <details class="disclosure" style="margin-top:16px">
        <summary>Connection details</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row"><div class="label">Saved connection</div><div class="value"><span class="pill warn" id="claude-chat-saved">Checking</span></div></div>
            <div class="row"><div class="label">Live sign-in</div><div class="value"><span class="pill warn" id="claude-chat-signin">Checking</span></div></div>
            <div class="row"><div class="label">Browser session</div><div class="value"><span class="pill" id="claude-chat-browser">Stopped</span></div></div>
            <div class="row"><div class="label">Current conversation</div><div class="value"><span class="mono" id="claude-chat-conversation">None yet</span></div></div>
            <div class="row" id="claude-chat-error-row" style="display:none"><div class="label">Last error</div><div class="value"><span class="pill bad" id="claude-chat-last-error"></span></div></div>
          </div>
          <p class="hint" style="margin-top:14px">FlipAi keeps exactly one dedicated Claude Chat WebView2 owner. Slow Claude rendering is never treated as a dead browser, so it cannot spawn duplicate hidden browser trees and fill RAM.</p>
          <div class="actions" style="margin-top:14px"><a class="btn" href="/activity">Open Activity</a></div>
        </div>
      </details>
    </section>

    <style>
    #agent-claude-chat:checked~.agents-shell .agent-item[for="agent-claude-chat"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-claude-chat:checked~.agents-shell .agent-item[for="agent-claude-chat"] b{color:var(--brand-ink)}
    #agent-claude-chat:checked~.agents-shell #claude-chat-pane{display:block}
    .claude-chat-actions{display:flex;gap:8px;flex-wrap:wrap}.claude-chat-actions form{margin:0}
    </style>
    <script>
    (function(){
      function text(id,v){var e=document.getElementById(id);if(e)e.textContent=v}
      function pill(id,state,label){var e=document.getElementById(id);if(!e)return;e.textContent=label;e.className='pill'+(state==='ok'?' ok':state==='bad'?' bad':state==='warn'?' warn':'')}
      function show(id,on){var e=document.getElementById(id);if(e)e.style.display=on?'':'none'}
      async function refreshClaudeChat(){
        try{
          var r=await fetch('/claude-chat/status.json',{cache:'no-store'});if(!r.ok)return;
          var s=await r.json();var connected=!!s.connected||!!s.signedIn;var live=!!s.signedIn;
          pill('claude-chat-head-status',connected?'ok':'warn',connected?'Connected':'Not connected');
          var rail=document.getElementById('claude-chat-rail-status');if(rail){rail.textContent=connected?'Connected':'Not connected';rail.className='agent-chip'+(connected?'':' warn')}
          show('claude-chat-connect-form',!connected);show('claude-chat-disconnect-form',connected);show('claude-chat-test-form',connected);
          pill('claude-chat-saved',connected?'ok':'warn',connected?'Connected':'Not connected');
          pill('claude-chat-signin',live?'ok':'warn',live?'Ready':connected?'Restoring':'Not verified');
          var browserLabel='Stopped',browserState='';if(s.loginActive)browserLabel='Sign-in open';else if(s.starting)browserLabel='Starting';else if(s.running)browserLabel=live?'Ready':'Loading';
          pill('claude-chat-browser',live?'ok':(s.running||s.starting)?'warn':'',browserLabel);
          text('claude-chat-conversation',s.conversationId||'None yet');
          var er=document.getElementById('claude-chat-error-row');if(er)er.style.display=s.lastError?'flex':'none';text('claude-chat-last-error',s.lastError||'');
        }catch(e){}
      }
      refreshClaudeChat();setInterval(refreshClaudeChat,1500);
    })();
    </script>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around Claude Chat pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
