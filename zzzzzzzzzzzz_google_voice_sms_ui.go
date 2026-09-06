package main

import "strings"

// removeTemplateSectionContaining removes one top-level <section> from a page
// template without deleting the implementation behind it. It is intentionally
// small and deterministic: this is used only for the historical Gmail card,
// whose source stays in Git (and on archive/gmail-voice-bridge-v0.46.50) so it
// can be restored later without rebuilding the backend.
func removeTemplateSectionContaining(body, needle string) string {
	at := strings.Index(body, needle)
	if at < 0 {
		return body
	}
	start := strings.LastIndex(body[:at], "<section")
	if start < 0 {
		return body
	}
	relEnd := strings.Index(body[at:], "</section>")
	if relEnd < 0 {
		return body
	}
	end := at + relEnd + len("</section>")
	return body[:start] + body[end:]
}

// Direct Google Voice is the only SMS connection shown in the current app.
// The Gmail/IMAP/OAuth transport code is deliberately retained in the repo for
// rollback, but there is no Gmail connect/manage surface in the product UI.
func init() {
	body := connectionsHTML
	body = removeTemplateSectionContaining(body, "Gmail / Google Voice")
	body = strings.Replace(body,
		`<p>Configure how FlipAi reads Google Voice texts from Gmail and sends replies back.</p>`,
		`<p>Connect the Google Voice account FlipAi uses to receive and send texts.</p>`, 1)
	body = strings.Replace(body,
		`<div class="page-actions">
    <a class="btn" href="/connections">{{icon "refresh"}}Refresh</a>
    <button class="btn accent" type="button" data-test="/gmail/test" data-test-busy="Checking Gmail">{{icon "send"}}Test Gmail</button>
  </div>`,
		`<div class="page-actions"><a class="btn" href="/connections">{{icon "refresh"}}Refresh</a></div>`, 1)

	card := `
<section class="card" id="gv-sms-connection">
  <div class="card-head divided">
    <div class="card-title-row">
      <span class="bmark lg google">{{brand "google"}}</span>
      <div>
        <h2>Google Voice SMS <span id="gv-sms-pill" class="pill warn">Not connected</span></h2>
        <p>Send and receive texts and media through FlipAi's private Google Voice browser. Email forwarding is not required.</p>
      </div>
    </div>
    <div class="head-actions">
      <button class="btn accent" id="gv-sms-connect" type="button">Connect</button>
    </div>
  </div>
  <div class="card-body">
    <div class="rows">
      <div class="row"><div class="label">Google Voice SMS account<span>This has its own private browser profile. It is separate from Google Voice calling.</span></div><div class="value"><b id="gv-sms-signin">Checking…</b></div></div>
      <div class="row"><div class="label">SMS listener<span>After sign-in, the Messages page stays active in its own background browser.</span></div><div class="value"><b id="gv-sms-listener">Checking…</b></div></div>
      <div class="row"><div class="label">SMS transport<span>Google Voice is FlipAi's only active SMS reader.</span></div><div class="value"><b id="gv-sms-mode">{{if eq .S.GmailMethod "google_voice"}}Direct Google Voice{{else}}Not selected{{end}}</b></div></div>
      <div class="row"><div class="label">MMS / attachments<span>Incoming photos, audio and supported video are passed to the selected agent as the actual file.</span></div><div class="value"><b>Direct file delivery</b></div></div>
    </div>
    <p class="hint" id="gv-sms-note">Press Connect. FlipAi will open a Google Voice window for this SMS connection. Sign in there once; normal operation stays in the background.</p>
  </div>
</section>
<script>
(() => {
  const svc='http://127.0.0.1:8772';
  const button=document.getElementById('gv-sms-connect');
  const pill=document.getElementById('gv-sms-pill');
  const signin=document.getElementById('gv-sms-signin');
  const listener=document.getElementById('gv-sms-listener');
  const mode=document.getElementById('gv-sms-mode');
  const note=document.getElementById('gv-sms-note');
  if(!button)return;
  let selected={{if eq .S.GmailMethod "google_voice"}}true{{else}}false{{end}}, connected=false, loginActive=false, restartWhenReady=false;
  const restart=async()=>{try{await fetch('/bridge/restart',{method:'POST',headers:{'X-FlipAi-Inline':'1'}})}catch(_){}setTimeout(()=>location.reload(),2200)};
  const show=(s)=>{
    const wasConnected=connected;
    selected=!!s.selected;connected=!!s.connected;loginActive=!!s.loginActive;
    if(signin){
      signin.textContent=connected||s.signedIn?'Signed in':(loginActive?'Sign-in window open':(s.starting?'Opening sign-in…':'Not signed in'));
    }
    if(listener)listener.textContent=!selected?'Off':(connected?'Ready':(s.listenerRunning?'Starting…':'Not running'));
    if(mode)mode.textContent=selected?'Direct Google Voice':'Not selected';
    if(connected) pill.textContent='Connected';
    else if(loginActive) pill.textContent='Sign in';
    else if(s.starting) pill.textContent='Opening…';
    else if(selected&&s.listenerError) pill.textContent='Needs attention';
    else pill.textContent='Not connected';
    pill.className='pill '+(connected?'ok':'warn');
    if(connected){button.textContent='Disconnect';button.className='btn';}
    else if(loginActive){button.textContent='Cancel';button.className='btn';}
    else if(selected){button.textContent='Retry sign-in';button.className='btn accent';}
    else{button.textContent='Connect';button.className='btn accent';}
    if(note&&connected)note.textContent='Google Voice SMS is signed in and its background Messages listener is verified ready.'+(s.listenerNote?' Last check: '+s.listenerNote+'.':'');
    else if(note&&loginActive)note.textContent='Sign in to Google Voice in the separate window FlipAi opened. After setup, SMS runs hidden in the background.';
    else if(note&&s.starting)note.textContent='FlipAi is opening the separate Google Voice SMS sign-in window.';
    else if(note&&selected&&s.listenerError)note.textContent=s.listenerError;
    else if(note&&!selected)note.textContent='Press Connect. FlipAi will open a separate Google Voice SMS sign-in window.';
    if(restartWhenReady&&connected&&!wasConnected){restartWhenReady=false;restart();}
  };
  async function status(){
    try{const r=await fetch(svc+'/status',{cache:'no-store'});if(r.ok)show(await r.json())}
    catch(_){if(signin)signin.textContent='Service unavailable';if(listener)listener.textContent='Unavailable';pill.textContent='Needs attention';pill.className='pill warn'}
  }
  button.addEventListener('click',async()=>{
    button.disabled=true;
    const disconnecting=connected||loginActive;
    button.textContent=disconnecting?'Disconnecting…':'Opening sign-in…';
    try{
      const r=await fetch(svc+(disconnecting?'/disconnect':'/connect'),{method:'POST',headers:{'Content-Type':'application/json'}});
      let data={};try{data=await r.json()}catch(_){}
      if(!r.ok)throw new Error(data.message||'Google Voice SMS connection failed');
      if(note&&data.message)note.textContent=data.message;
      if(disconnecting){restartWhenReady=false;await restart();return;}
      restartWhenReady=true;
      button.disabled=false;
      await status();
    }catch(e){if(note)note.textContent=e.message||String(e);button.disabled=false;await status()}
  });
  status();setInterval(status,1500);
})();
</script>
`
	if i := strings.LastIndex(body, "{{end}}"); i >= 0 {
		body = body[:i] + card + body[i:]
	}
	registerPage("connections", body)
}
