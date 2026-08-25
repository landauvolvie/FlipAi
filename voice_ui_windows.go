//go:build windows

package main

// voiceDesktopInitScript augments only FlipAi's trusted localhost desktop UI.
// The same pages opened in a normal browser remain unchanged. Keeping these
// controls client-side also means the existing SMS handlers and templates are
// untouched by the experimental voice-call feature.
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
  function textarea(id, val, placeholder='') { const x=E('textarea'); x.id=id; x.rows=3; x.value=val||''; x.placeholder=placeholder; return x; }
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
  function toast(message,bad=false) {
    q('#voice-call-toast')?.remove();
    const b=E('div','banner '+(bad?'bad':'ok')); b.id='voice-call-toast'; b.append(E('span','',message));
    q('.content')?.prepend(b);
    // A failure explains what to do next and can be several lines long, so it
    // stays until it is dismissed. Only the success note times itself out.
    if(bad){ const x=btn('Dismiss'); x.addEventListener('click',()=>b.remove()); b.append(x); }
    else setTimeout(()=>b.remove(),5500);
    b.scrollIntoView({block:'nearest'});
  }
  async function voiceFetch(path, options={}) {
    const r=await fetch(VOICE+path,Object.assign({cache:'no-store'},options));
    if(!r.ok) throw new Error((await r.text()).trim()||('Voice service returned '+r.status));
    return r.json();
  }
  async function refresh(){ snapshot=await voiceFetch('/status'); return snapshot; }
  async function saveConfig(next){ snapshot=await voiceFetch('/config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(next)}); toast('Voice-call settings saved.'); return snapshot; }
  async function openVoice(){
    const r=await voiceFetch('/open',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});
    if(/behind other windows/i.test(r?.note||'')) toast('Google Voice opened behind this window. Find "FlipAi \u2014 Google Voice" in the taskbar and sign in there.');
    else toast('Google Voice is open. Sign in to your Google account in that window.');
  }
  // Opening waits for the window to actually exist, which on a first run means
  // waiting for WebView2 to initialize. The button has to say so rather than
  // look like it did nothing.
  function wireOpen(b){
    b.addEventListener('click',async()=>{
      const label=b.textContent;
      b.disabled=true; b.textContent='Opening Google Voice...';
      try{ await openVoice(); }
      catch(e){ toast(e.message,true); }
      finally{ b.disabled=false; b.textContent=label; }
    });
  }
  function runtimePill(rt){ if(!rt?.browserRunning)return pill('Not open','warn'); return rt.signedIn?pill('Google Voice ready','ok'):pill('Sign-in needed','warn'); }
  function deviceSelect(id,kind,selected){
    const items=[['','Choose a device...']]; const seen=new Set();
    for(const d of (snapshot?.runtime?.devices||[])){ if(d.kind!==kind||!d.label||seen.has(d.label))continue; seen.add(d.label); items.push([d.label,d.label]); }
    if(selected&&!seen.has(selected))items.push([selected,selected+' (not currently found)']);
    return select(id,items,selected||'');
  }

  function serviceErrorCard(err){
    q('#voice-call-unavailable')?.remove();
    const card=E('section','card'); card.id='voice-call-unavailable';
    const [head,actions]=sectionHead('Google Voice calls','Phone-to-agent calling is installed, but the local voice service could not be reached.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Needs attention','warn'));
    const retry=btn('Retry','btn accent'); actions.append(retry); card.append(head);
    const body=E('div','card-body');
    const callout=E('p','callout'); callout.append(E('b','', 'Voice service error: '),document.createTextNode(err?.message||String(err||'unknown error'))); body.append(callout);
    body.append(E('p','hint','Restart FlipAi first. The calling controls will reappear automatically when the local voice component is available. SMS is unaffected.'));
    card.append(body);
    q('.content')?.append(card);
    retry.addEventListener('click',()=>location.reload());
  }

  function connectionsCard(){
    const cfg=snapshot.config,rt=snapshot.runtime,card=E('section','card'); card.id='voice-call-connection-card';
    const [head,actions]=sectionHead('Google Voice calls','Dedicated Google Voice inside FlipAi for phone conversations with desktop voice.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Experimental','brand'));
    const open=btn('Open Google Voice','btn accent'); actions.append(open); card.append(head);
    const body=E('div','card-body'),rows=E('div','rows');
    rows.append(row('Voice calling','Calls only. Existing SMS/Gmail routing is unchanged.',cfg.enabled?pill('Enabled','ok'):pill('Off')));
    rows.append(row('Google Voice window','Persistent WebView2 profile owned by FlipAi.',runtimePill(rt)));
    rows.append(row('Edge WebView2 runtime','Windows component FlipAi needs to show Google Voice.',
      snapshot.webView2?pill(snapshot.webView2,'ok'):pill('Not installed','warn')));
    rows.append(row('Current call','',rt.inCall?pill('Connected','ok'):pill('Idle')));
    rows.append(row('Ring seen','Whether a call has ever reached this window.',
      rt.lastRingAt&&!/^0001/.test(rt.lastRingAt)?pill(new Date(rt.lastRingAt).toLocaleString(),'ok'):pill('Never','warn')));
    if(rt.lastOpen) rows.append(row('Last open attempt','',E('span','',rt.lastOpen)));
    body.append(rows);
    if(rt.lastError){
      const c=E('p','callout');
      c.append(E('b','','Google Voice window: '),document.createTextNode(rt.lastError));
      body.append(c);
    }
    if(rt.blocked){
      const c=E('p','callout');
      c.append(E('b','','Last call was not connected: '),document.createTextNode(rt.blocked));
      body.append(c);
    }
    if(!rt.lastRingAt||/^0001/.test(rt.lastRingAt)){
      const c=E('p','callout');
      c.append(E('b','','No call has ever rung in this window. '),document.createTextNode('Google Voice only rings in a browser when you have switched that on in Google Voice itself: open Google Voice, go to Settings, Calls, and turn on receiving calls on this device. Until then an incoming call goes to your forwarding phones and never reaches FlipAi.'));
      body.append(c);
    }
    if(rt.controls){
      const d=E('details'); const sum=E('summary','','What FlipAi can see on the page right now');
      const pre=E('p','hint',rt.controls); d.append(sum,pre); body.append(d);
    }
    body.append(E('p','hint','Who may call, and whether a number may call at all, is set with the agent it reaches on the Agents page.')); card.append(body);
    q('.content')?.append(card); wireOpen(open);
  }

  function settingsCard(){
    const cfg=snapshot.config,rt=snapshot.runtime,card=E('section','card'); card.id='voice-call-settings-card';
    const [head,actions]=sectionHead('Google Voice phone bridge','Keep Google Voice listening and route authorized calls through virtual audio to desktop voice.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Experimental','brand')); actions.append(runtimePill(rt));
    const open=btn('Open Google Voice'),save=btn('Save voice settings','btn primary'); actions.append(open,save); card.append(head);
    const body=E('div','card-body');
    if(!snapshot.webView2){
      const c=E('p','callout');
      c.append(E('b','','Microsoft Edge WebView2 Runtime is not installed. '),document.createTextNode('FlipAi cannot show the Google Voice window without it. Install Microsoft\u2019s free Evergreen Standalone Installer, then press Open Google Voice again.'));
      body.append(c);
    }
    body.append(toggle('vc-enabled','Enable phone voice','Starts the dedicated Google Voice window automatically after this Windows user signs in and keeps it alive while the PC is locked.',cfg.enabled));
    body.append(toggle('vc-auto','Auto-answer authorized callers','Unknown or unparseable caller IDs are never auto-answered.',cfg.autoAnswer));
    body.append(field('Default voice agent',select('vc-default',[['C','ChatGPT / Codex'],['A','Claude Desktop']],cfg.defaultAgent),'If a caller is allowed for only one agent, that agent wins. If both allow the caller, this default wins.'));
    body.append(E('div','section-label','Audio bridge'));
    const grid=E('div','grid-2');
    grid.append(field('Google Voice microphone',deviceSelect('vc-gv-in','audioinput',cfg.googleVoiceInput),'Virtual capture endpoint receiving the AI app speaker.'));
    grid.append(field('Google Voice speaker',deviceSelect('vc-gv-out','audiooutput',cfg.googleVoiceOutput),'Virtual render endpoint carrying the caller toward the AI app microphone.'));
    grid.append(field('AI app microphone',deviceSelect('vc-agent-in','audioinput',cfg.agentInput),'Select this paired endpoint once in ChatGPT/Claude voice settings.'));
    grid.append(field('AI app speaker',deviceSelect('vc-agent-out','audiooutput',cfg.agentOutput),'Select this paired endpoint once in ChatGPT/Claude voice settings.'));
    body.append(grid);
    body.append(field('Ringing device (optional)',deviceSelect('vc-ring','audiooutput',cfg.ringOutput),'Optional local speaker for the ring; it is not used for the conversation path.'));
    const note=E('p','callout'); const bold=E('b','', 'Two virtual audio cables are required, one per direction. '); note.append(bold,document.createTextNode('Cable 1 carries the caller to the agent: set the Google Voice speaker to its input end and the AI app microphone to its output end. Cable 2 carries the agent back to the caller: set the AI app speaker to its input end and the Google Voice microphone to its output end. FlipAi applies the Google Voice side itself; the AI app side is chosen once inside ChatGPT or Claude. FlipAi does not install or redistribute third-party audio drivers.')); body.append(note);

    body.append(E('div','section-label','Desktop apps'));
    for(const agent of ['C','A']){
      const isClaude=agent==='A', own=isClaude?cfg.claude:cfg.codex, p=isClaude?'vca':'vcc';
      body.append(toggle(p+'-enabled','Allow calls to '+(isClaude?'Claude':'ChatGPT / Codex'),'Which numbers may call it is set with that agent on the Agents page.',own.enabled));
      const grid=E('div','grid-2');
      grid.append(field('Desktop window title contains',input(p+'-title',own.appTitle,isClaude?'Claude':'ChatGPT'),'FlipAi uses it to bring the right app forward when a call connects.'));
      grid.append(field('Voice shortcut (recommended)',input(p+'-shortcut',own.voiceShortcut,'Ctrl+Shift+V'),'The Voice shortcut set inside that desktop app. If blank, FlipAi tries its accessible Voice button.'));
      body.append(grid);
      body.append(field('Launch command (optional)',input(p+'-command',own.appCommand,'Path or app command'),'Used only if the desktop window is not already open.'));
      const test=btn('Start voice test');
      test.addEventListener('click',async()=>{
        const label=test.textContent; test.disabled=true; test.textContent='Testing...';
        try{ await voiceFetch('/test-agent?agent='+agent,{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'}); toast((isClaude?'Claude':'ChatGPT')+' voice start requested.'); }
        catch(e){ toast(e.message,true); }
        finally{ test.disabled=false; test.textContent=label; }
      });
      body.append(test);
    }
    if(rt.lastError){const er=E('p','callout');er.append(E('b','', 'Last voice error: '),document.createTextNode(rt.lastError));body.append(er);}
    card.append(body); q('.content')?.append(card);
    wireOpen(open);
    save.addEventListener('click',async()=>{
      try{
        const next=JSON.parse(JSON.stringify(snapshot.config));
        next.enabled=checked('vc-enabled');
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
        await saveConfig(next);
      }catch(e){toast(e.message,true)}
    });
  }

  async function install(){
    if(!globalThis.__flipaiDesktop && !document.documentElement?.dataset.flipaiDesktop)return;
    try{await refresh()}catch(e){serviceErrorCard(e);return}
    q('#voice-call-unavailable')?.remove();
    // Google Voice is a connection, so it lives on Connections and nowhere else.
    if(location.pathname==='/connections'){connectionsCard();settingsCard();}
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',install,{once:true});else install();
})();
`
