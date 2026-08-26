//go:build windows

package main

// voiceDesktopInitScript augments only FlipAi's trusted localhost desktop UI.
// The same pages opened in a normal browser remain unchanged. Keeping these
// controls client-side also means the existing SMS handlers and templates are
// untouched by the voice-call feature.
//
// There is one Google Voice card. It used to be two -- a read-only "Google
// Voice calls" summary and a "Google Voice phone bridge" form -- which meant
// the same connection was described twice, in two different vocabularies, and
// the switch that turned calling on lived in the second card while the status
// that contradicted it lived in the first.
//
// Nothing on this card waits for a Save button. Every control writes as soon as
// it is changed, and the switch that decides whether FlipAi answers the phone
// writes through an endpoint of its own so that nothing else on the page can
// hold it up.
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

  /* ---------- the embedded Google Voice panel ----------
     Google Voice runs in its own window so it can stay signed in and listening
     with FlipAi closed. That window is placed over the empty panel below and
     stripped of its frame, so what the user sees is Google Voice inside the
     app rather than a second window that appeared on its own. The rectangle is
     reported continuously; the moment this page stops reporting it, the window
     goes back to running quietly in the background. */
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
  function startDockReporting(){
    if(dockTimer) return;
    dockTimer=setInterval(()=>reportDock(true),250);
    addEventListener('pagehide',stopDockReporting);
    addEventListener('beforeunload',stopDockReporting);
    document.addEventListener('visibilitychange',()=>{ if(document.hidden) reportDock(true); });
  }
  function stopDockReporting(){
    if(dockTimer){clearInterval(dockTimer);dockTimer=0;}
    poppedOut=true;
    lastDockJSON='';
    post('/dock',{visible:false,x:0,y:0,width:0,height:0}).catch(()=>{});
  }

  function embedStyle(){
    if(q('#gv-embed-style')) return;
    const st=E('style'); st.id='gv-embed-style';
    st.textContent=[
      '#gv-embed-slot{position:relative;height:620px;min-height:420px;border-radius:16px;border:1px solid var(--line,#e7e4ee);background:#0f0d15;overflow:hidden}',
      '#gv-embed-slot .gv-empty{position:absolute;inset:0;display:flex;flex-direction:column;gap:10px;align-items:center;justify-content:center;text-align:center;padding:28px;color:#d8d3e6;background:#151221}',
      '#gv-embed-slot .gv-empty b{font-size:15px;color:#fff}',
      '#gv-embed-slot .gv-empty span{max-width:520px;font-size:13px;line-height:1.5;color:#b3abc7}',
      '.gv-embed-wrap{margin:6px 0 4px}',
      '.gv-embed-note{display:flex;justify-content:space-between;gap:12px;align-items:center;margin:8px 0 0}',
      '.gv-flow{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:6px 0 2px}',
      '.gv-flow span{font-size:12px;background:var(--chip,#f3f0fa);border:1px solid var(--line,#e7e4ee);border-radius:999px;padding:5px 10px}',
      '.gv-flow i{font-style:normal;color:#8b83a3}'
    ].join('\n');
    document.head.append(st);
  }

  /* ---------- saving ---------- */

  let saveTimer=0;
  function collectConfig(){
    const next=JSON.parse(JSON.stringify(snapshot.config));
    next.autoAnswer=checked('vc-auto');
    next.defaultAgent=value('vc-default');
    next.googleVoiceInput=value('vc-gv-in');
    next.googleVoiceOutput=value('vc-gv-out');
    next.agentInput=value('vc-agent-in');
    next.agentOutput=value('vc-agent-out');
    next.ringOutput=value('vc-ring');
    for(const agent of ['C','A']){
      const isClaude=agent==='A', p=isClaude?'vca':'vcc', target=isClaude?next.claude:next.codex;
      target.enabled=checked(p+'-enabled');
      target.appTitle=value(p+'-title');
      target.voiceShortcut=value(p+'-shortcut');
      target.appCommand=value(p+'-command');
    }
    return next;
  }
  function markSaving(text){ const s=q('#vc-saved'); if(s){ s.textContent=text; } }
  async function saveNow(){
    try{
      snapshot=await post('/config',collectConfig());
      markSaving('Saved');
      updateStatusRows();
    }catch(e){ markSaving('Not saved'); toast(e.message,true); }
  }
  function scheduleSave(){
    markSaving('Saving...');
    clearTimeout(saveTimer);
    saveTimer=setTimeout(saveNow,500);
  }
  function autosave(node){
    node.addEventListener('change',scheduleSave);
    if(node.tagName==='INPUT'&&node.type!=='checkbox') node.addEventListener('input',scheduleSave);
    return node;
  }

  /* ---------- the card ---------- */

  function runtimePill(rt){
    if(rt?.inCall) return pill('On a call','ok');
    if(!rt?.browserRunning) return pill('Not running','warn');
    return rt.signedIn?pill('Listening for calls','ok'):pill('Sign in to Google Voice','warn');
  }
  function deviceSelect(id,kind,selected){
    const items=[['','Choose a device...']]; const seen=new Set();
    for(const d of (snapshot?.runtime?.devices||[])){ if(d.kind!==kind||!d.label||seen.has(d.label))continue; seen.add(d.label); items.push([d.label,d.label]); }
    if(selected&&!seen.has(selected))items.push([selected,selected+' (not currently found)']);
    return autosave(select(id,items,selected||''));
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

  // updateStatusRows redraws only the parts that change on their own, so a
  // dropdown the user is in the middle of using is never rebuilt underneath
  // them.
  function updateStatusRows(){
    const rt=snapshot.runtime||{},cfg=snapshot.config||{};
    const set=(id,node)=>{ const el=q('#'+id); if(!el)return; el.textContent=''; el.append(node); };
    set('vcs-state',cfg.enabled?pill('On — answering calls','ok'):pill('Off','warn'));
    set('vcs-window',runtimePill(rt));
    set('vcs-agents',(snapshot.callAgents||[]).length?pill((snapshot.callAgents||[]).join(' and '),'ok'):pill('Nobody yet','warn'));
    set('vcs-audio',snapshot.audioWarning?pill('Needs attention','warn'):pill('Ready','ok'));
    set('vcs-call',rt.inCall?pill(rt.caller?('Connected — '+rt.caller):'Connected','ok'):pill('Idle'));
    set('vcs-ring',rt.lastRingAt&&!/^0001/.test(rt.lastRingAt)?pill(new Date(rt.lastRingAt).toLocaleString(),'ok'):pill('Never','warn'));
    const sw=q('#vc-enabled'); if(sw) sw.checked=!!cfg.enabled;
    const empty=q('#gv-embed-slot .gv-empty');
    if(empty) empty.style.display=(rt.docked&&rt.browserRunning)?'none':'flex';
    const problems=q('#vc-problems');
    if(problems){ problems.textContent=''; for(const node of problemNodes()) problems.append(node); }
  }

  function problemNodes(){
    const out=[],rt=snapshot.runtime||{},cfg=snapshot.config||{};
    if(!snapshot.webView2){
      out.push(callout('Microsoft Edge WebView2 Runtime is not installed. ','FlipAi cannot show Google Voice without it. Install Microsoft’s free Evergreen Standalone Installer and restart FlipAi.'));
    }
    if(!cfg.enabled){
      out.push(callout('Calling is off. ','Turn on "Answer phone calls with an agent" above. FlipAi then keeps Google Voice running from Windows sign-in onward, whether or not this window is open.'));
    }
    if(!(snapshot.callAgents||[]).length){
      out.push(callout('No agent can take a call yet. ','Open the Agents page, add your phone number under the agent you want to talk to, and set that number to "Texts and calls" or "Calls only". A number allowed under one agent can never reach the other one.'));
    }
    if(snapshot.audioWarning) out.push(callout('Audio path: ',snapshot.audioWarning));
    if(cfg.enabled&&(!rt.lastRingAt||/^0001/.test(rt.lastRingAt))){
      out.push(callout('No call has ever rung here. ','Google Voice only rings in a browser when you have switched that on in Google Voice itself: in the panel below open Settings, then Calls, and turn on receiving calls on this device. Until then an incoming call goes to your forwarding phones and never reaches FlipAi.'));
    }
    if(rt.blocked) out.push(callout('Last call was not connected: ',rt.blocked));
    if(rt.lastError) out.push(callout('Google Voice window: ',rt.lastError));
    return out;
  }

  function voiceCard(){
    embedStyle();
    const cfg=snapshot.config,rt=snapshot.runtime,card=E('section','card'); card.id='voice-call-card';
    const [head,actions]=sectionHead('Google Voice','One connection: the Google Voice window FlipAi keeps signed in, the calls it answers, and the audio path to the agent.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Experimental','brand'));
    const saved=E('span','hint','Changes save as you make them'); saved.id='vc-saved';
    const popOut=btn('Open in its own window');
    actions.append(saved,popOut); card.append(head);

    const body=E('div','card-body');

    // 1. The one switch that matters.
    const sw=toggle('vc-enabled','Answer phone calls with an agent',
      'FlipAi opens Google Voice at Windows sign-in, keeps it running while the PC is locked, and reopens it if it is ever closed. You never have to open it yourself. Texts and Gmail routing are unchanged.',cfg.enabled);
    body.append(sw);
    q('input',sw).addEventListener('change',async(e)=>{
      const want=e.target.checked;
      e.target.disabled=true; markSaving('Saving...');
      try{ snapshot=await post('/enable',{enabled:want}); markSaving('Saved'); updateStatusRows(); reportDock(true); }
      catch(err){ e.target.checked=!want; markSaving('Not saved'); toast(err.message,true); }
      finally{ e.target.disabled=false; }
    });

    // 2. Google Voice itself, inside the app.
    const wrap=E('div','gv-embed-wrap');
    const slot=E('div'); slot.id='gv-embed-slot';
    const empty=E('div','gv-empty');
    empty.append(E('b','','Google Voice is not on screen yet'),
      E('span','','FlipAi shows the Google Voice window here, inside the app. Turn calling on above and it appears within a few seconds; the first run has to unpack Microsoft WebView2, which can take a little longer. Sign in to Google here once and FlipAi keeps that session.'));
    slot.append(empty); wrap.append(slot);
    const note=E('div','gv-embed-note');
    note.append(E('p','hint','This is the real Google Voice, running in a window FlipAi owns and keeps alive. It stays signed in and keeps answering calls after you close FlipAi.'));
    wrap.append(note);
    body.append(wrap);

    // 3. What actually happens on a call.
    body.append(E('div','section-label','How a call reaches the agent'));
    const flow=E('div','gv-flow');
    for(const step of ['Caller dials your Google Voice number','Google Voice rings in this window','FlipAi checks the number against your agents','FlipAi clicks Answer','Caller audio goes out of the Google Voice speaker','into the AI app microphone','the agent replies through the AI app speaker','back into the Google Voice microphone','caller hears the agent']){
      if(flow.childNodes.length) flow.append(E('i','','→'));
      flow.append(E('span','',step));
    }
    body.append(flow);
    body.append(E('p','hint','FlipAi never records or transcribes the call itself. It moves the sound between two applications on your PC using virtual audio cables, which is why two of them are needed — one for each direction.'));

    // 4. Everything that can go wrong, in one place, kept live.
    const problems=E('div'); problems.id='vc-problems'; body.append(problems);

    // 5. Status.
    const rows=E('div','rows');
    const cell=(id)=>{ const v=E('span'); v.id=id; return v; };
    rows.append(row('Calling','Whether FlipAi answers the phone at all.',cell('vcs-state')));
    rows.append(row('Google Voice window','Kept running by FlipAi, signed in to your Google account.',cell('vcs-window')));
    rows.append(row('Agents on calls','Set by giving an agent a number that may call, on the Agents page.',cell('vcs-agents')));
    rows.append(row('Audio path','Needed for the caller and the agent to hear each other.',cell('vcs-audio')));
    rows.append(row('Current call','',cell('vcs-call')));
    rows.append(row('Ring seen','Whether a call has ever reached this window.',cell('vcs-ring')));
    rows.append(row('Edge WebView2 runtime','Windows component FlipAi needs to show Google Voice.',
      snapshot.webView2?pill(snapshot.webView2,'ok'):pill('Not installed','warn')));
    body.append(rows);

    // 6. Call handling.
    body.append(E('div','section-label','Call handling'));
    const auto=toggle('vc-auto','Auto-answer authorized callers','A number that is not allowed under an agent is never answered, and an unreadable caller ID is never answered.',cfg.autoAnswer);
    autosave(q('input',auto)); body.append(auto);
    body.append(field('Default voice agent',autosave(select('vc-default',[['C','ChatGPT / Codex'],['A','Claude Desktop']],cfg.defaultAgent)),
      'Only used if a caller is somehow allowed under both agents. A phone number belongs to exactly one agent, so normally the number itself decides.'));

    // 7. Audio bridge.
    body.append(E('div','section-label','Audio bridge'));
    const grid=E('div','grid-2');
    grid.append(field('Google Voice microphone',deviceSelect('vc-gv-in','audioinput',cfg.googleVoiceInput),'Virtual capture endpoint receiving the AI app speaker.'));
    grid.append(field('Google Voice speaker',deviceSelect('vc-gv-out','audiooutput',cfg.googleVoiceOutput),'Virtual render endpoint carrying the caller toward the AI app microphone.'));
    grid.append(field('AI app microphone',deviceSelect('vc-agent-in','audioinput',cfg.agentInput),'Select this paired endpoint once in ChatGPT/Claude voice settings.'));
    grid.append(field('AI app speaker',deviceSelect('vc-agent-out','audiooutput',cfg.agentOutput),'Select this paired endpoint once in ChatGPT/Claude voice settings.'));
    body.append(grid);
    body.append(field('Ringing device (optional)',deviceSelect('vc-ring','audiooutput',cfg.ringOutput),'Optional local speaker for the ring; it is not used for the conversation path.'));
    body.append(callout('Two virtual audio cables are required, one per direction. ','Cable 1 carries the caller to the agent: set the Google Voice speaker to its input end and the AI app microphone to its output end. Cable 2 carries the agent back to the caller: set the AI app speaker to its input end and the Google Voice microphone to its output end. FlipAi applies the Google Voice side itself; the AI app side is chosen once inside ChatGPT or Claude. FlipAi does not install or redistribute third-party audio drivers.'));

    // 8. Desktop apps.
    body.append(E('div','section-label','Desktop apps'));
    for(const agent of ['C','A']){
      const isClaude=agent==='A', own=isClaude?cfg.claude:cfg.codex, p=isClaude?'vca':'vcc';
      const t=toggle(p+'-enabled','Allow calls to '+(isClaude?'Claude':'ChatGPT / Codex'),
        'Not needed once a number under that agent is set to "Texts and calls" or "Calls only" on the Agents page. This only decides whether the agent may take calls at all — who may call it is always that agent’s own list of numbers, and no other agent’s.',own.enabled);
      autosave(q('input',t)); body.append(t);
      const g=E('div','grid-2');
      g.append(field('Desktop window title contains',autosave(input(p+'-title',own.appTitle,isClaude?'Claude':'ChatGPT')),'FlipAi uses it to bring the right app forward when a call connects.'));
      g.append(field('Voice shortcut (recommended)',autosave(input(p+'-shortcut',own.voiceShortcut,'Ctrl+Shift+V')),'The Voice shortcut set inside that desktop app. If blank, FlipAi tries its accessible Voice button.'));
      body.append(g);
      body.append(field('Launch command (optional)',autosave(input(p+'-command',own.appCommand,'Path or app command')),'Used only if the desktop window is not already open.'));
      const test=btn('Start voice test');
      test.addEventListener('click',async()=>{
        const label=test.textContent; test.disabled=true; test.textContent='Testing...';
        try{ await post('/test-agent?agent='+agent); toast((isClaude?'Claude':'ChatGPT')+' voice start requested.'); }
        catch(e){ toast(e.message,true); }
        finally{ test.disabled=false; test.textContent=label; }
      });
      body.append(test);
    }

    body.append(E('p','hint','Who may call, and whether a number may call at all, is set with the agent it reaches on the Agents page. Each agent has its own list; a number allowed under one agent cannot reach the other.'));
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

  async function install(){
    if(!globalThis.__flipaiDesktop && !document.documentElement?.dataset.flipaiDesktop)return;
    // Google Voice is a connection, so it lives on Connections and nowhere else.
    if(location.pathname!=='/connections'){ post('/dock',{visible:false,x:0,y:0,width:0,height:0}).catch(()=>{}); return; }
    try{await refresh()}catch(e){serviceErrorCard(e);return}
    q('#voice-call-unavailable')?.remove();
    voiceCard();
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',install,{once:true});else install();
})();
`
