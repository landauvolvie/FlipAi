package main

// voiceDesktopInitScript augments only FlipAi's trusted localhost desktop UI.
//
// It lives in a platform-independent file for the same reason
// googleVoiceInitScript does: voice_ui_browser_test.go runs this exact string
// in headless Chromium against the real local voice endpoint, which is the only
// way to prove that the switch on this card really does turn calling on. The
// bug it exists to catch -- the switch that never saved -- was invisible to
// every test there was, because every test there was stopped at the Go side.
// The same pages opened in a normal browser remain unchanged. Keeping these
// controls client-side also means the existing SMS handlers and templates are
// untouched by the voice-call feature.
//
// The feature lives on two pages, each doing one job:
//
//	Settings     -- the whole setup: the switch that turns calling on, the
//	                Google sign-in and sign-out, the desktop apps a call is
//	                put through to, and every status and permission check,
//	                each with what to do about it.
//	Connections  -- the live view: the real Google Voice browser, standing
//	                inside the page. Leaving the page only takes the panel
//	                away; Google Voice keeps running and answering in the
//	                background.
//
// Nothing on either card waits for a Save button. Every control writes as soon
// as it is changed, and the switch that decides whether FlipAi answers the
// phone writes through an endpoint of its own so that nothing else on the page
// can hold it up.
const voiceDesktopInitScript = `
(() => {
  const VOICE = 'http://127.0.0.1:8771';
  const q = (s, root=document) => root.querySelector(s);
  const checked = (id) => !!q('#'+id)?.checked;
  const value = (id) => q('#'+id)?.value || '';
  let snapshot = null;

  function E(tag, cls, text) {
    const e=document.createElement(tag);
    if(cls) e.className=cls;
    if(text!==undefined) e.textContent=text;
    return e;
  }
  function pill(text, tone) { return E('span','pill'+(tone?' '+tone:''),text); }
  function btn(text, cls='btn') { const b=E('button',cls,text); b.type='button'; return b; }
  function input(id, val, placeholder='') { const x=E('input'); x.id=id; x.value=val||''; x.placeholder=placeholder; return x; }
  function select(id, items, selected) {
    const s=E('select'); s.id=id;
    for(const item of items){ const o=E('option','',item[1]); o.value=item[0]; if(item[0]===selected)o.selected=true; s.append(o); }
    return s;
  }
  function field(label, control, hint) {
    const f=E('div','field'); const l=E('label','',label); if(control.id)l.htmlFor=control.id; f.append(l,control);
    if(hint) f.append(E('p','hint',hint)); return f;
  }
  function toggle(id, label, hint, on) {
    const row=E('div','toggle'); const lab=E('div','label',label); if(hint)lab.append(E('span','',hint));
    const sw=E('label','switch'); const x=E('input'); x.type='checkbox'; x.id=id; x.checked=!!on; sw.append(x,E('span','slider')); row.append(lab,sw); return row;
  }
  function sectionHead(title, desc) {
    const head=E('div','card-head divided'); const wrap=E('div'); wrap.append(E('h2','',title),E('p','',desc)); const actions=E('div','head-actions'); head.append(wrap,actions); return [head,actions];
  }
  function row(label, sub, valueNode) {
    const r=E('div','row'); const l=E('div','label',label); if(sub)l.append(E('span','',sub)); const v=E('div','value'); v.append(valueNode); r.append(l,v); return r;
  }
  function callout(bold, rest) {
    const c=E('p','callout'); c.append(E('b','',bold),document.createTextNode(rest)); return c;
  }
  function toast(message,bad=false) {
    q('#voice-call-toast')?.remove();
    const b=E('div','banner '+(bad?'bad':'ok')); b.id='voice-call-toast'; b.append(E('span','',message));
    q('.content')?.prepend(b);
    if(bad){ const x=btn('Dismiss'); x.addEventListener('click',()=>b.remove()); b.append(x); }
    else setTimeout(()=>b.remove(),4000);
    b.scrollIntoView({block:'nearest'});
  }
  async function voiceFetch(path, options={}) {
    const r=await fetch(VOICE+path,Object.assign({cache:'no-store'},options));
    if(!r.ok) throw new Error((await r.text()).trim()||('Voice service returned '+r.status));
    if(r.status===204) return null;
    return r.json();
  }
  const post=(path,body)=>voiceFetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body||{})});
  async function refresh(){ snapshot=await voiceFetch('/status'); return snapshot; }

  // Which of the several things went wrong, in the words for that one. This
  // used to be guessed from the note's wording, and a machine with no cables at
  // all was told the desktop app was being waited for -- sending the user to
  // look at the app when the app was not the problem.
  function routingPill(rt){
    switch(rt?.routingState){
      case 'applied': return pill('Applied','ok');
      case 'no-cables': return pill('No cable to route to','warn');
      case 'waiting-for-app': return pill('Waiting for the desktop app','warn');
      case 'refused': return pill('Windows refused it','warn');
    }
    return pill('Not applied yet','warn');
  }

  // A call that was refused, or one FlipAi could not manage to answer, used to
  // leave the page reading "Idle" and nothing else -- the same screen as a call
  // that never happened. This is the one row that says which.
  function lastCallPill(rt){
    const outcome=rt?.lastCallOutcome;
    if(!outcome) return pill('None yet');
    const bad=/not allowed|could not|did not|has not|not bridged|no agent/i.test(outcome);
    return pill(bad?'Did not connect':'Connected',bad?'warn':'ok');
  }

  function cablesPill(){
    const audio=snapshot?.audio||{};
    if(audio.warning&&!(audio.cables||[]).length) return pill('Not found','warn');
    if(audio.warning) return pill('One of two','warn');
    return pill((audio.cables||[]).join(' + ')||'Wired','ok');
  }

  // The row that reports a missing cable is where the user is looking, so it is
  // where the way to fix it belongs. A button further down the page is a button
  // that does not get found.
  function cablesCell(){
    const wrap=E('span'); wrap.append(cablesPill());
    const audio=snapshot?.audio||{};
    const missing=(audio.cables||[]).length<2;
    if(!missing||typeof globalThis.__flipaiInstallAudioBridge!=='function') return wrap;
    const b=E('button','btn small accent'); b.type='button'; b.style.marginLeft='8px';
    b.textContent=(audio.cables||[]).length?'Get the second':'Set up';
    // FlipAi cannot install the driver itself: Windows loads a virtual audio
    // driver only when Microsoft signed it. The button opens the free,
    // properly signed one instead. Saying "Install" here would be the same
    // promise that produced problem code 52.
    b.title='Opens the free virtual audio cable FlipAi needs. Install it, restart, and FlipAi wires both directions itself.';
    b.addEventListener('click',async()=>{
      const label=b.textContent; b.disabled=true; b.textContent='Opening...';
      try{ await globalThis.__flipaiInstallAudioBridge(); }
      catch(e){ toast(e.message||String(e),true); }
      finally{ b.disabled=false; b.textContent=label; }
    });
    wrap.append(b);
    return wrap;
  }

  /* ---------- the embedded Google Voice panel (Connections) ----------
     Google Voice is FlipAi's own browser view, kept alive by FlipAi in its own
     process so it stays signed in and listening with the FlipAi window closed.
     While Connections is open, that view stands inside the panel below; the
     rest of the time it is parked off-screen. There is no state in which it is
     a window of its own on the desktop. */
  let dockTimer=0;
  let lastDockJSON='';
  let desiredDockJSON='';
  let dockStableSince=0;
  const hiddenDock={visible:false,x:0,y:0,width:0,height:0};

  function panelRect(){
    const el=q('#gv-embed-slot');
    if(!el) return null;
    const r=el.getBoundingClientRect();
    // A native window cannot be clipped by HTML, so it is clipped here: Google
    // Voice is docked to the part of the reserved panel that is really on
    // screen, and to nothing else.
    //
    // This used to refuse to dock at all unless the whole panel fitted in the
    // viewport, which is why Google Voice would simply not be there on a
    // smaller FlipAi window, at a higher display scale, or with the page
    // scrolled by a few pixels -- the one symptom nobody could explain,
    // because the panel looked exactly the same as when it was starting up.
    const left=Math.max(r.left,0);
    const top=Math.max(r.top,0);
    const right=Math.min(r.right,innerWidth);
    const bottom=Math.min(r.bottom,innerHeight);
    const width=right-left;
    const height=bottom-top;
    // Below this there is not enough of the panel on screen to be worth
    // showing a browser in, and the window is withdrawn instead.
    if(width<160||height<160) return null;
    const dpr=devicePixelRatio||1;
    return {
      visible:true,
      x:Math.round(left*dpr),
      y:Math.round(top*dpr),
      width:Math.round(width*dpr),
      height:Math.round(height*dpr)
    };
  }

  async function reportDock(force){
    if(!snapshot) return;
    const want=!document.hidden?panelRect():null;
    const body=want||hiddenDock;
    const raw=JSON.stringify(body);
    if(raw!==desiredDockJSON){
      desiredDockJSON=raw;
      dockStableSince=Date.now();
    }
    // The panel follows the page rather than being withdrawn while the page
    // moves. Withdrawing it during a scroll is what made Google Voice blink
    // out every time the user touched the wheel; the window is clipped to the
    // visible part of the panel above, so tracking it is safe.
    if(!force&&raw===lastDockJSON) return;
    lastDockJSON=raw;
    try{ await post('/dock',body); }catch(_){}
  }

  function leavingPage(){ flushSave(); withdrawPanel(); }
  function startDockReporting(){
    if(dockTimer) return;
    dockTimer=setInterval(()=>reportDock(true),250);
    addEventListener('pagehide',leavingPage);
    addEventListener('beforeunload',leavingPage);
    document.addEventListener('visibilitychange',()=>{ if(document.hidden){ flushSave(); reportDock(true); } });
  }
  function withdrawPanel(){
    if(dockTimer){clearInterval(dockTimer);dockTimer=0;}
    lastDockJSON=''; desiredDockJSON=''; dockStableSince=0;
    const body=JSON.stringify(hiddenDock);
    try{
      if(navigator.sendBeacon&&navigator.sendBeacon(VOICE+'/dock',new Blob([body],{type:'text/plain'}))) return;
    }catch(_){}
    post('/dock',hiddenDock).catch(()=>{});
  }

  function embedStyle(){
    if(q('#gv-embed-style')) return;
    const st=E('style'); st.id='gv-embed-style';
    st.textContent=[
      '#gv-embed-slot{position:relative;height:620px;min-height:420px;border-radius:16px;border:1px solid var(--line,#e7e4ee);background:#0f0d15;overflow:hidden}',
      '#gv-embed-slot .gv-empty{position:absolute;inset:0;display:flex;flex-direction:column;gap:10px;align-items:center;justify-content:center;text-align:center;padding:28px;color:#d8d3e6;background:#151221}',
      '#gv-embed-slot .gv-empty b{font-size:15px;color:#fff}',
      '#gv-embed-slot .gv-empty span{max-width:520px;font-size:13px;line-height:1.5;color:#b3abc7}',
      '#gv-embed-slot .gv-empty .gv-why{max-width:560px;font-size:12px;line-height:1.5;color:#8f87a8;font-family:ui-monospace,Consolas,monospace;white-space:pre-wrap}',
      '#gv-embed-slot .gv-empty .gv-acts{display:flex;gap:8px;flex-wrap:wrap;justify-content:center;margin-top:4px}',
      '#gv-embed-slot .gv-empty button{cursor:pointer;border-radius:10px;border:1px solid #3a3350;background:#241f36;color:#efe9ff;font:inherit;font-size:13px;padding:8px 14px}',
      '#gv-embed-slot .gv-empty button:hover{background:#2f2846}',
      '#gv-embed-slot .gv-empty button[disabled]{opacity:.6;cursor:default}',
      '.gv-embed-wrap{margin:6px 0 4px}',
      '.gv-embed-note{display:flex;justify-content:space-between;gap:12px;align-items:center;margin:8px 0 0}',
      '.gv-flow{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:6px 0 2px}',
      '.gv-flow span{font-size:12px;background:var(--chip,#f3f0fa);border:1px solid var(--line,#e7e4ee);border-radius:999px;padding:5px 10px}',
      '.gv-flow i{font-style:normal;color:#8b83a3}'
    ].join('\n');
    document.head.append(st);
  }

  /* ---------- saving (Settings) ---------- */
  let saveTimer=0;
  let savePending=false;
  function collectConfig(){
    const next=JSON.parse(JSON.stringify(snapshot.config));
    next.defaultAgent=value('vc-default');
    next.codex=Object.assign({},next.codex,{appTitle:value('vcc-title'),voiceShortcut:value('vcc-shortcut'),appCommand:value('vcc-command')});
    next.claude=Object.assign({},next.claude,{appTitle:value('vca-title'),voiceShortcut:value('vca-shortcut'),appCommand:value('vca-command')});
    return next;
  }
  function markSaving(text){ const s=q('#vc-saved'); if(s){ s.textContent=text; } }
  async function saveNow(){
    clearTimeout(saveTimer); saveTimer=0; savePending=false;
    try{ snapshot=await post('/config',collectConfig()); markSaving('Saved'); updateStatusRows(); }
    catch(e){ markSaving('Not saved'); toast(e.message,true); }
  }
  function scheduleSave(){ markSaving('Saving...'); savePending=true; clearTimeout(saveTimer); saveTimer=setTimeout(saveNow,500); }
  function flushSave(){
    if(!savePending) return;
    savePending=false; clearTimeout(saveTimer); saveTimer=0;
    let body; try{ body=JSON.stringify(collectConfig()); }catch(_){ return; }
    try{ if(navigator.sendBeacon&&navigator.sendBeacon(VOICE+'/config',new Blob([body],{type:'text/plain'}))) return; }catch(_){}
    try{ fetch(VOICE+'/config',{method:'POST',headers:{'Content-Type':'application/json'},body:body,keepalive:true}); }catch(_){}
  }
  function autosave(node){ node.addEventListener('change',scheduleSave); if(node.tagName==='INPUT'&&node.type!=='checkbox') node.addEventListener('input',scheduleSave); return node; }

  /* ---------- shared status ---------- */
  // A call is not one thing. Reporting "connected" for a call whose agent
  // never entered voice mode is exactly how a caller could be told everything
  // was fine while hearing silence, so each stage says what it is.
  function callPill(rt){
    switch(rt?.callPhase){
      case 'ringing': return pill('Answering a call','ok');
      case 'refused': return pill('Ringing — caller not allowed','warn');
      case 'connecting': return pill('Answered — starting voice','warn');
      case 'live': return pill(rt.caller?('Talking to '+rt.caller):'Talking','ok');
      case 'unbridged': return pill('Answered by hand — not bridged','warn');
    }
    return null;
  }
  function runtimePill(rt){
    const call=callPill(rt);
    if(call) return call;
    if(!rt?.browserRunning) return pill('Not running','warn');
    return rt.signedIn?pill('Listening for calls','ok'):pill('Sign in to Google Voice','warn');
  }

  function serviceErrorCard(err){
    q('#voice-call-unavailable')?.remove();
    const card=E('section','card'); card.id='voice-call-unavailable';
    const [head,actions]=sectionHead('Google Voice','Phone-to-agent calling is installed, but the local voice service could not be reached.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Needs attention','warn'));
    const retry=btn('Retry','btn accent'); actions.append(retry); card.append(head);
    const body=E('div','card-body');
    const c=E('p','callout'); c.append(E('b','','Voice service error: '),document.createTextNode(err?.message||String(err||'unknown error'))); body.append(c);
    body.append(E('p','hint','Restart FlipAi first. The calling controls will reappear automatically when the local voice component is available. SMS is unaffected.'));
    card.append(body); q('.content')?.append(card); retry.addEventListener('click',()=>location.reload());
  }

  /* ---------- the Connections preview panel ---------- */
  function panelState(){
    const rt=snapshot.runtime||{},cfg=snapshot.config||{};
    if(rt.docked&&rt.browserRunning) return null;
    if(!cfg.enabled) return {title:'Calling is off',detail:'Turn on Google Voice calling under Settings and the live Google Voice browser appears here.'};
    if(!snapshot.webView2) return {title:'Windows is missing the component that shows Google Voice',detail:'FlipAi draws Google Voice with the Microsoft Edge WebView2 Runtime, the same component it draws itself with. Install or repair it, then press Retry.',retry:true};
    if(rt.lastOpenError) return {title:'Google Voice could not start',why:rt.lastOpenError,retry:true,detail:'Nothing else in FlipAi is affected — texts and Gmail routing carry on.'};
    if(!rt.browserRunning) return {title:'Starting Google Voice...',why:rt.lastOpen||'',detail:'FlipAi is starting the persistent Google Voice receiver. If it stays here, press Retry.',retry:true};
    if(!rt.docked) return {title:'Google Voice is listening in the background',why:rt.dockBlocked||'',detail:'Scroll this panel into view and Google Voice appears here. It stays signed in and keeps taking calls while it is out of sight.',retry:true};
    return {title:'Google Voice is loading...',detail:'The receiver is running; waiting for it to paint in this panel.'};
  }

  function renderPanel(){
    const empty=q('#gv-embed-empty'); if(!empty) return;
    const state=panelState(); if(!state){ empty.style.display='none'; return; }
    empty.style.display='flex'; empty.textContent=''; empty.append(E('b','',state.title));
    if(state.detail) empty.append(E('span','',state.detail));
    if(state.why) empty.append(E('p','gv-why',state.why));
    if(state.retry){
      const acts=E('div','gv-acts');
      const r=E('button','','Retry Google Voice'); r.type='button';
      r.addEventListener('click',async()=>{
        const label=r.textContent; r.disabled=true; r.textContent='Restarting...';
        try{ snapshot=await post('/restart'); updateStatusRows(); } catch(e){ toast(e.message,true); }
        finally{ r.disabled=false; r.textContent=label; }
      });
      acts.append(r); empty.append(acts);
    }
  }

  function updateStatusRows(){
    const rt=snapshot.runtime||{},cfg=snapshot.config||{};
    const set=(id,node)=>{ const el=q('#'+id); if(!el)return; el.textContent=''; el.append(node); };
    set('vcs-state',cfg.enabled?pill('On — answering calls','ok'):pill('Off','warn'));
    set('vcs-window',runtimePill(rt));
    set('vcs-google',rt.browserRunning?(rt.signedIn?pill('Signed in','ok'):pill('Not signed in','warn')):pill('Window not running','warn'));
    set('vcs-cables',cablesCell());
    set('vcs-audio',snapshot.audioWarning?pill('Needs attention','warn'):pill('Ready — silent and virtual','ok'));
    set('vcs-routing',routingPill(rt));
    set('vcs-agents',(snapshot.callAgents||[]).length?pill((snapshot.callAgents||[]).join(' and '),'ok'):pill('Nobody yet','warn'));
    set('vcs-call',callPill(rt)||pill('Idle'));
    set('vcs-ring',rt.lastRingAt&&!/^0001/.test(rt.lastRingAt)?pill(new Date(rt.lastRingAt).toLocaleString(),'ok'):pill('Never','warn'));
    set('vcs-lastcall',lastCallPill(rt));
    set('vcs-webview2',snapshot.webView2?pill(snapshot.webView2,'ok'):pill('Not installed','warn'));
    set('vcs-permissions',pill('Mic + notifications allowed','ok'));
    const sw=q('#vc-enabled'); if(sw) sw.checked=!!cfg.enabled;
    renderPanel();
    const problems=q('#vc-problems'); if(problems){ problems.textContent=''; for(const node of problemNodes()) problems.append(node); }
  }

  function problemNodes(){
    const out=[],rt=snapshot.runtime||{},cfg=snapshot.config||{};
    if(!(snapshot.callAgents||[]).length) out.push(callout('No agent can take a call yet. ','Open the Agents page, add your phone number under the agent you want to talk to, and set that number to "Texts and calls" or "Calls only".'));
    if(snapshot.audioWarning) out.push(callout('Audio path: ',snapshot.audioWarning));
    // The missing cable already has its own row and its own Install button; do
    // not repeat it here as if it were a second, different problem.
    if(rt.routingNote&&rt.routingState&&rt.routingState!=='applied'&&rt.routingState!=='no-cables') out.push(callout('Desktop app audio: ',rt.routingNote));
    if(cfg.enabled&&rt.browserRunning&&(!rt.lastRingAt||/^0001/.test(rt.lastRingAt))) out.push(callout('No call has ever rung here. ','In Google Voice itself, open Settings → Calls and make sure receiving calls on this device is on.'));
    // Google Voice announced a call but FlipAi never found a control to press.
    // That is a different failure from "not allowed" and from "never rang", and
    // without saying so it looks like FlipAi simply did nothing.
    const ringSeen=rt.lastRingAt&&!/^0001/.test(rt.lastRingAt)&&(Date.now()-new Date(rt.lastRingAt).getTime())<120000;
    const announced=/\[notification:/.test(rt.controls||'');
    if(announced&&ringSeen&&(!rt.callPhase||rt.callPhase==='idle')){
      out.push(callout('A call reached this window but FlipAi found nothing to press. ','Google Voice announced an incoming call and never drew an Answer control in the page. Open Connections while a call comes in to see what it is showing, and check Google Voice\u2019s own Settings \u2192 Calls for receiving calls on this device.'));
    }
    if(rt.callNote) out.push(callout('This call: ',rt.callNote));
    // And once it is over, what happened to it -- with what FlipAi tried, in
    // order, so a call that was not answered can be told apart from a call that
    // was never allowed and from one that never rang.
    if(!rt.callNote&&rt.lastCallOutcome){
      const when=rt.lastCallAt&&!/^0001/.test(rt.lastCallAt)?new Date(rt.lastCallAt).toLocaleString()+': ':'';
      out.push(callout('Last call \u2014 '+when,rt.lastCallOutcome+(rt.lastCallTrace?('  FlipAi tried: '+rt.lastCallTrace):'')));
    }
    if(rt.blocked&&!rt.callNote) out.push(callout('Last call was not connected: ',rt.blocked));
    if(rt.lastError&&!rt.lastOpenError){
      // A desktop app that would not enter voice mode is not a Google Voice
      // problem, and labelling it as one sends the user to the wrong place.
      const agentProblem=/desktop voice session|voice mode|Voice control|desktop app/i.test(rt.lastError);
      out.push(callout(agentProblem?'Desktop app: ':'Google Voice window: ',rt.lastError));
    }
    return out;
  }

  /* ---------- the Settings card ---------- */
  function actionButton(b, doIt, busyLabel){
    b.addEventListener('click',async()=>{
      const label=b.textContent; b.disabled=true; b.textContent=busyLabel||'Working...';
      try{ await doIt(); } catch(e){ toast(e.message,true); }
      finally{ b.disabled=false; b.textContent=label; try{ await refresh(); updateStatusRows(); }catch(_){} }
    });
    return b;
  }

  function settingsCard(){
    const cfg=snapshot.config,card=E('section','card'); card.id='voice-call-card';
    const [head,actions]=sectionHead('Google Voice calling','FlipAi answers calls to your Google Voice number and puts the caller through to the ChatGPT/Codex desktop app’s voice mode — the same agent that can work on this PC.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Experimental','brand'));
    const saved=E('span','hint','Changes save as you make them'); saved.id='vc-saved'; actions.append(saved); card.append(head);
    const body=E('div','card-body');

    const sw=toggle('vc-enabled','Answer phone calls with an agent','When an allowed number calls, FlipAi automatically picks up Google Voice and starts the selected desktop app’s voice mode. There is no separate auto-answer setting.',cfg.enabled);
    body.append(sw);
    q('input',sw).addEventListener('change',async(e)=>{
      const want=e.target.checked; e.target.disabled=true; markSaving('Saving...');
      try{ snapshot=await post('/enable',{enabled:want}); markSaving('Saved'); updateStatusRows(); }
      catch(err){ e.target.checked=!want; markSaving('Not saved'); toast(err.message,true); }
      finally{ e.target.disabled=false; }
    });

    body.append(E('div','section-label','Set up'));
    body.append(E('p','hint','Google Voice stays signed in and listening in the background. Open Connections to see and use the same persistent Google Voice session.'));
    const signin=btn('Open Google Voice','btn'); signin.id='vc-signin';
    signin.addEventListener('click',()=>{ location.href='/connections'; });
    const signout=btn('Sign out','btn'); signout.id='vc-signout';
    actionButton(signout,async()=>{
      if(!confirm('Sign out of Google Voice? Calls stop being answered until you sign in again.')) return;
      snapshot=await post('/signout'); updateStatusRows(); toast('Signed out. Open Connections when you are ready to sign in again.');
    },'Signing out...');
    body.append(row('Google Voice account','The persistent Google Voice session FlipAi uses for incoming and outgoing calls.',(()=>{const w=E('div');w.append(signin,document.createTextNode(' '),signout);return w;})()));

    body.append(E('div','section-label','Status'));
    const rows=E('div','rows'); const cell=(id)=>{ const v=E('span'); v.id=id; return v; };
    rows.append(row('Calling','Whether FlipAi answers the phone at all.',cell('vcs-state')));
    rows.append(row('Google Voice window','Kept running in the background by FlipAi.',cell('vcs-window')));
    rows.append(row('Google account','Signed-in state of the Google Voice receiver.',cell('vcs-google')));
    rows.append(row('Virtual audio cables','The internal two-way audio path used between Google Voice and the desktop agent.',cell('vcs-cables')));
    rows.append(row('Audio path','Silent on this PC’s speakers; no real microphone involved.',cell('vcs-audio')));
    rows.append(row('Desktop app audio','The desktop app’s microphone and speaker route.',cell('vcs-routing')));
    rows.append(row('Agents on calls','Set by giving an agent a number that may call, on the Agents page.',cell('vcs-agents')));
    rows.append(row('Current call','',cell('vcs-call')));
    rows.append(row('Ring seen','Whether a call has reached Google Voice.',cell('vcs-ring')));
    rows.append(row('Last call','What happened to the most recent call, kept after it ends.',cell('vcs-lastcall')));
    rows.append(row('Edge WebView2 runtime','The Windows component FlipAi draws both its own window and Google Voice with.',cell('vcs-webview2')));
    rows.append(row('Browser permissions','Microphone and notifications for Google Voice are granted by FlipAi.',cell('vcs-permissions')));
    body.append(rows);

    const problems=E('div'); problems.id='vc-problems'; body.append(problems);

    body.append(E('div','section-label','Desktop apps'));
    body.append(field('Default voice agent',autosave(select('vc-default',[['C','ChatGPT / Codex desktop app'],['A','Claude Desktop']],cfg.defaultAgent)),'Normally the caller’s agent allowlist decides.'));
    for(const agent of ['C','A']){
      const isClaude=agent==='A', own=(isClaude?cfg.claude:cfg.codex)||{}, p=isClaude?'vca':'vcc';
      body.append(E('p','hint',(isClaude?'Claude Desktop':'ChatGPT / Codex desktop app')+' — the caller talks to this app’s voice mode.'));
      const g=E('div','grid-2');
      g.append(field('Desktop window title contains',autosave(input(p+'-title',own.appTitle,isClaude?'Claude':'ChatGPT')),'FlipAi uses it to find the app when a call connects.'));
      g.append(field('Voice shortcut (optional)',autosave(input(p+'-shortcut',own.voiceShortcut,'Ctrl+Shift+V')),'If blank, FlipAi uses the app’s accessible Voice control.'));
      body.append(g);
      body.append(field('Launch command (optional)',autosave(input(p+'-command',own.appCommand,'Path or app command')),'Used only if the desktop window is not already open.'));
      const test=btn('Start '+(isClaude?'Claude':'ChatGPT')+' voice test');
      actionButton(test,async()=>{ await post('/test-agent?agent='+agent); toast((isClaude?'Claude':'ChatGPT')+' voice start requested.'); },'Testing...');
      body.append(test);
    }

    body.append(E('p','hint','Who may call is set on the Agents page. A number allowed under one agent cannot reach the other.'));
    card.append(body); q('.content')?.append(card);
    addEventListener('pagehide',flushSave); addEventListener('beforeunload',flushSave);
    document.addEventListener('visibilitychange',()=>{ if(document.hidden) flushSave(); });
    updateStatusRows();
    setInterval(async()=>{ try{ await refresh(); updateStatusRows(); }catch(_){} },4000);
  }

  /* ---------- the Connections card ---------- */
  function connectionsCard(){
    embedStyle();
    const card=E('section','card'); card.id='voice-preview-card';
    const [head,actions]=sectionHead('Google Voice','Your persistent Google Voice session. It stays signed in and listening even when you leave Connections; this page only shows or hides the live view.');
    head.querySelector('h2').append(document.createTextNode(' '),cellSpan('vcs-window'));
    const setup=btn('Set up in Settings'); setup.id='vc-open-settings'; setup.addEventListener('click',()=>{ location.href='/settings'; });
    actions.append(setup); card.append(head);

    const body=E('div','card-body');
    const wrap=E('div','gv-embed-wrap');
    const slot=E('div'); slot.id='gv-embed-slot';
    const empty=E('div','gv-empty'); empty.id='gv-embed-empty'; slot.append(empty); wrap.append(slot);
    const note=E('div','gv-embed-note');
    note.append(E('p','hint','Google Voice remains alive in the background on every FlipAi tab. When this full panel is visible it is shown here; while you scroll or leave Connections, the native window is hidden instead of floating over the app.'));
    wrap.append(note); body.append(wrap); card.append(body); q('.content')?.append(card);

    updateStatusRows(); startDockReporting(); reportDock(true);
    setInterval(async()=>{ try{ await refresh(); updateStatusRows(); }catch(_){} },4000);
  }
  function cellSpan(id){ const v=E('span'); v.id=id; return v; }

  async function install(){
    if(!globalThis.__flipaiDesktop&&!document.documentElement?.dataset.flipaiDesktop)return;
    const here=location.pathname;
    if(here!=='/connections'&&here!=='/settings'){
      post('/dock',hiddenDock).catch(()=>{});
      return;
    }
    try{await refresh()}catch(e){serviceErrorCard(e);return}
    q('#voice-call-unavailable')?.remove();
    if(here==='/settings') settingsCard(); else connectionsCard();
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',install,{once:true});else install();
})();
`