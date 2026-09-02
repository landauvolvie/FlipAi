package main

import "strings"

// ChatGPT Chat is a real third connection now. It uses a FlipAi-owned WebView2
// profile that is completely separate from the normal ChatGPT desktop app.
// The user signs in once; background SMS turns then use that same profile
// without Windows accessibility, mouse/keyboard automation or focus stealing.
func init() {
	registerPage("agents", chatGPTWebUI(agentConnectFirstRunHTML(agentsPageHTML)))
}

func chatGPTWebUI(body string) string {
	const radios = `<input class="agent-switch" type="radio" name="agent-view" id="agent-codex" checked>
<input class="agent-switch" type="radio" name="agent-view" id="agent-claude">`
	body = replaceAgentUIOnce(body, radios, radios+`
<input class="agent-switch" type="radio" name="agent-view" id="agent-chatgpt">`, "ChatGPT WebView radio")

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
          <b>ChatGPT Chat <span class="agent-chip warn" id="chatgpt-rail-chip">Checking</span></b>
          <span>Answers G: messages</span>
        </span>
      </label>`
	body = replaceAgentUIOnce(body, claudeRail, claudeRail+chatRail, "ChatGPT WebView rail item")

	const pane = `

    <!-- ------------------------- ChatGPT Chat ------------------------- -->
    <section class="agent-pane" id="chatgpt-pane">
      <div class="agent-head">
        <div class="agent-head-main">
          <span class="bmark lg codex">{{brand "codex"}}</span>
          <div>
            <h2>ChatGPT Chat <span class="pill warn" id="chatgpt-head-chip">Checking</span></h2>
            <p>Your normal ChatGPT account, through a private FlipAi browser session. No accessibility or visible desktop-app automation.</p>
          </div>
        </div>
        <div class="agent-head-actions">
          <button class="btn accent" type="button" id="chatgpt-connect" onclick="flipChatGPTAction('connect')">Connect ChatGPT</button>
          <button class="btn" type="button" id="chatgpt-test" onclick="flipChatGPTAction('test')">Test</button>
          <button class="btn" type="button" onclick="flipChatGPTAction('show')">Show session</button>
        </div>
      </div>

      <p class="callout"><b>How it works:</b> Connect opens ChatGPT in a dedicated WebView2 profile only FlipAi uses. Sign in once. After the ChatGPT composer appears, FlipAi parks that view off-screen and sends future <b>G:</b> SMS turns through the page internally. The assistant reply is captured from ChatGPT's completed network response when possible, with the new assistant DOM message as a fallback.</p>

      <section class="card">
        <div class="card-head divided">
          <div><h2>Connection</h2><p>These values come from the live private WebView, not from the normal ChatGPT desktop app.</p></div>
          <a class="btn" href="/activity">Open Activity</a>
        </div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">ChatGPT session<span>Signed in means the real ChatGPT composer is present in FlipAi's private profile.</span></div><div class="value"><span class="pill warn" id="chatgpt-session-value">Checking</span></div></div>
            <div class="row"><div class="label">Background browser<span>Runs parked off-screen between turns. It does not move or inspect your normal ChatGPT window.</span></div><div class="value" id="chatgpt-browser-value">Checking</div></div>
            <div class="row"><div class="label">Current saved chat<span>FlipAi remembers the ChatGPT conversation id so the next G: text continues the same chat.</span></div><div class="value" id="chatgpt-conversation-value">—</div></div>
            <div class="row"><div class="label">Last response capture<span>Network is preferred; DOM is the automatic fallback.</span></div><div class="value" id="chatgpt-capture-value">—</div></div>
            <div class="row"><div class="label">Last ChatGPT turn<span>Measured from FlipAi handing the prompt to the WebView until the assistant reply was captured.</span></div><div class="value" id="chatgpt-duration-value">—</div></div>
            <div class="row"><div class="label">Windows accessibility<span>No UI Automation tree, mouse clicks, SendKeys or desktop focus are used for background ChatGPT turns.</span></div><div class="value"><span class="pill">Not used</span></div></div>
          </div>
          <div class="hint" id="chatgpt-detail" style="margin-top:14px">Checking the ChatGPT WebView…</div>
          <div class="hint" id="chatgpt-action-result" style="margin-top:8px"></div>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>SMS chat</h2><p>Use your existing allowed SMS number. ChatGPT Chat has its own routing prefix.</p></div></div>
        <div class="card-body">
          <div class="rows">
            <div class="row"><div class="label">ChatGPT prefix<span>Send G: followed by your message.</span></div><div class="value"><code>G:</code></div></div>
            <div class="row"><div class="label">Continue current chat<span>Every normal G: message continues the ChatGPT conversation FlipAi remembered.</span></div><div class="value"><code>G: your message</code></div></div>
            <div class="row"><div class="label">Start a new chat<span>The normal NEW command works with the ChatGPT prefix too.</span></div><div class="value"><code>G NEW</code></div></div>
            <div class="row"><div class="label">Chat history<span>These are normal saved ChatGPT chats in the account you sign into, not API-only conversations.</span></div><div class="value">Same account</div></div>
          </div>
          <div class="actions" style="margin-top:16px">
            <button class="btn" type="button" onclick="flipChatGPTAction('new')">New SMS chat</button>
            <button class="btn danger" type="button" onclick="if(confirm('Disconnect ChatGPT from FlipAi and remove only FlipAi\'s private ChatGPT browser profile?')) flipChatGPTAction('disconnect')">Disconnect</button>
          </div>
          <p class="hint" style="margin-top:12px">For this first WebView release, ChatGPT uses the same phone allowlist/security gate that already admitted the SMS to Codex or Claude. Attachments sent to G: are refused with a clear message instead of being silently dropped.</p>
        </div>
      </section>

      <section class="card">
        <div class="card-head divided"><div><h2>Troubleshooting trail</h2><p>Activity records the stage and timing, never the prompt, assistant text, cookies or tokens.</p></div></div>
        <div class="card-body"><p class="hint" style="margin:0">Look for stage <b>chatgpt</b>: WebView start requested → sign-in window opened → signed in successfully → prompt submitted → reply captured via network/DOM. Failures name the missing composer/send control or WebView problem, so you can send that Activity entry back without exposing conversation content.</p></div>
      </section>
    </section>

    <style>
    #agent-chatgpt:checked~.agents-shell .agent-item[for="agent-chatgpt"]{background:var(--brand-soft);border-color:var(--brand-line)}
    #agent-chatgpt:checked~.agents-shell .agent-item[for="agent-chatgpt"] b{color:var(--brand-ink)}
    #agent-chatgpt:checked~.agents-shell #chatgpt-pane{display:block}
    #chatgpt-pane code{font:700 12px ui-monospace,SFMono-Regular,Consolas,monospace;background:var(--soft);padding:5px 8px;border-radius:8px}
    </style>

    <script>
    (function(){
      const ep='/chatgpt-direct/probe?web=1&action=';
      const by=id=>document.getElementById(id);
      function setChip(el,text,good){ if(!el)return; el.textContent=text; el.className='pill'+(good?'':' warn'); }
      function apply(s){
        s=s||{};
        const ready=!!(s.running&&s.signedIn&&s.composerReady);
        const rail=by('chatgpt-rail-chip'); if(rail){ rail.textContent=ready?'Connected':(s.running?'Sign in':'Not connected'); rail.className='agent-chip'+(ready?'':' warn'); }
        setChip(by('chatgpt-head-chip'),ready?'Connected':(s.running?'Sign in needed':'Not connected'),ready);
        setChip(by('chatgpt-session-value'),ready?'Signed in':(s.running?'Waiting for sign-in':'Not running'),ready);
        if(by('chatgpt-browser-value')) by('chatgpt-browser-value').textContent=s.running?'Running in FlipAi':'Stopped';
        if(by('chatgpt-conversation-value')) by('chatgpt-conversation-value').textContent=s.conversationId?s.conversationId:'New chat on next G: message';
        if(by('chatgpt-capture-value')) by('chatgpt-capture-value').textContent=s.lastCapture||'—';
        if(by('chatgpt-duration-value')) by('chatgpt-duration-value').textContent=s.lastDurationMs?((s.lastDurationMs/1000).toFixed(1)+' s'):'—';
        if(by('chatgpt-detail')) by('chatgpt-detail').textContent=s.lastError?('Last error: '+s.lastError):(s.detail||'');
        if(by('chatgpt-test')) by('chatgpt-test').disabled=!ready;
        if(by('chatgpt-connect')) by('chatgpt-connect').textContent=ready?'ChatGPT connected':'Connect ChatGPT';
      }
      async function refresh(){
        try{ const r=await fetch(ep+'status',{cache:'no-store'}); const j=await r.json(); apply(j.status||{}); }
        catch(e){ if(by('chatgpt-detail')) by('chatgpt-detail').textContent='Could not read ChatGPT status: '+e.message; }
      }
      globalThis.flipChatGPTAction=async function(action){
        const out=by('chatgpt-action-result'); if(out) out.textContent=action==='test'?'Sending a real ChatGPT test prompt…':'Working…';
        try{
          const r=await fetch(ep+encodeURIComponent(action),{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});
          const j=await r.json();
          if(!r.ok) throw new Error(j.error||('HTTP '+r.status));
          apply(j.status||{});
          let msg=j.message||'Done.';
          if(action==='test'&&j.capture) msg+=' Captured via '+j.capture+(j.durationMs?' in '+(j.durationMs/1000).toFixed(1)+' s.':'.');
          if(out) out.textContent=msg;
        }catch(e){ if(out) out.textContent='Failed: '+e.message; }
        setTimeout(refresh,500); setTimeout(refresh,1800);
      };
      refresh(); setInterval(refresh,2500);
    })();
    </script>`

	marker := "\n  </div>\n</div>\n{{end}}"
	idx := strings.LastIndex(body, marker)
	if idx < 0 {
		panic("FlipAi Agents template changed around ChatGPT WebView pane insertion")
	}
	return body[:idx] + pane + body[idx:]
}
