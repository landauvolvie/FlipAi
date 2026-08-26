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
  let poppedOut = false;

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
    // A failure explains what to do next and can be several lines long, so it
    // stays until it is dismissed. Only the success note times itself out.
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

  function cablesPill(){
    const audio=snapshot?.audio||{};
    if(audio.warning&&!(audio.cables||[]).length) return pill('Not found','warn');
    if(audio.warning) return pill('One of two','warn');
    return pill((audio.cables||[]).join(' + ')||'Wired','ok');
  }

  /* ---------- the embedded Google Voice panel (Connections) ----------
     Google Voice runs in its own window so it can stay signed in and listening
     with FlipAi closed. That window is placed over the empty panel below and
     stripped of its frame, so what the user sees is Google Voice inside the
     app rather than a second window that appeared on its own. The rectangle is
     reported continuously; the moment this page stops reporting it, the window
     goes back to running quietly in the background -- still answering calls. */
  let dockTimer=0;
  function panelRect(){
    const el=q('#gv-embed-slot');
    if(!el) return null;
    const r=el.getBoundingClientRect();
    // Only the part of the panel that is really on screen is reported. The
    // card is taller than the window, so a panel scrolled half out of view
    // would otherwise hang the Google Voice window over the page above it.
    const left=Math.max(r.left,0), top=Math.max(r.top,0);
    const right=Math.min(r.right,innerWidth), bottom=Math.min(r.bottom,innerHeight);
    if(right-left<160||bottom-top<160) return null;
    const dpr=devicePixelRatio||1;
    // Reported against the page's own viewport, in physical pixels. FlipAi
    // turns that into a screen position from its own window, so nothing here
    // has to guess where a title bar or a display scale put the page.
    return {
      visible:true,
      x:Math.round(left*dpr),
      y:Math.round(top*dpr),
      width:Math.round((right-left)*dpr),
      height:Math.round((bottom-top)*dpr)
    };
  }
  let lastDockJSON='';
  async function reportDock(force){
    if(!snapshot) return;
    let want=null;
    if(!poppedOut && !document.hidden) want=panelRect();
    const body=want||{visible:false,x:0,y:0,width:0,height:0};
    const raw=JSON.stringify(body);
    // The window has to be told repeatedly that the panel is still wanted, or
    // the dock expires; a hidden panel only needs saying once.
    if(!force && !body.visible && raw===lastDockJSON) return;
    lastDockJSON=raw;
    try{ await post('/dock',body); }catch(_){}
  }
  // Leaving the page is not popping the window out: the panel is withdrawn,
  // but nothing about this page's next visit is decided here.
  function leavingPage(){ flushSave(); withdrawPanel(); }
  function startDockReporting(){
    if(dockTimer) return;
    dockTimer=setInterval(()=>reportDock(true),250);
    addEventListener('pagehide',leavingPage);
    addEventListener('beforeunload',leavingPage);
    document.addEventListener('visibilitychange',()=>{ if(document.hidden){ flushSave(); reportDock(true); } });
  }
  // withdrawPanel takes the Google Voice window off this page. Leaving the page
  // is the one moment a plain fetch cannot be relied on -- the navigation
  // cancels it -- and the panel would then stand over the app until its own
  // timeout expired, which is a visible second of Google Voice sitting on top
  // of whatever the user opened next. A beacon is handed to the browser to
  // deliver instead, exactly as a pending save is.
  function withdrawPanel(){
    if(dockTimer){clearInterval(dockTimer);dockTimer=0;}
    lastDockJSON='';
    const body=JSON.stringify({visible:false,x:0,y:0,width:0,height:0});
    try{
      if(navigator.sendBeacon && navigator.sendBeacon(VOICE+'/dock',new Blob([body],{type:'text/plain'}))) return;
    }catch(_){}
    post('/dock',{visible:false,x:0,y:0,width:0,height:0}).catch(()=>{});
  }
  function stopDockReporting(){
    poppedOut=true;
    withdrawPanel();
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
    // Start from what the server has, and only overwrite what this card
    // actually edits. Fields the card does not carry -- the hand-edit-only
    // audio overrides among them -- must survive a save untouched.
    const next=JSON.parse(JSON.stringify(snapshot.config));
    next.defaultAgent=value('vc-default');
    next.codex=Object.assign({},next.codex,{
      appTitle:value('vcc-title'),
      voiceShortcut:value('vcc-shortcut'),
      appCommand:value('vcc-command')
    });
    next.claude=Object.assign({},next.claude,{
      appTitle:value('vca-title'),
      voiceShortcut:value('vca-shortcut'),
      appCommand:value('vca-command')
    });
    return next;
  }
  function markSaving(text){ const s=q('#vc-saved'); if(s){ s.textContent=text; } }
  async function saveNow(){
    clearTimeout(saveTimer); saveTimer=0; savePending=false;
    try{
      snapshot=await post('/config',collectConfig());
      markSaving('Saved');
      updateStatusRows();
    }catch(e){ markSaving('Not saved'); toast(e.message,true); }
  }
  function scheduleSave(){
    markSaving('Saving...');
    savePending=true;
    clearTimeout(saveTimer);
    saveTimer=setTimeout(saveNow,500);
  }
  // A change made and then navigated away from within the debounce would be
  // lost, silently, while the card promises that changes save as they are made.
  // The page is going away here, so fetch cannot be relied on to finish: a
  // beacon is handed to the browser to deliver instead. It is sent as plain
  // text on purpose, which keeps it a request the browser sends immediately
  // rather than one it must ask permission for first; the endpoint reads the
  // body as JSON either way.
  function flushSave(){
    if(!savePending) return;
    savePending=false;
    clearTimeout(saveTimer); saveTimer=0;
    let body;
    try{ body=JSON.stringify(collectConfig()); }catch(_){ return; }
    try{
      if(navigator.sendBeacon && navigator.sendBeacon(VOICE+'/config',new Blob([body],{type:'text/plain'}))) return;
    }catch(_){}
    try{ fetch(VOICE+'/config',{method:'POST',headers:{'Content-Type':'application/json'},body:body,keepalive:true}); }catch(_){}
  }
  function autosave(node){
    node.addEventListener('change',scheduleSave);
    if(node.tagName==='INPUT'&&node.type!=='checkbox') node.addEventListener('input',scheduleSave);
    return node;
  }

  /* ---------- shared status ---------- */

  function runtimePill(rt){
    if(rt?.inCall) return pill('On a call','ok');
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
    card.append(body);
    q('.content')?.append(card);
    retry.addEventListener('click',()=>location.reload());
  }

  /* ---------- the Connections preview panel ---------- */

  // panelState answers the only question the dark panel has ever been asked:
  // why is Google Voice not in it? Everything below is a state FlipAi actually
  // knows it is in, and each one that a person can do something about says
  // what.
  function panelState(){
    const rt=snapshot.runtime||{},cfg=snapshot.config||{};
    if(rt.docked&&rt.browserRunning) return null;
    // The user's own switch comes first: with calling off, nothing is being
    // attempted, so nothing else is the reason yet.
    if(!cfg.enabled){
      return {title:'Calling is off',
        detail:'Turn on Google Voice calling under Settings and the live Google Voice browser appears here.'};
    }
    if(!snapshot.webView2){
      return {title:'Windows is missing the component that shows Google Voice',
        detail:'FlipAi draws Google Voice with the Microsoft Edge WebView2 Runtime, which is not installed on this PC. Microsoft distributes it free as the Evergreen Standalone Installer. Install it, then press Retry.',
        retry:true};
    }
    if(rt.lastOpenError){
      return {title:'Google Voice could not start', why:rt.lastOpenError, retry:true, popOut:true,
        detail:'Nothing else in FlipAi is affected — texts and Gmail routing carry on.'};
    }
    if(!rt.browserRunning){
      return {title:'Starting Google Voice...', why:rt.lastOpen||'',
        detail:'The first run has to unpack Microsoft WebView2, which can take a minute. If it stays like this, press Retry.',
        retry:true};
    }
    if(!rt.docked){
      return {title:'Google Voice is running, but not in this panel',
        why:rt.dockBlocked||rt.lastOpen||'',
        detail:'It is signed in and it will still answer calls. Reload this page to try placing it here again, or open it in its own window.',
        reload:true, popOut:true, retry:true};
    }
    return {title:'Google Voice is loading...', detail:'The window is running; waiting for it to appear in this panel.'};
  }

  function renderPanel(){
    const empty=q('#gv-embed-empty');
    if(!empty) return;
    const state=panelState();
    if(!state){ empty.style.display='none'; return; }
    empty.style.display='flex';
    empty.textContent='';
    empty.append(E('b','',state.title));
    if(state.detail) empty.append(E('span','',state.detail));
    if(state.why) empty.append(E('p','gv-why',state.why));
    if(state.retry||state.popOut){
      const acts=E('div','gv-acts');
      if(state.retry){
        const r=E('button','','Retry, drawing it another way'); r.type='button';
        r.addEventListener('click',async()=>{
          const label=r.textContent;
          r.disabled=true; r.textContent='Restarting...';
          try{ snapshot=await post('/restart'); updateStatusRows(); }
          catch(e){ toast(e.message,true); }
          finally{ r.disabled=false; r.textContent=label; }
        });
        acts.append(r);
      }
      if(state.reload){
        const rl=E('button','','Put it back in this panel'); rl.type='button';
        rl.addEventListener('click',()=>location.reload());
        acts.append(rl);
      }
      if(state.popOut){
        const o=E('button','','Open in its own window'); o.type='button';
        o.addEventListener('click',()=>q('#vc-pop-out')?.click());
        acts.append(o);
      }
      empty.append(acts);
    }
  }

  // updateStatusRows redraws only the parts that change on their own, so a
  // control the user is in the middle of using is never rebuilt underneath
  // them. It tolerates either card: rows that are not on this page are skipped.
  function updateStatusRows(){
    const rt=snapshot.runtime||{},cfg=snapshot.config||{};
    const set=(id,node)=>{ const el=q('#'+id); if(!el)return; el.textContent=''; el.append(node); };
    set('vcs-state',cfg.enabled?pill('On — answering calls','ok'):pill('Off','warn'));
    set('vcs-window',runtimePill(rt));
    set('vcs-google',rt.browserRunning?(rt.signedIn?pill('Signed in','ok'):pill('Not signed in','warn')):pill('Window not running','warn'));
    set('vcs-cables',cablesPill());
    set('vcs-audio',snapshot.audioWarning?pill('Needs attention','warn'):pill('Ready — silent and virtual','ok'));
    set('vcs-routing',/Applied automatically|is wired to the cables/.test(rt.routingNote||'')?pill('Applied','ok'):pill(rt.routingNote?'Waiting':'Not applied yet','warn'));
    set('vcs-agents',(snapshot.callAgents||[]).length?pill((snapshot.callAgents||[]).join(' and '),'ok'):pill('Nobody yet','warn'));
    set('vcs-call',rt.inCall?pill(rt.caller?('Connected — '+rt.caller):'Connected','ok'):pill('Idle'));
    set('vcs-ring',rt.lastRingAt&&!/^0001/.test(rt.lastRingAt)?pill(new Date(rt.lastRingAt).toLocaleString(),'ok'):pill('Never','warn'));
    set('vcs-webview2',snapshot.webView2?pill(snapshot.webView2,'ok'):pill('Not installed','warn'));
    set('vcs-permissions',pill('Handled by FlipAi','ok'));
    const sw=q('#vc-enabled'); if(sw) sw.checked=!!cfg.enabled;
    renderPanel();
    const problems=q('#vc-problems');
    if(problems){ problems.textContent=''; for(const node of problemNodes()) problems.append(node); }
  }

  function problemNodes(){
    const out=[],rt=snapshot.runtime||{},cfg=snapshot.config||{};
    if(!(snapshot.callAgents||[]).length){
      out.push(callout('No agent can take a call yet. ','Open the Agents page, add your phone number under the agent you want to talk to, and set that number to "Texts and calls" or "Calls only". A number allowed under one agent can never reach the other one.'));
    }
    if(snapshot.audioWarning) out.push(callout('Audio path: ',snapshot.audioWarning));
    if(rt.routingNote&&!/Applied automatically|is wired to the cables/.test(rt.routingNote)) out.push(callout('Desktop app audio: ',rt.routingNote));
    if(cfg.enabled&&rt.browserRunning&&(!rt.lastRingAt||/^0001/.test(rt.lastRingAt))){
      out.push(callout('No call has ever rung here. ','Google Voice only rings in a browser when you have switched that on in Google Voice itself: open the live view on Connections, then in Google Voice open Settings, then Calls, and turn on receiving calls on this device. Until then an incoming call goes to your forwarding phones and never reaches FlipAi.'));
    }
    if(rt.blocked) out.push(callout('Last call was not connected: ',rt.blocked));
    if(rt.lastError&&!rt.lastOpenError) out.push(callout('Google Voice window: ',rt.lastError));
    return out;
  }

  /* ---------- the Settings card ---------- */

  function actionButton(b, doIt, busyLabel){
    b.addEventListener('click',async()=>{
      const label=b.textContent;
      b.disabled=true; b.textContent=busyLabel||'Working...';
      try{ await doIt(); }
      catch(e){ toast(e.message,true); }
      finally{ b.disabled=false; b.textContent=label; try{ await refresh(); updateStatusRows(); }catch(_){} }
    });
    return b;
  }

  function settingsCard(){
    const cfg=snapshot.config,card=E('section','card'); card.id='voice-call-card';
    const [head,actions]=sectionHead('Google Voice calling','FlipAi answers calls to your Google Voice number and puts the caller through to the ChatGPT/Codex desktop app\u2019s voice mode \u2014 the same agent that can work on this PC. All setup lives here; the live Google Voice view is on Connections.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Experimental','brand'));
    const saved=E('span','hint','Changes save as you make them'); saved.id='vc-saved';
    actions.append(saved); card.append(head);

    const body=E('div','card-body');

    // 1. The one switch that matters. There is deliberately no separate
    //    auto-answer option: enabled means an authorized caller is answered,
    //    and an unauthorized one never is.
    const sw=toggle('vc-enabled','Answer phone calls with an agent',
      'FlipAi opens Google Voice at Windows sign-in, keeps it running while the PC is locked, and reopens it if it is ever closed. When an allowed number calls, FlipAi clicks Answer itself and starts the desktop app\u2019s voice mode; anyone else keeps ringing. Texts and Gmail routing are unchanged.',cfg.enabled);
    body.append(sw);
    q('input',sw).addEventListener('change',async(e)=>{
      const want=e.target.checked;
      e.target.disabled=true; markSaving('Saving...');
      try{ snapshot=await post('/enable',{enabled:want}); markSaving('Saved'); updateStatusRows(); }
      catch(err){ e.target.checked=!want; markSaving('Not saved'); toast(err.message,true); }
      finally{ e.target.disabled=false; }
    });

    // 2. Set up: one sign-in, one Google Voice setting, one driver install.
    body.append(E('div','section-label','Set up'));
    body.append(E('p','hint','Sign in to Google once, turn on receiving calls in Google Voice itself once, and have two virtual audio cables installed once (VB-CABLE A+B, or VoiceMeeter which includes two). FlipAi wires everything from there: Google Voice\u2019s microphone and speaker, and the desktop app\u2019s own audio, are all pointed at the cables automatically \u2014 nothing to select anywhere, nothing audible on your speakers, no real microphone in the path.'));
    const signin=btn('Sign in to Google Voice','btn'); signin.id='vc-signin';
    actionButton(signin,async()=>{ stopDockReporting(); await post('/open'); toast('The Google Voice window is open. Sign in to the Google account that owns your Voice number, then in Google Voice open Settings \u2192 Calls and turn on receiving calls on this device.'); },'Opening...');
    const signout=btn('Sign out','btn'); signout.id='vc-signout';
    actionButton(signout,async()=>{
      if(!confirm('Sign out of Google Voice? FlipAi forgets the saved browser session; calls stop being answered until you sign in again.')) return;
      snapshot=await post('/signout'); updateStatusRows();
      toast('Signed out. The Google Voice window restarts signed out; use Sign in when you are ready.');
    },'Signing out...');
    body.append(row('Google Voice account','The Google account whose Voice number FlipAi answers.',(()=>{const w=E('div');w.append(signin,document.createTextNode(' '),signout);return w;})()));

    // 3. Status and permission checks, live.
    body.append(E('div','section-label','Status'));
    const rows=E('div','rows');
    const cell=(id)=>{ const v=E('span'); v.id=id; return v; };
    rows.append(row('Calling','Whether FlipAi answers the phone at all.',cell('vcs-state')));
    rows.append(row('Google Voice window','Kept running in the background by FlipAi.',cell('vcs-window')));
    rows.append(row('Google account','Signed-in state of the Google Voice window.',cell('vcs-google')));
    rows.append(row('Virtual audio cables','Found automatically; each direction of the call uses its own cable.',cell('vcs-cables')));
    rows.append(row('Audio path','Silent on this PC\u2019s speakers; no real microphone involved.',cell('vcs-audio')));
    rows.append(row('Desktop app audio','The app\u2019s own microphone and speaker, pointed at the cables per-app by FlipAi.',cell('vcs-routing')));
    rows.append(row('Agents on calls','Set by giving an agent a number that may call, on the Agents page.',cell('vcs-agents')));
    rows.append(row('Current call','',cell('vcs-call')));
    rows.append(row('Ring seen','Whether a call has ever reached the Google Voice window.',cell('vcs-ring')));
    rows.append(row('Edge WebView2 runtime','Windows component FlipAi needs to show Google Voice.',cell('vcs-webview2')));
    rows.append(row('Browser permissions','Microphone, notifications, autoplay and pop-ups inside FlipAi\u2019s Google Voice window are granted by FlipAi itself; no Windows prompt ever needs answering.',cell('vcs-permissions')));
    body.append(rows);

    // 4. Everything that is currently wrong, in one place, kept live.
    const problems=E('div'); problems.id='vc-problems'; body.append(problems);

    // 5. The desktop apps a call is put through to.
    body.append(E('div','section-label','Desktop apps'));
    body.append(field('Default voice agent',autosave(select('vc-default',[['C','ChatGPT / Codex desktop app'],['A','Claude Desktop']],cfg.defaultAgent)),
      'Only used if a caller is somehow allowed under both agents. A phone number belongs to exactly one agent, so normally the number itself decides.'));
    for(const agent of ['C','A']){
      const isClaude=agent==='A', own=(isClaude?cfg.claude:cfg.codex)||{}, p=isClaude?'vca':'vcc';
      body.append(E('p','hint',(isClaude?'Claude Desktop':'ChatGPT / Codex desktop app')+' \u2014 the caller talks to this app\u2019s voice mode.'));
      const g=E('div','grid-2');
      g.append(field('Desktop window title contains',autosave(input(p+'-title',own.appTitle,isClaude?'Claude':'ChatGPT')),'FlipAi uses it to find the app when a call connects.'));
      g.append(field('Voice shortcut (optional)',autosave(input(p+'-shortcut',own.voiceShortcut,'Ctrl+Shift+V')),'The Voice shortcut set inside that app. If blank, FlipAi clicks its accessible Voice button instead.'));
      body.append(g);
      body.append(field('Launch command (optional)',autosave(input(p+'-command',own.appCommand,'Path or app command')),'Used only if the desktop window is not already open.'));
      const test=btn('Start '+(isClaude?'Claude':'ChatGPT')+' voice test');
      actionButton(test,async()=>{ await post('/test-agent?agent='+agent); toast((isClaude?'Claude':'ChatGPT')+' voice start requested \u2014 the app\u2019s voice mode should be listening now.'); },'Testing...');
      body.append(test);
    }

    body.append(E('p','hint','Who may call, and whether a number may call at all, is set with the agent it reaches on the Agents page. Each agent has its own list; a number allowed under one agent cannot reach the other.'));
    card.append(body);
    q('.content')?.append(card);

    // A change made and immediately navigated away from still has to land;
    // these are the moments the debounce cannot be trusted to finish.
    addEventListener('pagehide',flushSave);
    addEventListener('beforeunload',flushSave);
    document.addEventListener('visibilitychange',()=>{ if(document.hidden) flushSave(); });

    updateStatusRows();
    setInterval(async()=>{ try{ await refresh(); updateStatusRows(); }catch(_){} },4000);
  }

  /* ---------- the Connections card ---------- */

  function connectionsCard(){
    embedStyle();
    const card=E('section','card'); card.id='voice-preview-card';
    const [head,actions]=sectionHead('Google Voice','The real Google Voice, live, running in a window FlipAi owns and keeps alive. It stays signed in and keeps answering calls after you close this page or the whole app.');
    head.querySelector('h2').append(document.createTextNode(' '),cellSpan('vcs-window'));
    const popOut=btn('Open in its own window'); popOut.id='vc-pop-out';
    const setup=btn('Set up in Settings'); setup.id='vc-open-settings';
    setup.addEventListener('click',()=>{ location.href='/settings'; });
    actions.append(setup,popOut); card.append(head);

    const body=E('div','card-body');
    const wrap=E('div','gv-embed-wrap');
    const slot=E('div'); slot.id='gv-embed-slot';
    const empty=E('div','gv-empty'); empty.id='gv-embed-empty';
    slot.append(empty); wrap.append(slot);
    const note=E('div','gv-embed-note');
    note.append(E('p','hint','Closing or leaving this preview never stops Google Voice: the window only leaves the panel and keeps running in the background, detecting and answering incoming calls. Turning calling on and off, sign-in and sign-out, and all status checks live under Settings.'));
    wrap.append(note);
    body.append(wrap);
    card.append(body);
    q('.content')?.append(card);

    popOut.addEventListener('click',async()=>{
      const label=popOut.textContent;
      popOut.disabled=true; popOut.textContent='Opening...';
      stopDockReporting();
      try{ await post('/open'); toast('Google Voice is now a window of its own. Reload this page to put it back inside FlipAi.'); }
      catch(e){ toast(e.message,true); poppedOut=false; startDockReporting(); }
      finally{ popOut.disabled=false; popOut.textContent=label; }
    });

    updateStatusRows();
    startDockReporting();
    reportDock(true);
    setInterval(async()=>{ try{ await refresh(); updateStatusRows(); }catch(_){} },4000);
  }
  function cellSpan(id){ const v=E('span'); v.id=id; return v; }

  async function install(){
    if(!globalThis.__flipaiDesktop && !document.documentElement?.dataset.flipaiDesktop)return;
    const here=location.pathname;
    if(here!=='/connections'&&here!=='/settings'){
      // Any other page: make sure no stale panel request keeps a window docked.
      post('/dock',{visible:false,x:0,y:0,width:0,height:0}).catch(()=>{});
      return;
    }
    try{await refresh()}catch(e){serviceErrorCard(e);return}
    q('#voice-call-unavailable')?.remove();
    if(here==='/settings') settingsCard();
    else connectionsCard();
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',install,{once:true});else install();
})();
`
