//go:build windows

package main

// desktopInitScript is injected by WebView2 before each local FlipAi page.
// It turns the existing local control surface into the native desktop layout
// from the redesign mockup while leaving every existing form action/endpoint
// intact. This lets the UI be replaced incrementally without risking the SMS,
// Gmail, Codex, Claude, watchdog, or tray logic.
const desktopInitScript = `
(() => {
  const css = ` + "`" + `
    :root{--desktop-purple:#6941c6;--desktop-purple-soft:#f4f0ff;--desktop-green:#1f8a4c;--desktop-border:#e6e7eb;--desktop-muted:#667085;--desktop-bg:#ffffff;--desktop-side:#fbfbfc}
    *{box-sizing:border-box}
    html,body{background:var(--desktop-bg)!important;color:#101828!important;font-family:"Segoe UI Variable","Segoe UI",system-ui,sans-serif!important}
    body{min-width:900px}
    .topbar,.top{display:none!important}
    .wrap{width:100%!important;max-width:none!important;margin:0!important;padding:0!important;display:grid!important;grid-template-columns:210px minmax(0,1fr)!important;gap:0!important;min-height:100vh!important}
    .side{display:block!important;position:sticky!important;top:0!important;height:100vh!important;background:var(--desktop-side)!important;border-right:1px solid var(--desktop-border)!important;padding:0!important;z-index:10}
    .sidebox{height:100%!important;background:transparent!important;border:0!important;border-radius:0!important;box-shadow:none!important;padding:14px 12px!important;display:flex!important;flex-direction:column!important}
    .desktop-brand{display:flex;align-items:center;gap:10px;padding:8px 8px 20px;font-size:18px;font-weight:700;color:#101828}
    .desktop-brand-mark{width:28px;height:28px;display:grid;place-items:center;border-radius:6px;background:var(--desktop-purple);color:white;font-weight:800;font-size:14px}
    .side a{display:flex!important;align-items:center!important;gap:10px!important;color:#344054!important;padding:9px 11px!important;margin:1px 0!important;border-radius:7px!important;font-size:13px!important;font-weight:500!important;text-decoration:none!important}
    .side a:hover,.side a.desktop-active{background:var(--desktop-purple-soft)!important;color:#4f2aa8!important}
    .side a.desktop-separator{margin-top:12px!important;padding-top:18px!important;border-top:1px solid var(--desktop-border)!important;border-radius:0!important}
    .side .tiny{margin-top:auto!important;border:1px solid var(--desktop-border)!important;border-radius:8px!important;padding:12px!important;background:white!important;color:var(--desktop-muted)!important;font-size:11px!important}
    .content{padding:28px 34px 44px!important;min-width:0!important;max-width:1320px!important;width:100%!important;margin:0 auto!important}
    .hero{background:white!important;color:#101828!important;padding:0 0 20px!important;border-radius:0!important;box-shadow:none!important;overflow:visible!important}
    .hero:after{display:none!important}
    .hero h1{font-size:27px!important;line-height:1.2!important;letter-spacing:-.4px!important;margin:0 0 4px!important;color:#101828!important}
    .hero p{color:var(--desktop-muted)!important;font-size:13px!important;max-width:760px!important}
    .statusgrid,.summary{display:grid!important;grid-template-columns:repeat(4,minmax(0,1fr))!important;gap:12px!important;margin:18px 0 0!important}
    .hero .stat,.summary .stat{background:#fff!important;border:1px solid var(--desktop-border)!important;border-radius:8px!important;padding:14px!important;color:#101828!important;box-shadow:none!important;min-height:88px!important}
    .hero .stat span,.summary .stat span{color:var(--desktop-muted)!important;font-size:11px!important;text-transform:none!important;letter-spacing:0!important}
    .hero .stat b,.summary .stat b{color:#101828!important;font-size:14px!important;margin-top:5px!important}
    .card{background:#fff!important;border:1px solid var(--desktop-border)!important;border-radius:9px!important;padding:20px!important;margin-top:14px!important;box-shadow:none!important}
    .content>.card:first-of-type{display:none!important}
    .cardhead{margin-bottom:14px!important}
    .cardhead h2{font-size:18px!important;letter-spacing:-.2px!important}
    .cardhead p{font-size:12px!important;color:var(--desktop-muted)!important;margin-top:3px!important}
    .num{display:none!important}
    .heading{gap:0!important}
    .badge{border-radius:999px!important;padding:4px 7px!important;font-size:10px!important}
    .good{background:#ecfdf3!important;color:#027a48!important}.neutral{background:#f2f4f7!important;color:#475467!important}.attention{background:#fffaeb!important;color:#b54708!important}
    .choicegrid,.grid2,.agentcards{gap:10px!important}
    .choice,.agent,.methodbox{border:1px solid var(--desktop-border)!important;border-radius:8px!important;box-shadow:none!important;background:white!important}
    .choice:has(input:checked){border-color:#b7a2ee!important;box-shadow:0 0 0 2px var(--desktop-purple-soft)!important}
    input[type=text],input[type=password],input[type=email],input[type=number],input[type=file],select,textarea,.input{border:1px solid #d0d5dd!important;border-radius:6px!important;padding:9px 10px!important;box-shadow:0 1px 2px rgba(16,24,40,.04)!important}
    input:focus,select:focus,textarea:focus{border-color:#9e77ed!important;box-shadow:0 0 0 3px #f4ebff!important}
    .btn,button{border-radius:6px!important;padding:8px 12px!important;font-size:12px!important;font-weight:600!important;box-shadow:none!important}
    .primary{background:var(--desktop-purple)!important;color:#fff!important}.primary:hover{background:#5834b0!important}
    .secondary{background:#f9fafb!important;color:#344054!important;border:1px solid var(--desktop-border)!important}.outline{background:#fff!important;border:1px solid #d0d5dd!important;color:#344054!important}
    .codebox{border-radius:7px!important;background:#f9fafb!important;color:#344054!important;border:1px solid var(--desktop-border)!important;font-size:11px!important}
    .codebox .mutedcode{color:#667085!important}
    .help,.footer-note{color:var(--desktop-muted)!important;font-size:11px!important}
    .advanced{border-top:1px solid var(--desktop-border)!important}
    .desktop-home-recent{margin-top:14px;border:1px solid var(--desktop-border);border-radius:9px;background:white;overflow:hidden}
    .desktop-home-recent-head{display:flex;align-items:center;justify-content:space-between;padding:13px 16px;border-bottom:1px solid var(--desktop-border)}
    .desktop-home-recent-head b{font-size:14px}.desktop-home-recent-head a{font-size:12px;color:var(--desktop-purple);text-decoration:none}
    .desktop-event{display:grid;grid-template-columns:130px 90px 90px minmax(0,1fr);gap:12px;padding:11px 16px;border-bottom:1px solid #f0f1f3;font-size:12px;align-items:center}
    .desktop-event:last-child{border-bottom:0}.desktop-event-time{color:var(--desktop-muted)}.desktop-event-stage{font-weight:600;text-transform:capitalize}.desktop-event-level{font-size:10px;width:max-content;border-radius:999px;padding:3px 6px;background:#ecfdf3;color:#027a48}.desktop-event-level.error{background:#fef3f2;color:#b42318}.desktop-event-level.warn{background:#fffaeb;color:#b54708}
    body.desktop-activity .wrap{display:block!important;padding:28px 34px 44px 244px!important}
    body.desktop-activity .desktop-activity-side{position:fixed;left:0;top:0;bottom:0;width:210px;background:var(--desktop-side);border-right:1px solid var(--desktop-border);padding:14px 12px;z-index:20}
    body.desktop-activity .head h1{font-size:24px!important}body.desktop-activity .head p{font-size:12px!important;color:var(--desktop-muted)!important}
    body.desktop-activity .card{padding:0!important;overflow:hidden!important}.event{font-size:11px!important}.privacy{font-size:11px!important;background:#fafafa!important}
    @media(max-width:980px){.statusgrid,.summary{grid-template-columns:repeat(2,minmax(0,1fr))!important}.content{padding-left:24px!important;padding-right:24px!important}}
  ` + "`" + `;

  const style = document.createElement('style');
  style.id = 'flipai-desktop-theme';
  style.textContent = css;
  document.documentElement.appendChild(style);

  function brand(){
    const el=document.createElement('div'); el.className='desktop-brand';
    el.innerHTML='<span class="desktop-brand-mark">F</span><span>FlipAi</span>'; return el;
  }

  function setupSettings(){
    const sidebox=document.querySelector('.sidebox');
    if(!sidebox) return;
    sidebox.innerHTML='';
    sidebox.appendChild(brand());
    const items=[
      ['home','⌂','Home'],['connections','↗','Connections'],['agents','◇','Agents'],['phone','☎','Phone'],['activity','◷','Activity'],['settings','⚙','Settings'],['advanced','⌁','Advanced']
    ];
    for(const [view,icon,label] of items){
      const a=document.createElement('a'); a.href=view==='activity'?'/activity':'#'+view; a.dataset.view=view;
      if(view==='settings') a.classList.add('desktop-separator');
      a.innerHTML='<span style="width:16px;text-align:center">'+icon+'</span><span>'+label+'</span>';
      if(view!=='activity') a.addEventListener('click',e=>{e.preventDefault();showView(view);history.replaceState(null,'','#'+view)});
      sidebox.appendChild(a);
    }
    const tiny=document.createElement('div'); tiny.className='tiny'; tiny.innerHTML='<span style="color:#22a06b">●</span> FlipAi is running<br><span style="display:block;margin-top:4px">Local desktop control</span>'; sidebox.appendChild(tiny);

    const recent=document.createElement('section'); recent.className='desktop-home-recent'; recent.id='desktop-home-recent';
    recent.innerHTML='<div class="desktop-home-recent-head"><b>Recent activity</b><a href="/activity">View all activity ›</a></div><div id="desktop-events"><div class="desktop-event"><span class="desktop-event-time">Loading…</span></div></div>';
    const hero=document.querySelector('.hero'); if(hero) hero.insertAdjacentElement('afterend',recent);
    loadRecent();
    const initial=(location.hash||'#home').slice(1); showView(['home','connections','agents','phone','settings','advanced'].includes(initial)?initial:'home');
  }

  function showView(view){
    const hero=document.querySelector('.hero'), recent=document.getElementById('desktop-home-recent');
    const sections={connections:document.getElementById('gmail'),agents:document.getElementById('agents'),phone:document.getElementById('security'),settings:document.getElementById('startup'),advanced:document.getElementById('diagnostics')};
    if(hero) hero.style.display=view==='home'?'block':'none'; if(recent) recent.style.display=view==='home'?'block':'none';
    for(const [name,el] of Object.entries(sections)) if(el) el.style.display=view===name?'block':'none';
    document.querySelectorAll('.side a[data-view]').forEach(a=>a.classList.toggle('desktop-active',a.dataset.view===view));
    const titles={home:'FlipAi',connections:'Connections',agents:'Agents',phone:'Phone',settings:'Settings',advanced:'Advanced'}; document.title=(titles[view]||'FlipAi')+' — FlipAi';
  }

  async function loadRecent(){
    const root=document.getElementById('desktop-events'); if(!root)return;
    try{const r=await fetch('/activity.json',{cache:'no-store'}); const events=await r.json();
      if(!events.length){root.innerHTML='<div class="desktop-event"><span class="desktop-event-time">No activity yet</span><span></span><span></span><span>Send a test SMS to begin.</span></div>';return}
      root.innerHTML=events.slice(0,6).map(e=>'<div class="desktop-event"><span class="desktop-event-time">'+new Date(e.time).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'})+'</span><span class="desktop-event-stage">'+esc(e.stage)+'</span><span class="desktop-event-level '+esc(e.level)+'">'+esc(e.level)+'</span><span>'+esc(e.message)+'</span></div>').join('');
    }catch(_){root.innerHTML='<div class="desktop-event"><span class="desktop-event-time">Activity unavailable</span></div>'}
  }
  function esc(v){return String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}

  function setupActivity(){
    document.body.classList.add('desktop-activity');
    const side=document.createElement('aside'); side.className='desktop-activity-side'; side.appendChild(brand());
    const items=[['/','⌂','Home'],['/#connections','↗','Connections'],['/#agents','◇','Agents'],['/#phone','☎','Phone'],['/activity','◷','Activity'],['/#settings','⚙','Settings'],['/#advanced','⌁','Advanced']];
    items.forEach((it,i)=>{const a=document.createElement('a');a.href=it[0];a.innerHTML='<span style="width:16px;text-align:center">'+it[1]+'</span><span>'+it[2]+'</span>';if(i===4)a.classList.add('desktop-active');if(i===5)a.classList.add('desktop-separator');side.appendChild(a)});
    const tiny=document.createElement('div');tiny.className='tiny';tiny.innerHTML='<span style="color:#22a06b">●</span> FlipAi is running';side.appendChild(tiny);document.body.prepend(side);
  }

  addEventListener('DOMContentLoaded',()=>{ if(location.pathname==='/activity') setupActivity(); else setupSettings(); });
})();
`
