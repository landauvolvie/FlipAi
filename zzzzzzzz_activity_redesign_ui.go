package main

// activityRedesignHTML intentionally lives outside ui_pages.go. The updater UI
// is being changed independently, and keeping Activity isolated lets both pieces
// move without creating avoidable merge conflicts.
const activityRedesignHTML = `{{define "content"}}
<div class="activity2" data-activity-redesign>
  <div class="activity2-head">
    <div>
      <div class="activity2-title-line">
        <h1>Activity</h1>
        <span class="activity2-live"><i></i>Live</span>
      </div>
      <p>Live message and action log across every FlipAi agent.</p>
    </div>
    <div class="activity2-actions">
      <a class="btn" href="/logs/export">{{icon "download"}}Export</a>
      <form method="post" action="/activity/clear" data-confirm="Clear the FlipAi activity history?"><button class="btn" type="submit">{{icon "trash"}}Clear</button></form>
    </div>
  </div>

  <div class="activity2-search-row">
    <label class="activity2-search">{{icon "search"}}<input type="search" data-activity-search placeholder="Search by agent, model, message, or status" aria-label="Search activity"></label>
    <select data-activity-range aria-label="Filter by time">
      <option value="">Any time</option>
      <option value="1">Last hour</option>
      <option value="24">Last 24 hours</option>
      <option value="168">Last 7 days</option>
    </select>
  </div>

  <div class="activity2-agents" role="group" aria-label="Filter by agent">
    <button type="button" class="activity2-agent active" data-activity-agent="">All</button>
    <button type="button" class="activity2-agent" data-activity-agent="G"><span data-static-agent-icon="G"></span>ChatGPT Chat</button>
    <button type="button" class="activity2-agent" data-activity-agent="C"><span data-static-agent-icon="C"></span>Codex</button>
    <button type="button" class="activity2-agent" data-activity-agent="A"><span data-static-agent-icon="A"></span>Claude Code</button>
    <button type="button" class="activity2-agent" data-activity-agent="H"><span data-static-agent-icon="H"></span>Claude Chat</button>
    <button type="button" class="activity2-agent" data-activity-agent="X"><span data-static-agent-icon="X"></span>Grok</button>
    <button type="button" class="activity2-agent" data-activity-agent="M"><span data-static-agent-icon="M"></span>Gemini</button>
  </div>

  <div class="activity2-stats">
    <div class="activity2-stat"><span class="activity2-stat-icon total">{{icon "mail"}}</span><div><span>Messages today</span><b data-activity-stat="total">0</b><small>Unique Google Voice messages</small></div></div>
    <div class="activity2-stat"><span class="activity2-stat-icon incoming">↓</span><div><span>Incoming</span><b data-activity-stat="incoming">0</b><small>Routed to an agent</small></div></div>
    <div class="activity2-stat"><span class="activity2-stat-icon outgoing">↑</span><div><span>Outgoing</span><b data-activity-stat="outgoing">0</b><small>Replies sent</small></div></div>
    <div class="activity2-stat"><span class="activity2-stat-icon errors">!</span><div><span>Errors</span><b data-activity-stat="errors">0</b><small>Needs attention</small></div></div>
  </div>

  <section class="activity2-card">
    <div class="activity2-table-wrap">
      <table class="activity2-table">
        <thead><tr><th>Agent</th><th>Direction</th><th>Activity</th><th>Source</th><th>Time</th><th>Status</th></tr></thead>
        <tbody data-activity-events><tr><td colspan="6"><div class="activity2-empty">Loading activity…</div></td></tr></tbody>
      </table>
    </div>
    <div class="activity2-foot"><span data-activity-count>—</span><div class="activity2-pager" data-activity-pager></div></div>
  </section>

  <p class="activity2-privacy">FlipAi logs message flow and status only. SMS text, agent prompts/results, security codes, passwords, and tokens are not written to Activity.</p>
</div>

<style>
.activity2{max-width:1240px}
.activity2-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:18px}
.activity2-head h1{font-size:28px;font-weight:700;letter-spacing:-.025em}
.activity2-head p{margin:5px 0 0;color:var(--muted);font-size:13px}
.activity2-title-line{display:flex;align-items:center;gap:10px}
.activity2-live{display:inline-flex;align-items:center;gap:6px;height:25px;padding:0 9px;border:1px solid var(--ok-line);border-radius:999px;background:var(--ok-soft);color:var(--ok);font-size:11.5px;font-weight:700}
.activity2-live i{width:7px;height:7px;border-radius:50%;background:var(--ok);box-shadow:0 0 0 3px color-mix(in srgb,var(--ok) 13%,transparent)}
.activity2-actions{display:flex;gap:8px}.activity2-actions form{margin:0}
.activity2-search-row{display:grid;grid-template-columns:minmax(0,1fr) 145px;gap:10px;margin-bottom:12px}
.activity2-search{height:44px;display:flex;align-items:center;gap:9px;padding:0 13px;border:1px solid var(--line);border-radius:12px;background:var(--surface);box-shadow:var(--shadow-xs)}
.activity2-search:focus-within{border-color:var(--brand-line);box-shadow:var(--ring)}
.activity2-search svg{width:17px;height:17px;color:var(--faint);flex:0 0 auto}
.activity2-search input{width:100%;border:0;outline:0;background:transparent;font-size:13.5px}
.activity2-search-row select{height:44px;border:1px solid var(--line);border-radius:12px;background:var(--surface);padding:0 11px;outline:0;color:var(--ink-2)}
.activity2-agents{display:flex;gap:8px;overflow-x:auto;padding:1px 1px 7px;margin-bottom:11px;scrollbar-width:thin}
.activity2-agent{height:38px;display:inline-flex;align-items:center;gap:8px;white-space:nowrap;border:1px solid var(--line);border-radius:11px;background:var(--surface);padding:0 12px;color:var(--ink-2);font-size:12.5px;font-weight:620;cursor:pointer;transition:.14s ease}
.activity2-agent:hover{border-color:var(--brand-line);background:var(--surface-2)}
.activity2-agent.active{border-color:var(--brand-line);background:var(--brand-soft);color:var(--brand-ink)}
.activity2-agent .activity2-logo{width:22px;height:22px;border-radius:7px}
.activity2-stats{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin-bottom:12px}
.activity2-stat{display:flex;align-items:center;gap:12px;min-height:90px;padding:14px 15px;border:1px solid var(--line);border-radius:14px;background:var(--surface);box-shadow:var(--shadow-xs)}
.activity2-stat-icon{width:38px;height:38px;border-radius:11px;display:grid;place-items:center;flex:0 0 auto;font-size:20px;font-weight:700;background:var(--brand-soft);color:var(--brand-ink)}
.activity2-stat-icon svg{width:18px;height:18px}.activity2-stat-icon.incoming{background:var(--ok-soft);color:var(--ok)}.activity2-stat-icon.outgoing{background:var(--info-soft);color:var(--info)}.activity2-stat-icon.errors{background:var(--bad-soft);color:var(--bad)}
.activity2-stat div{min-width:0}.activity2-stat span:not(.activity2-stat-icon){display:block;color:var(--muted);font-size:11.5px;font-weight:600}.activity2-stat b{display:block;margin-top:1px;font-size:22px;line-height:1.15;letter-spacing:-.02em}.activity2-stat small{display:block;margin-top:3px;color:var(--faint);font-size:10.5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.activity2-card{overflow:hidden;border:1px solid var(--line);border-radius:15px;background:var(--surface);box-shadow:var(--shadow-sm)}
.activity2-table-wrap{overflow:auto}.activity2-table{width:100%;border-collapse:collapse;table-layout:fixed}.activity2-table th{height:43px;padding:0 13px;border-bottom:1px solid var(--line);background:var(--surface-2);color:var(--muted);text-align:left;font-size:10.5px;font-weight:720;letter-spacing:.035em;text-transform:uppercase}.activity2-table td{padding:11px 13px;border-bottom:1px solid var(--line-soft);vertical-align:middle;font-size:12.5px}.activity2-table tbody tr:last-child td{border-bottom:0}.activity2-table tbody tr:hover{background:color-mix(in srgb,var(--surface-2) 58%,transparent)}
.activity2-table th:nth-child(1){width:174px}.activity2-table th:nth-child(2){width:112px}.activity2-table th:nth-child(4){width:126px}.activity2-table th:nth-child(5){width:118px}.activity2-table th:nth-child(6){width:104px}
.activity2-agent-cell{display:flex;align-items:center;gap:9px;min-width:0}.activity2-agent-copy{min-width:0}.activity2-agent-copy b{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12.5px}.activity2-agent-copy small{display:block;color:var(--faint);font-size:10.5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.activity2-logo{width:28px;height:28px;display:grid;place-items:center;border-radius:8px;flex:0 0 auto;overflow:hidden;background:var(--surface-2);color:var(--ink)}.activity2-logo svg{width:19px;height:19px;display:block}.activity2-logo.chatgpt{background:#10a37f;color:white}.activity2-logo.codex{background:#15171b;color:white;font:700 10px/1 ui-monospace,SFMono-Regular,Consolas,monospace}.activity2-logo.claude{background:#f3e9df;color:#d97745}.activity2-logo.grok{background:#08090a;color:white}.activity2-logo.gemini{background:#eef2ff;color:#536dfe}
.activity2-direction{display:inline-flex;align-items:center;gap:5px;height:25px;padding:0 8px;border-radius:999px;font-size:10.5px;font-weight:700}.activity2-direction.incoming{background:var(--ok-soft);color:var(--ok)}.activity2-direction.outgoing{background:var(--info-soft);color:var(--info)}.activity2-direction.system{background:var(--surface-2);color:var(--muted)}
.activity2-message{max-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink-2)}.activity2-source{color:var(--muted);white-space:nowrap}.activity2-time{white-space:nowrap;color:var(--muted);font-variant-numeric:tabular-nums}.activity2-status{display:inline-flex;align-items:center;height:25px;padding:0 8px;border-radius:999px;font-size:10.5px;font-weight:720}.activity2-status.ok{background:var(--ok-soft);color:var(--ok)}.activity2-status.info{background:var(--info-soft);color:var(--info)}.activity2-status.warn{background:var(--warn-soft);color:var(--warn)}.activity2-status.bad{background:var(--bad-soft);color:var(--bad)}.activity2-status.neutral{background:var(--surface-2);color:var(--muted)}
.activity2-empty{padding:36px 16px;text-align:center;color:var(--muted)}.activity2-empty b{display:block;color:var(--ink);margin-bottom:3px}
.activity2-foot{min-height:48px;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 13px;border-top:1px solid var(--line);color:var(--muted);font-size:11.5px}.activity2-pager{display:flex;gap:5px}.activity2-pager button{min-width:30px;height:30px;border:1px solid var(--line);border-radius:8px;background:var(--surface);color:var(--ink-2);cursor:pointer}.activity2-pager button[aria-current="true"]{background:var(--brand-soft);border-color:var(--brand-line);color:var(--brand-ink);font-weight:700}.activity2-pager button:disabled{opacity:.42;cursor:default}
.activity2-privacy{margin:10px 2px 0;color:var(--faint);font-size:10.5px}
@media(max-width:980px){.activity2-stats{grid-template-columns:repeat(2,minmax(0,1fr))}.activity2-table{min-width:880px}}
@media(max-width:640px){.activity2-head{flex-direction:column}.activity2-search-row{grid-template-columns:1fr}.activity2-stats{grid-template-columns:1fr 1fr}.activity2-stat{min-height:78px}.activity2-stat small{display:none}}
</style>

<script>
(function(){
  var root=document.querySelector('[data-activity-redesign]');
  if(!root)return;

  var AGENTS={
    C:{name:'Codex',company:'OpenAI',kind:'codex'},
    A:{name:'Claude Code',company:'Anthropic',kind:'claude'},
    G:{name:'ChatGPT Chat',company:'OpenAI',kind:'chatgpt'},
    H:{name:'Claude Chat',company:'Anthropic',kind:'claude'},
    M:{name:'Gemini',company:'Google',kind:'gemini'},
    X:{name:'Grok',company:'xAI',kind:'grok'}
  };
  var state={events:[],agent:'',query:'',hours:0,page:1,perPage:12};

  function esc(v){return String(v==null?'':v).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];});}
  function logo(agent){
    var m=AGENTS[agent];
    if(!m)return '<span class="activity2-logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M5 7h14v10H5zM8 4v3m8-3v3M8 12h.01M16 12h.01"/></svg></span>';
    if(m.kind==='codex')return '<span class="activity2-logo codex">&gt;_</span>';
    if(m.kind==='claude')return '<span class="activity2-logo claude"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2.8v18.4M2.8 12h18.4M5.5 5.5l13 13M18.5 5.5l-13 13M8.2 3.8l7.6 16.4M20.2 8.2 3.8 15.8"/></svg></span>';
    if(m.kind==='grok')return '<span class="activity2-logo grok"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="M5 5l14 14M19 5 5 19"/><path d="M15.5 4.2h4.3v4.3"/></svg></span>';
    if(m.kind==='gemini')return '<span class="activity2-logo gemini"><svg viewBox="0 0 24 24"><path fill="currentColor" d="M12 1.8c.7 5.7 4.5 9.5 10.2 10.2-5.7.7-9.5 4.5-10.2 10.2C11.3 16.5 7.5 12.7 1.8 12 7.5 11.3 11.3 7.5 12 1.8z"/></svg></span>';
    return '<span class="activity2-logo chatgpt"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M12 3.1a4.2 4.2 0 0 1 4.1 3.2 4.2 4.2 0 0 1 2.1 7.4 4.2 4.2 0 0 1-6.1 5.7 4.2 4.2 0 0 1-7-4.2 4.2 4.2 0 0 1 .9-7.8A4.2 4.2 0 0 1 12 3.1z"/><path d="m8 8.2 4-2.3 4 2.3v4.6l-4 2.3-4-2.3zM12 15.1v4.1M8 12.8l-3.5 2M16 12.8l3.5 2"/></svg></span>';
  }
  function meta(agent){return AGENTS[agent]||{name:agent||'FlipAi',company:agent?'Agent':'System',kind:''};}
  function isStarted(e){return /started|starting|queued|routed to/i.test(e.message||'');}
  function direction(e){
    if(e.stage==='reply')return {name:'Outgoing',kind:'outgoing',arrow:'↑'};
    if(e.stage==='routing'&&e.agent)return {name:'Incoming',kind:'incoming',arrow:'↓'};
    if(e.stage==='agent'&&e.agent){
      if(isStarted(e))return {name:'Outgoing',kind:'outgoing',arrow:'↑'};
      return {name:'Incoming',kind:'incoming',arrow:'↓'};
    }
    if(e.stage==='gmail'||e.stage==='security')return {name:'Incoming',kind:'incoming',arrow:'↓'};
    return {name:'System',kind:'system',arrow:'•'};
  }
  function source(e){if(e.stage==='reply'||e.stage==='gmail'||e.stage==='security'||e.stage==='routing')return 'Google Voice';if(e.stage==='agent')return 'Agent';return 'FlipAi';}
  function status(e){
    if(e.level==='error')return {name:'Failed',tone:'bad'};
    if(e.level==='warn')return {name:'Attention',tone:'warn'};
    if(e.stage==='reply'&&e.level==='success')return {name:'Sent',tone:'ok'};
    if(e.stage==='routing'&&e.level==='success')return {name:'Received',tone:'ok'};
    if(e.stage==='agent'&&e.level==='success')return {name:'Completed',tone:'ok'};
    if(e.stage==='agent'&&isStarted(e))return {name:'Running',tone:'info'};
    if(e.stage==='gmail')return {name:'Received',tone:'info'};
    if(e.level==='success')return {name:'Completed',tone:'ok'};
    return {name:'Noted',tone:'neutral'};
  }
  function eventAgent(e){
    if(e.agent)return e.agent;
    return '';
  }
  function formatTime(v){var d=new Date(v);if(isNaN(d))return v||'—';return d.toLocaleTimeString([],{hour:'numeric',minute:'2-digit',second:'2-digit'});}
  function row(e){
    var key=eventAgent(e),m=meta(key),d=direction(e),s=status(e);
    return '<tr><td><div class="activity2-agent-cell">'+logo(key)+'<div class="activity2-agent-copy"><b>'+esc(m.name)+'</b><small>'+esc(m.company)+'</small></div></div></td>'+
      '<td><span class="activity2-direction '+d.kind+'">'+d.arrow+' '+d.name+'</span></td>'+
      '<td class="activity2-message" title="'+esc(e.message||'')+'">'+esc(e.message||'—')+'</td>'+
      '<td class="activity2-source">'+esc(source(e))+'</td>'+
      '<td class="activity2-time">'+esc(formatTime(e.time))+'</td>'+
      '<td><span class="activity2-status '+s.tone+'">'+s.name+'</span></td></tr>';
  }
  function searchable(e){var m=meta(eventAgent(e)),d=direction(e),s=status(e);return [e.message,e.stage,e.level,e.sender,m.name,m.company,d.name,s.name,source(e)].join(' ').toLowerCase();}
  function filtered(){
    var cutoff=state.hours?Date.now()-state.hours*3600000:0;
    return state.events.filter(function(e){
      if(state.agent&&eventAgent(e)!==state.agent)return false;
      if(cutoff&&new Date(e.time).getTime()<cutoff)return false;
      if(state.query&&searchable(e).indexOf(state.query)<0)return false;
      return true;
    });
  }
  function renderPager(pages){
    var host=root.querySelector('[data-activity-pager]');if(!host)return;
    if(pages<2){host.innerHTML='';return;}
    var out='<button type="button" data-activity-page="'+(state.page-1)+'"'+(state.page<=1?' disabled':'')+'>‹</button>';
    for(var i=1;i<=pages;i++){
      if(i>2&&i<pages-1&&Math.abs(i-state.page)>1)continue;
      out+='<button type="button" data-activity-page="'+i+'"'+(i===state.page?' aria-current="true"':'')+'>'+i+'</button>';
    }
    out+='<button type="button" data-activity-page="'+(state.page+1)+'"'+(state.page>=pages?' disabled':'')+'>›</button>';
    host.innerHTML=out;
  }
  function render(){
    var list=filtered(),pages=Math.max(1,Math.ceil(list.length/state.perPage));
    if(state.page>pages)state.page=pages;
    var start=(state.page-1)*state.perPage,slice=list.slice(start,start+state.perPage),body=root.querySelector('[data-activity-events]');
    if(slice.length)body.innerHTML=slice.map(row).join('');
    else body.innerHTML='<tr><td colspan="6"><div class="activity2-empty"><b>'+(state.events.length?'Nothing matches these filters':'No activity yet')+'</b>'+(state.events.length?'Try another agent or search.':'The first incoming message will appear here automatically.')+'</div></td></tr>';
    var count=root.querySelector('[data-activity-count]');
    if(count)count.textContent=list.length?'Showing '+(start+1)+'–'+Math.min(start+state.perPage,list.length)+' of '+list.length+' events':'No events recorded';
    renderPager(pages);renderStats();
  }
  function sameLocalDay(v){var d=new Date(v),n=new Date();return d.getFullYear()===n.getFullYear()&&d.getMonth()===n.getMonth()&&d.getDate()===n.getDate();}
  function renderStats(){
    var today=state.events.filter(function(e){return sameLocalDay(e.time);});
    var ids={},incoming={},outgoing={};
    today.forEach(function(e){
      if(e.messageId)ids[e.messageId]=true;
      if(e.stage==='routing'&&e.agent&&e.messageId)incoming[e.messageId]=true;
      if(e.stage==='reply'&&e.level==='success'&&e.agent&&e.messageId)outgoing[e.messageId]=true;
    });
    var values={total:Object.keys(ids).length,incoming:Object.keys(incoming).length,outgoing:Object.keys(outgoing).length,errors:today.filter(function(e){return e.level==='error';}).length};
    Object.keys(values).forEach(function(k){var el=root.querySelector('[data-activity-stat="'+k+'"]');if(el)el.textContent=values[k];});
  }
  function load(){
    fetch('/activity.json',{cache:'no-store'}).then(function(r){if(!r.ok)throw new Error('HTTP '+r.status);return r.json();}).then(function(events){state.events=events||[];render();root.classList.remove('offline');}).catch(function(){root.classList.add('offline');});
  }

  root.querySelectorAll('[data-static-agent-icon]').forEach(function(el){el.innerHTML=logo(el.getAttribute('data-static-agent-icon'));});
  root.addEventListener('click',function(e){
    var agent=e.target.closest('[data-activity-agent]');
    if(agent){root.querySelectorAll('[data-activity-agent]').forEach(function(x){x.classList.toggle('active',x===agent);});state.agent=agent.getAttribute('data-activity-agent')||'';state.page=1;render();return;}
    var page=e.target.closest('[data-activity-page]');if(page&&!page.disabled){state.page=parseInt(page.getAttribute('data-activity-page'),10)||1;render();}
  });
  var search=root.querySelector('[data-activity-search]');if(search)search.addEventListener('input',function(){state.query=search.value.trim().toLowerCase();state.page=1;render();});
  var range=root.querySelector('[data-activity-range]');if(range)range.addEventListener('change',function(){state.hours=parseFloat(range.value)||0;state.page=1;render();});
  load();setInterval(load,4000);
})();
</script>
{{end}}`

func init() {
	registerPage("activity", activityRedesignHTML)
}
