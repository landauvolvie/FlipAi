//go:build windows

package main

// voiceDesktopInitScript augments only FlipAi's trusted localhost desktop UI.
// The same pages opened in a normal browser remain unchanged. Keeping these
// controls client-side also means the existing SMS handlers and templates are
// untouched by the experimental voice-call feature.
const voiceDesktopInitScript = `
(() => {
  const VOICE = 'http://127.0.0.1:8771';
  const esc = (v) => String(v == null ? '' : v).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const q = (s, root=document) => root.querySelector(s);
  const checked = (id) => !!q('#'+id)?.checked;
  const value = (id) => q('#'+id)?.value || '';
  let snapshot = null;

  async function voiceFetch(path, options={}) {
    const r = await fetch(VOICE + path, Object.assign({cache:'no-store'}, options));
    if (!r.ok) throw new Error((await r.text()).trim() || ('Voice service returned ' + r.status));
    return r.json();
  }
  async function refresh() {
    snapshot = await voiceFetch('/status');
    return snapshot;
  }
  const statusPill = (runtime) => {
    if (!runtime || !runtime.browserRunning) return '<span class="pill warn">Not open</span>';
    if (runtime.signedIn) return '<span class="pill ok">Google Voice ready</span>';
    return '<span class="pill warn">Sign-in needed</span>';
  };
  function deviceOptions(kind, selected) {
    const devices = (snapshot?.runtime?.devices || []).filter(d => d.kind === kind);
    const seen = new Set();
    let out = '<option value="">Choose a device…</option>';
    for (const d of devices) {
      if (!d.label || seen.has(d.label)) continue;
      seen.add(d.label);
      out += '<option value="'+esc(d.label)+'"'+(d.label===selected?' selected':'')+'>'+esc(d.label)+'</option>';
    }
    if (selected && !seen.has(selected)) out += '<option value="'+esc(selected)+'" selected>'+esc(selected)+' (not currently found)</option>';
    return out;
  }
  function toast(message, bad=false) {
    const old = q('#voice-call-toast'); if (old) old.remove();
    const b = document.createElement('div');
    b.id='voice-call-toast'; b.className='banner '+(bad?'bad':'ok');
    b.innerHTML='<span>'+esc(message)+'</span>';
    q('.content')?.prepend(b);
    setTimeout(()=>b.remove(), 5500);
  }
  async function saveConfig(next) {
    snapshot = await voiceFetch('/config', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(next)});
    toast('Voice-call settings saved.');
    return snapshot;
  }
  async function openVoice() {
    await voiceFetch('/open', {method:'POST', headers:{'Content-Type':'application/json'}, body:'{}'});
    toast('Google Voice opened in FlipAi.');
  }

  function connectionsCard() {
    const cfg=snapshot.config, rt=snapshot.runtime;
    const card=document.createElement('section'); card.className='card'; card.id='voice-call-connection-card';
    card.innerHTML=`
      <div class="card-head divided">
        <div class="card-title-row"><span class="bmark lg voice">${document.querySelector('#brand-voice')?.innerHTML || ''}</span><div>
          <h2>Google Voice calls <span class="pill brand">Experimental</span></h2>
          <p>A dedicated Google Voice window kept alive by FlipAi for phone conversations with desktop voice.</p>
        </div></div>
        <div class="head-actions"><button class="btn accent" type="button" id="voice-open-google">Open Google Voice</button></div>
      </div>
      <div class="card-body"><div class="rows">
        <div class="row"><div class="label">Voice calling<span>This affects calls only. Your existing SMS/Gmail bridge is unchanged.</span></div><div class="value">${cfg.enabled?'<span class="pill ok">Enabled</span>':'<span class="pill">Off</span>'}</div></div>
        <div class="row"><div class="label">Google Voice window<span>Uses its own persistent WebView2 profile inside FlipAi.</span></div><div class="value">${statusPill(rt)}</div></div>
        <div class="row"><div class="label">Current call</div><div class="value">${rt.inCall?'<span class="pill ok">Connected</span><b>'+esc(rt.caller||'caller')+'</b>':'<span class="pill">Idle</span>'}</div></div>
      </div><p class="hint">Configure audio routing under Settings and caller access separately under each agent.</p></div>`;
    q('.tiles')?.after(card);
    q('#voice-open-google')?.addEventListener('click', ()=>openVoice().catch(e=>toast(e.message,true)));
  }

  function settingsCard() {
    const cfg=snapshot.config, rt=snapshot.runtime;
    const card=document.createElement('section'); card.className='card'; card.id='voice-call-settings-card';
    card.innerHTML=`
      <div class="card-head divided"><div class="card-title-row"><span class="mark shield">${document.querySelector('#icon-phone')?.innerHTML || ''}</span><div>
        <h2>Google Voice phone bridge <span class="pill brand">Experimental</span></h2>
        <p>Keep Google Voice listening and route a call through virtual audio to the selected desktop voice app.</p>
      </div></div><div class="head-actions">${statusPill(rt)}<button class="btn" type="button" id="vc-open">Open Google Voice</button><button class="btn primary" type="button" id="vc-save">Save voice settings</button></div></div>
      <div class="card-body">
        <div class="toggle"><div class="label">Enable phone voice<span>Starts the dedicated Google Voice window automatically after this Windows user signs in and keeps it alive while the PC is locked.</span></div><label class="switch"><input id="vc-enabled" type="checkbox"${cfg.enabled?' checked':''}><span class="slider"></span></label></div>
        <div class="toggle"><div class="label">Auto-answer authorized callers<span>Unknown or unparseable callers are never auto-answered.</span></div><label class="switch"><input id="vc-auto" type="checkbox"${cfg.autoAnswer?' checked':''}><span class="slider"></span></label></div>
        <div class="field"><label for="vc-default">Default voice agent</label><select id="vc-default"><option value="C"${cfg.defaultAgent==='C'?' selected':''}>ChatGPT / Codex</option><option value="A"${cfg.defaultAgent==='A'?' selected':''}>Claude Desktop</option></select><p class="hint">If a caller is allowed for only one agent, FlipAi routes to that agent automatically. If both allow the caller, this default wins.</p></div>
        <div class="section-label">Audio bridge</div>
        <div class="grid-2">
          <div class="field"><label for="vc-gv-in">Google Voice microphone</label><select id="vc-gv-in">${deviceOptions('audioinput',cfg.googleVoiceInput)}</select><p class="hint">Choose the virtual capture endpoint that receives the AI app's speaker audio.</p></div>
          <div class="field"><label for="vc-gv-out">Google Voice speaker</label><select id="vc-gv-out">${deviceOptions('audiooutput',cfg.googleVoiceOutput)}</select><p class="hint">Choose the virtual render endpoint that carries the caller toward the AI app's microphone.</p></div>
          <div class="field"><label for="vc-agent-in">AI app microphone</label><select id="vc-agent-in">${deviceOptions('audioinput',cfg.agentInput)}</select><p class="hint">Select this same paired endpoint once in ChatGPT/Claude voice settings.</p></div>
          <div class="field"><label for="vc-agent-out">AI app speaker</label><select id="vc-agent-out">${deviceOptions('audiooutput',cfg.agentOutput)}</select><p class="hint">Select this same paired endpoint once in ChatGPT/Claude voice settings.</p></div>
        </div>
        <div class="field"><label for="vc-ring">Ringing device (optional)</label><select id="vc-ring">${deviceOptions('audiooutput',cfg.ringOutput)}</select><p class="hint">Optional physical speaker for hearing an incoming ring locally. It is not used for the conversation bridge.</p></div>
        <p class="callout"><b>Two virtual audio cable paths are required.</b> FlipAi discovers the Windows audio endpoints and forces the Google Voice side automatically. The AI desktop app must be set to the paired microphone/speaker endpoints in its own voice settings. FlipAi does not silently install or redistribute a third-party audio driver.</p>
        ${rt.lastError?'<p class="callout"><b>Last voice error:</b> '+esc(rt.lastError)+'</p>':''}
      </div>`;
    const cards=q('.cards-2'); if(cards) cards.before(card); else q('.content')?.append(card);
    q('#vc-open')?.addEventListener('click', ()=>openVoice().catch(e=>toast(e.message,true)));
    q('#vc-save')?.addEventListener('click', async ()=>{
      try {
        const next=JSON.parse(JSON.stringify(snapshot.config));
        next.enabled=checked('vc-enabled'); next.autoAnswer=checked('vc-auto'); next.defaultAgent=value('vc-default');
        next.googleVoiceInput=value('vc-gv-in'); next.googleVoiceOutput=value('vc-gv-out'); next.agentInput=value('vc-agent-in'); next.agentOutput=value('vc-agent-out'); next.ringOutput=value('vc-ring');
        await saveConfig(next);
      } catch(e){toast(e.message,true)}
    });
  }

  function agentCard(agent) {
    const isClaude=agent==='A';
    const own=isClaude?snapshot.config.claude:snapshot.config.codex;
    const pane=q(isClaude?'#claude-pane':'#codex-pane'); if(!pane) return;
    const card=document.createElement('section'); card.className='card'; card.id=isClaude?'voice-call-agent-claude':'voice-call-agent-codex';
    const prefix=isClaude?'vca':'vcc';
    card.innerHTML=`
      <div class="card-head divided"><div><h2>Phone voice <span class="pill brand">Experimental</span></h2><p>${isClaude?'Let an authorized caller talk to Claude Desktop Voice. Claude Code itself currently has dictation rather than full two-way voice.':'Let an authorized caller talk to ChatGPT desktop Voice; Codex/Work voice can then control the agent from the conversation.'}</p></div><div class="head-actions"><button class="btn" type="button" id="${prefix}-test">Start voice test</button><button class="btn primary" type="button" id="${prefix}-save">Save phone voice</button></div></div>
      <div class="card-body">
        <div class="toggle"><div class="label">Allow this agent on phone calls<span>Caller authorization below is separate from the SMS allowlist.</span></div><label class="switch"><input id="${prefix}-enabled" type="checkbox"${own.enabled?' checked':''}><span class="slider"></span></label></div>
        <div class="field"><label for="${prefix}-callers">Allowed callers</label><textarea id="${prefix}-callers" rows="3" placeholder="8455551234&#10;9145559876">${esc(own.allowedCallers||'')}</textarea><p class="hint">One US/Canada number per line. Caller ID that cannot be matched to one of these numbers is blocked from the agent bridge.</p></div>
        <div class="grid-2">
          <div class="field"><label for="${prefix}-title">Desktop window title contains</label><input id="${prefix}-title" value="${esc(own.appTitle||'')}"><p class="hint">Usually ${isClaude?'Claude':'ChatGPT'}. FlipAi uses this to focus the right desktop app.</p></div>
          <div class="field"><label for="${prefix}-shortcut">Voice shortcut (recommended)</label><input id="${prefix}-shortcut" value="${esc(own.voiceShortcut||'')}" placeholder="Ctrl+Shift+V"><p class="hint">Use the Voice shortcut you configured in the desktop app. If blank, FlipAi tries the accessible Voice button.</p></div>
        </div>
        <div class="field"><label for="${prefix}-command">Launch command (optional)</label><input id="${prefix}-command" value="${esc(own.appCommand||'')}" placeholder="Path or app command"><p class="hint">Used only if the desktop window is not already open.</p></div>
      </div>`;
    pane.append(card);
    q('#'+prefix+'-save')?.addEventListener('click', async ()=>{
      try {
        const next=JSON.parse(JSON.stringify(snapshot.config)); const target=isClaude?next.claude:next.codex;
        target.enabled=checked(prefix+'-enabled'); target.allowedCallers=value(prefix+'-callers'); target.appTitle=value(prefix+'-title'); target.appCommand=value(prefix+'-command'); target.voiceShortcut=value(prefix+'-shortcut');
        await saveConfig(next);
      } catch(e){toast(e.message,true)}
    });
    q('#'+prefix+'-test')?.addEventListener('click', async ()=>{
      try {
        // Save first so the test uses exactly what is on screen.
        const next=JSON.parse(JSON.stringify(snapshot.config)); const target=isClaude?next.claude:next.codex;
        target.enabled=checked(prefix+'-enabled'); target.allowedCallers=value(prefix+'-callers'); target.appTitle=value(prefix+'-title'); target.appCommand=value(prefix+'-command'); target.voiceShortcut=value(prefix+'-shortcut');
        await saveConfig(next);
        await voiceFetch('/test-agent?agent='+agent,{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});
        toast((isClaude?'Claude':'ChatGPT')+' voice start requested.');
      } catch(e){toast(e.message,true)}
    });
  }

  async function install() {
    if (!document.documentElement.dataset.flipaiDesktop) return;
    try { await refresh(); } catch (_) { return; }
    const path=location.pathname;
    if(path==='/connections') connectionsCard();
    if(path==='/settings') settingsCard();
    if(path==='/agents'){agentCard('C');agentCard('A');}
  }
  if(document.readyState==='loading') document.addEventListener('DOMContentLoaded',install,{once:true}); else install();
})();
`
