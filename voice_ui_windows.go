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
    q('#voice-call-toast')?.remove(); const b=E('div','banner '+(bad?'bad':'ok')); b.id='voice-call-toast'; b.append(E('span','',message)); q('.content')?.prepend(b); setTimeout(()=>b.remove(),5500);
  }
  async function voiceFetch(path, options={}) {
    const r=await fetch(VOICE+path,Object.assign({cache:'no-store'},options));
    if(!r.ok) throw new Error((await r.text()).trim()||('Voice service returned '+r.status));
    return r.json();
  }
  async function refresh(){ snapshot=await voiceFetch('/status'); return snapshot; }
  async function saveConfig(next){ snapshot=await voiceFetch('/config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(next)}); toast('Voice-call settings saved.'); return snapshot; }
  async function openVoice(){ await voiceFetch('/open',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'}); toast('Google Voice opened in FlipAi.'); }
  function runtimePill(rt){ if(!rt?.browserRunning)return pill('Not open','warn'); return rt.signedIn?pill('Google Voice ready','ok'):pill('Sign-in needed','warn'); }
  function deviceSelect(id,kind,selected){
    const items=[['','Choose a device...']]; const seen=new Set();
    for(const d of (snapshot?.runtime?.devices||[])){ if(d.kind!==kind||!d.label||seen.has(d.label))continue; seen.add(d.label); items.push([d.label,d.label]); }
    if(selected&&!seen.has(selected))items.push([selected,selected+' (not currently found)']);
    return select(id,items,selected||'');
  }

  function connectionsCard(){
    const cfg=snapshot.config,rt=snapshot.runtime,card=E('section','card'); card.id='voice-call-connection-card';
    const [head,actions]=sectionHead('Google Voice calls','Dedicated Google Voice inside FlipAi for phone conversations with desktop voice.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Experimental','brand'));
    const open=btn('Open Google Voice','btn accent'); actions.append(open); card.append(head);
    const body=E('div','card-body'),rows=E('div','rows');
    rows.append(row('Voice calling','Calls only. Existing SMS/Gmail routing is unchanged.',cfg.enabled?pill('Enabled','ok'):pill('Off')));
    rows.append(row('Google Voice window','Persistent WebView2 profile owned by FlipAi.',runtimePill(rt)));
    rows.append(row('Current call','',rt.inCall?pill('Connected','ok'):pill('Idle')));
    body.append(rows,E('p','hint','Audio routing lives under Settings. Caller access is configured separately under each agent.')); card.append(body);
    q('.tiles')?.after(card); open.addEventListener('click',()=>openVoice().catch(e=>toast(e.message,true)));
  }

  function settingsCard(){
    const cfg=snapshot.config,rt=snapshot.runtime,card=E('section','card'); card.id='voice-call-settings-card';
    const [head,actions]=sectionHead('Google Voice phone bridge','Keep Google Voice listening and route authorized calls through virtual audio to desktop voice.');
    head.querySelector('h2').append(document.createTextNode(' '),pill('Experimental','brand')); actions.append(runtimePill(rt));
    const open=btn('Open Google Voice'),save=btn('Save voice settings','btn primary'); actions.append(open,save); card.append(head);
    const body=E('div','card-body');
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
    const note=E('p','callout'); const bold=E('b','', 'Two virtual audio cable paths are required. '); note.append(bold,document.createTextNode('FlipAi discovers Windows endpoints and forces the Google Voice side automatically. Set the paired microphone/speaker once in the AI desktop app. FlipAi does not silently install or redistribute third-party audio drivers.')); body.append(note);
    if(rt.lastError){const er=E('p','callout');er.append(E('b','', 'Last voice error: '),document.createTextNode(rt.lastError));body.append(er);}
    card.append(body); const cards=q('.cards-2'); if(cards)cards.before(card); else q('.content')?.append(card);
    open.addEventListener('click',()=>openVoice().catch(e=>toast(e.message,true)));
    save.addEventListener('click',async()=>{try{const next=JSON.parse(JSON.stringify(snapshot.config));next.enabled=checked('vc-enabled');next.autoAnswer=checked('vc-auto');next.defaultAgent=value('vc-default');next.googleVoiceInput=value('vc-gv-in');next.googleVoiceOutput=value('vc-gv-out');next.agentInput=value('vc-agent-in');next.agentOutput=value('vc-agent-out');next.ringOutput=value('vc-ring');await saveConfig(next);}catch(e){toast(e.message,true)}});
  }

  function agentCard(agent){
    const isClaude=agent==='A',own=isClaude?snapshot.config.claude:snapshot.config.codex,pane=q(isClaude?'#claude-pane':'#codex-pane'); if(!pane)return;
    const p=isClaude?'vca':'vcc',card=E('section','card'); card.id=isClaude?'voice-call-agent-claude':'voice-call-agent-codex';
    const desc=isClaude?'Allow approved callers to talk to Claude Desktop Voice. Claude Code itself currently provides dictation rather than a full two-way voice session.':'Allow approved callers to talk to ChatGPT desktop Voice; Work/Codex voice can then control the agent from the conversation.';
    const [head,actions]=sectionHead('Phone voice',desc); head.querySelector('h2').append(document.createTextNode(' '),pill('Experimental','brand'));
    const test=btn('Start voice test'),save=btn('Save phone voice','btn primary');actions.append(test,save);card.append(head);
    const body=E('div','card-body'); body.append(toggle(p+'-enabled','Allow this agent on phone calls','Caller authorization here is separate from the SMS allowlist.',own.enabled));
    body.append(field('Allowed callers',textarea(p+'-callers',own.allowedCallers,'8455551234\n9145559876'),'One US/Canada number per line. Unmatched or hidden caller ID cannot reach the agent bridge.'));
    const grid=E('div','grid-2');
    grid.append(field('Desktop window title contains',input(p+'-title',own.appTitle,isClaude?'Claude':'ChatGPT'),'Usually '+(isClaude?'Claude':'ChatGPT')+'. FlipAi uses it to focus the correct app.'));
    grid.append(field('Voice shortcut (recommended)',input(p+'-shortcut',own.voiceShortcut,'Ctrl+Shift+V'),'Use the Voice shortcut configured in the desktop app. If blank, FlipAi tries the accessible Voice button.'));
    body.append(grid,field('Launch command (optional)',input(p+'-command',own.appCommand,'Path or app command'),'Used only if the desktop window is not already open.'));card.append(body);pane.append(card);
    const copyToConfig=()=>{const next=JSON.parse(JSON.stringify(snapshot.config)),target=isClaude?next.claude:next.codex;target.enabled=checked(p+'-enabled');target.allowedCallers=value(p+'-callers');target.appTitle=value(p+'-title');target.appCommand=value(p+'-command');target.voiceShortcut=value(p+'-shortcut');return next;};
    save.addEventListener('click',async()=>{try{await saveConfig(copyToConfig())}catch(e){toast(e.message,true)}});
    test.addEventListener('click',async()=>{try{await saveConfig(copyToConfig());await voiceFetch('/test-agent?agent='+agent,{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});toast((isClaude?'Claude':'ChatGPT')+' voice start requested.')}catch(e){toast(e.message,true)}});
  }

  async function install(){
    if(!document.documentElement.dataset.flipaiDesktop)return;
    try{await refresh()}catch(_){return}
    if(location.pathname==='/connections')connectionsCard();
    if(location.pathname==='/settings')settingsCard();
    if(location.pathname==='/agents'){agentCard('C');agentCard('A');}
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',install,{once:true});else install();
})();
`
