package main

import "strings"

// Add direct Google Voice SMS as a second connection choice without moving or
// changing the existing Google Voice calling controls injected on this page.
func init() {
	body := connectionsHTML
	card := `
<section class="card" id="gv-sms-connection">
  <div class="card-head divided">
    <div class="card-title-row">
      <span class="bmark lg google">{{brand "google"}}</span>
      <div>
        <h2>Google Voice SMS <span id="gv-sms-pill" class="pill {{if eq .S.GmailMethod "google_voice"}}ok{{else}}warn{{end}}">{{if eq .S.GmailMethod "google_voice"}}Connected{{else}}Not connected{{end}}</span></h2>
        <p>Send and receive texts directly through your signed-in Google Voice session. Gmail forwarding is not required.</p>
      </div>
    </div>
    <div class="head-actions">
      <button class="btn {{if ne .S.GmailMethod "google_voice"}}accent{{end}}" id="gv-sms-connect" type="button">{{if eq .S.GmailMethod "google_voice"}}Disconnect{{else}}Connect{{end}}</button>
    </div>
  </div>
  <div class="card-body">
    <div class="rows">
      <div class="row"><div class="label">Google Voice sign-in<span>Uses FlipAi's existing private Google Voice browser profile.</span></div><div class="value"><b id="gv-sms-signin">Checking…</b></div></div>
      <div class="row"><div class="label">SMS transport<span>Only one reader is active, so Gmail and direct Voice cannot answer the same text twice.</span></div><div class="value"><b id="gv-sms-mode">{{if eq .S.GmailMethod "google_voice"}}Direct Google Voice{{else}}Gmail / not selected{{end}}</b></div></div>
    </div>
    <p class="hint" id="gv-sms-note">When connected, all existing agent routing, security codes, NEW/STATUS commands, acknowledgements and replies keep working the same way.</p>
  </div>
</section>
<script>
(() => {
  const svc='http://127.0.0.1:8772';
  const button=document.getElementById('gv-sms-connect');
  const pill=document.getElementById('gv-sms-pill');
  const signin=document.getElementById('gv-sms-signin');
  const mode=document.getElementById('gv-sms-mode');
  const note=document.getElementById('gv-sms-note');
  if(!button)return;
  let selected={{if eq .S.GmailMethod "google_voice"}}true{{else}}false{{end}};
  const show=(s)=>{
    selected=!!s.selected;
    if(signin)signin.textContent=s.signedIn?'Signed in':(s.browserRunning?'Sign-in needed':'Opening Google Voice…');
    if(mode)mode.textContent=selected?'Direct Google Voice':'Gmail / not selected';
    pill.textContent=selected?'Connected':'Not connected';pill.className='pill '+(selected?'ok':'warn');
    button.textContent=selected?'Disconnect':'Connect';button.className='btn '+(selected?'':'accent');
  };
  async function status(){try{const r=await fetch(svc+'/status',{cache:'no-store'});if(r.ok)show(await r.json())}catch(_){if(signin)signin.textContent='Google Voice service unavailable'}}
  async function restart(){try{await fetch('/bridge/restart',{method:'POST',headers:{'X-FlipAi-Inline':'1'}})}catch(_){}setTimeout(()=>location.reload(),2200)}
  button.addEventListener('click',async()=>{
    button.disabled=true;const was=selected;button.textContent=was?'Disconnecting…':'Connecting…';
    try{
      const r=await fetch(svc+(was?'/disconnect':'/connect'),{method:'POST',headers:{'Content-Type':'application/json'}});
      let data={};try{data=await r.json()}catch(_){}
      if(!r.ok){throw new Error(data.message||'Google Voice SMS could not connect')}
      if(note&&data.message)note.textContent=data.message;
      await restart();
    }catch(e){if(note)note.textContent=e.message||String(e);button.disabled=false;button.textContent=was?'Disconnect':'Connect';await status()}
  });
  status();setInterval(status,2500);
})();
</script>
`
	if i := strings.LastIndex(body, "{{end}}"); i >= 0 {
		body = body[:i] + card + body[i:]
	}
	registerPage("connections", body)
}
