package main

import "strings"

// Add direct Google Voice SMS as a second connection choice without moving or
// changing the existing Google Voice calling controls injected on this page.
func init() {
	body := connectionsHTML
	// When Direct Google Voice is selected, the bridge's generic mail transport
	// is healthy by design, but that does not mean Gmail itself is connected.
	body = strings.Replace(body,
		`<p>Configure how FlipAi reads Google Voice texts from Gmail and sends replies back.</p>`,
		`<p>Choose how FlipAi receives Google Voice texts and sends replies back.</p>`, 1)
	body = strings.Replace(body,
		`<h2>Gmail / Google Voice <span class="pill {{if .S.GmailReady}}ok{{else}}warn{{end}}">{{if .S.GmailReady}}Connected{{else}}Not connected{{end}}</span></h2>`,
		`<h2>Gmail / Google Voice <span class="pill {{if and .S.GmailReady (ne .S.GmailMethod "google_voice")}}ok{{else}}warn{{end}}">{{if and .S.GmailReady (ne .S.GmailMethod "google_voice")}}Connected{{else}}Not connected{{end}}</span></h2>`, 1)
	body = strings.Replace(body,
		`<div class="row"><div class="label">Authentication method</div><div class="value"><b>{{.S.GmailMethodLabel}}</b>{{if .S.GmailReady}}<span class="pill ok">Valid</span>{{else if .S.GmailMethod}}<span class="pill warn">Incomplete</span>{{end}}</div></div>`,
		`<div class="row"><div class="label">Authentication method</div><div class="value"><b>{{.S.GmailMethodLabel}}</b>{{if eq .S.GmailMethod "google_voice"}}<span class="pill">Not selected</span>{{else if .S.GmailReady}}<span class="pill ok">Valid</span>{{else if .S.GmailMethod}}<span class="pill warn">Incomplete</span>{{end}}</div></div>`, 1)
	body = strings.Replace(body,
		`<div class="row"><div class="label">Reply address<span>FlipAi always answers the authenticated Google Voice thread the text arrived on.</span></div><div class="value"><b>Authenticated Voice thread</b>{{if .ReplyReady}}<span class="pill ok">Ready</span>{{else}}<span class="pill warn">Waiting</span>{{end}}</div></div>`,
		`<div class="row"><div class="label">Reply address<span>FlipAi always answers the authenticated Google Voice thread the text arrived on.</span></div><div class="value"><b>Authenticated Voice thread</b>{{if eq .S.GmailMethod "google_voice"}}<span class="pill">Not using Gmail</span>{{else if .ReplyReady}}<span class="pill ok">Ready</span>{{else}}<span class="pill warn">Waiting</span>{{end}}</div></div>`, 1)

	card := `
<section class="card" id="gv-sms-connection">
  <div class="card-head divided">
    <div class="card-title-row">
      <span class="bmark lg google">{{brand "google"}}</span>
      <div>
        <h2>Google Voice SMS <span id="gv-sms-pill" class="pill warn">{{if eq .S.GmailMethod "google_voice"}}Not connected{{else}}Not connected{{end}}</span></h2>
        <p>Send and receive texts directly through a private Google Voice SMS browser. Gmail forwarding is not required.</p>
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
      <div class="row"><div class="label">SMS transport<span>Only one reader is active, so Gmail and direct Voice cannot answer the same text twice.</span></div><div class="value"><b id="gv-sms-mode">{{if eq .S.GmailMethod "google_voice"}}Direct Google Voice{{else}}Gmail / not selected{{end}}</b></div></div>
    </div>
    <p class="hint" id="gv-sms-note">Press Connect. FlipAi will open a Google Voice window for this SMS connection. Sign in there once; calling uses a different profile and is not changed.</p>
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
    if(mode)mode.textContent=selected?'Direct Google Voice':'Gmail / not selected';
    pill.textContent=connected?'Connected':(selected&&(s.listenerError||s.running)?'Needs attention':'Not connected');
    pill.className='pill '+(connected?'ok':'warn');
    if(connected){button.textContent='Disconnect';button.className='btn';}
    else if(loginActive){button.textContent='Cancel';button.className='btn';}
    else if(selected){button.textContent='Retry sign-in';button.className='btn accent';}
    else{button.textContent='Connect';button.className='btn accent';}
    if(note&&connected)note.textContent='Direct Google Voice SMS is signed in and the Messages listener is verified ready.';
    else if(note&&loginActive)note.textContent='Sign in to Google Voice in the separate window FlipAi opened. This SMS login is intentionally separate from calling.';
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
