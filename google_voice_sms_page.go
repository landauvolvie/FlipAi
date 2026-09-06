package main

// googleVoiceSMSInitScript observes the Messages surface of FlipAi's dedicated
// Google Voice SMS WebView. Google Voice changes its conversation-list markup
// periodically, so the detector does not depend on one anchor shape: it watches
// trusted message links plus semantic conversation rows and resolves the exact
// itemId/phone from Google Voice metadata or, for a linkless SPA row, by opening
// that changed row in this dedicated hidden browser. Contact names and message
// body phone numbers are never used as sender identity.
const googleVoiceSMSInitScript = `
(() => {
  if (globalThis.__flipAiDirectSMSInstalled) return;
  globalThis.__flipAiDirectSMSInstalled = true;
  const state = {armed:false, rows:new Map(), recent:new Map(), pending:new Set(), chain:Promise.resolve()};
  const norm = v => String(v||'').replace(/\s+/g,' ').trim();
  const digits = v => {
    const m=String(v||'').match(/(?:\+?1[\s().-]*)?(?:\d[\s().-]*){10}/g)||[];
    for(const x of m){ const d=x.replace(/\D/g,'').replace(/^1(?=\d{10}$)/,''); if(d.length===10)return d; }
    return '';
  };
  const accountSlot = () => (String(location.pathname||'').match(/^\/u\/(\d+)/i)||[])[1]||'0';
  const threadForPhone = phone => phone ? '/u/'+accountSlot()+'/messages?itemId='+encodeURIComponent('t.+1'+phone) : '';
  const itemInfo = raw => {
    let v=String(raw||'').trim();
    try{v=decodeURIComponent(v)}catch(_){}
    const m=v.match(/(?:^|[^A-Za-z0-9])t\.\+1(\d{10})(?:$|[^0-9])/);
    if(!m)return {thread:'',phone:''};
    const item='t.+1'+m[1];
    return {thread:'/u/'+accountSlot()+'/messages?itemId='+encodeURIComponent(item),phone:m[1]};
  };
  const voiceURL = raw => {
    raw=String(raw||'').trim();
    if(!raw)return {thread:'',phone:''};
    try{
      const u=new URL(raw,location.href);
      if(u.protocol!=='https:'||u.hostname.toLowerCase()!=='voice.google.com'||u.hash)return {thread:'',phone:''};
      const path=String(u.pathname||'');
      const base=path.match(/^\/u\/(\d+)\/messages\/?$/i);
      if(base){
        const keys=[...u.searchParams.keys()];
        const vals=u.searchParams.getAll('itemId');
        if(keys.length!==1||keys[0]!=='itemId'||vals.length!==1)return {thread:'',phone:''};
        const item=vals[0], m=item.match(/^t\.\+1(\d{10})$/);
        if(!m)return {thread:'',phone:''};
        return {thread:'/u/'+base[1]+'/messages?itemId='+encodeURIComponent(item),phone:m[1]};
      }
      if(/^\/u\/\d+\/messages\/[^/?#]+$/i.test(path) && !u.search && !path.includes('..'))return {thread:path,phone:''};
    }catch(_){}
    return {thread:'',phone:''};
  };
  const rowAttrs=['href','data-item-id','data-thread-id','data-conversation-id','data-phone','data-phone-number','data-number','data-e164'];
  const identityAttrs=[...rowAttrs,'value','title','aria-label'];
  const identitySelector='[data-item-id],[data-thread-id],[data-conversation-id],[data-phone],[data-phone-number],[data-number],[data-e164],[class*="contact" i],[class*="sender" i],[class*="recipient" i],[data-contact],[data-recipient]';
  const addValues=(el,out,attrs) => {for(const a of attrs){try{const v=el?.getAttribute?.(a);if(v)out.push(v)}catch(_){}}};
  const trustedInfo = row => {
    const values=[];
    addValues(row,values,rowAttrs);
    let links=[];try{links=row?.matches?.('a[href*="/messages"]')?[row]:[...(row?.querySelectorAll?.('a[href*="/messages"]')||[])]}catch(_){}
    for(const a of links){
      const info=voiceURL(a.getAttribute?.('href')||'');
      if(info.thread)return info;
      addValues(a,values,identityAttrs);
    }
    let ids=[];try{ids=[...(row?.querySelectorAll?.(identitySelector)||[])]}catch(_){}
    for(const el of ids){addValues(el,values,identityAttrs);if(values.length>260)break;}
    let phone='';
    for(const v of values){
      const info=voiceURL(v);if(info.thread)return info;
      const item=itemInfo(v);if(item.thread)return item;
      if(!phone){const p=digits(v);if(p)phone=p;}
    }
    return {thread:threadForPhone(phone),phone};
  };
  const infoKey = info => info&&info.thread&&info.phone ? info.thread+'\u0000'+info.phone : '';
  const uniqueInfos = infos => {
    const out=new Map();
    for(const info of infos||[]){const k=infoKey(info);if(k)out.set(k,info)}
    return [...out.values()];
  };
  const historyInfos = () => {
    const out=[],seen=new Set();let budget=0;
    const visit=v=>{
      if(v==null||budget++>500)return;
      if(typeof v==='string'){
        const u=voiceURL(v);if(u.thread&&u.phone)out.push(u);
        const it=itemInfo(v);if(it.thread&&it.phone)out.push(it);
        return;
      }
      if(typeof v!=='object'||seen.has(v))return;
      seen.add(v);
      if(Array.isArray(v)){for(const x of v)visit(x);return}
      let vals=[];try{vals=Object.values(v)}catch(_){return}
      for(const x of vals)visit(x);
    };
    try{visit(history.state)}catch(_){}
    return uniqueInfos(out);
  };
  const pageIdentityInfos = () => {
    const out=[];
    const here=voiceURL(location.href);if(here.thread&&here.phone)out.push(here);
    out.push(...historyInfos());
    let els=[];try{els=[...document.querySelectorAll('a[href*="/messages"],[data-item-id],[data-thread-id],[data-conversation-id]')]}catch(_){}
    for(const el of els){
      if(el.matches?.('a[href*="/messages"]')){const u=voiceURL(el.getAttribute?.('href')||'');if(u.thread&&u.phone)out.push(u)}
      for(const a of ['data-item-id','data-thread-id','data-conversation-id']){
        let v='';try{v=el.getAttribute?.(a)||''}catch(_){}
        const it=itemInfo(v);if(it.thread&&it.phone)out.push(it);
      }
      if(out.length>600)break;
    }
    return uniqueInfos(out);
  };
  const resourceNames = () => {
    try{return new Set(performance.getEntriesByType('resource').map(e=>String(e.name||'')).filter(Boolean))}catch(_){return new Set()}
  };
  const newResourceInfos = before => {
    const out=[];
    let entries=[];try{entries=performance.getEntriesByType('resource')}catch(_){}
    for(const e of entries){
      const name=String(e?.name||'');if(!name||before.has(name))continue;
      try{const u=new URL(name,location.href);if(u.protocol!=='https:'||u.hostname.toLowerCase()!=='voice.google.com')continue}catch(_){continue}
      const vi=voiceURL(name);if(vi.thread&&vi.phone)out.push(vi);
      const it=itemInfo(name);if(it.thread&&it.phone)out.push(it);
    }
    return uniqueInfos(out);
  };
  const selectedConversationInfo = () => {
    const candidates=[];
    const selectors=[
      '[aria-selected="true"]','[aria-current="true"]','[data-selected="true"]',
      'gv-conversation-header','gv-thread-header','[class*="conversation-header" i]',
      '[class*="thread-header" i]','[class*="contact-header" i]'
    ];
    for(const sel of selectors){
      let list=[];try{list=document.querySelectorAll(sel)}catch(_){}
      for(const el of list){const info=trustedInfo(el);if(info.thread&&info.phone)candidates.push(info);if(candidates.length>80)break}
      if(candidates.length>80)break;
    }
    const unique=uniqueInfos(candidates);
    return unique.length===1?unique[0]:{thread:'',phone:''};
  };
  const previewSelector='[data-message-text],[data-message-snippet],[data-last-message],[class*="snippet" i],[class*="preview" i],[class*="last-message" i],[class*="message-text" i]';
  const isConversationRow = el => !!el && (
    el.matches?.('gv-conversation-list-item,gv-message-list-item,gv-thread-list-item,[data-conversation-id],[data-thread-id],[data-item-id]') ||
    /conversation|thread-(?:row|item)|message-(?:row|list-item)/i.test(String(el.className||''))
  );
  const messageLinks = () => {
    let list=[];try{list=[...document.querySelectorAll('a[href*="/messages"]')]}catch(_){}
    return list.filter(a=>!!voiceURL(a.getAttribute?.('href')||'').thread);
  };
  const linkContainer = link => {
    let best=link,node=link;
    for(let depth=0;depth<8&&node;depth++,node=node.parentElement){
      let count=0;try{count=[...node.querySelectorAll?.('a[href*="/messages"]')||[]].filter(a=>!!voiceURL(a.getAttribute?.('href')||'').thread).length}catch(_){}
      if(count>1)break;
      const text=norm(node.innerText||node.textContent||'');
      let preview=null;try{preview=node.querySelector?.(previewSelector)}catch(_){}
      if(preview&&text.length<7000)return node;
      if((isConversationRow(node)||text.length<1500)&&count===1)best=node;
    }
    return best;
  };
  const rows = () => {
    const out=[],seen=new Set();
    const add=el=>{if(el&&!seen.has(el)){seen.add(el);out.push(el)}};
    for(const a of messageLinks())add(linkContainer(a));
    const sels=['gv-conversation-list-item','gv-message-list-item','gv-thread-list-item','[data-conversation-id]','[data-thread-id]','[data-item-id]','[class*="conversation-list-item" i]','[class*="conversation-row" i]','[class*="thread-list-item" i]','[class*="thread-row" i]'];
    for(const sel of sels){let list=[];try{list=document.querySelectorAll(sel)}catch(_){}for(const el of list)add(el)}
    let generic=[];try{generic=document.querySelectorAll('[role="listitem"]')}catch(_){}
    for(const el of generic){let p=null;try{p=el.querySelector?.(previewSelector)}catch(_){}if(p||isConversationRow(el))add(el)}
    return out;
  };
  const bodyOf = (row,text,phone) => {
    let preferred=null;try{preferred=row.querySelector?.(previewSelector)}catch(_){}
    let v=norm(preferred?.innerText||preferred?.textContent||preferred?.getAttribute?.('aria-label')||'');
    if(v&&!/^messages?$/i.test(v))return v;
    const lines=String(row.innerText||text||'').split(/\n+/).map(norm).filter(Boolean);
    const skip=/^(messages?|unread|read|today|yesterday|now|just now|\d{1,2}:\d{2}\s*(am|pm)?)$/i;
    for(let i=lines.length-1;i>=0;i--){const line=lines[i];if(skip.test(line)||line===phone||digits(line)===phone)continue;return line}
    return '';
  };
  const emit = (phone,thread,body) => {
    const payload=JSON.stringify({sender:phone||'',thread:thread||'',body:body,at:new Date().toISOString()});
    try{if(typeof globalThis.flipVoiceSMS==='function')globalThis.flipVoiceSMS(payload)}catch(_){}
  };
  const clickRow = row => {
    try{
      let target=null;
      if(row.matches?.('a[href*="/messages"]')&&voiceURL(row.getAttribute?.('href')||'').thread)target=row;
      if(!target){for(const a of row.querySelectorAll?.('a[href*="/messages"]')||[]){if(voiceURL(a.getAttribute?.('href')||'').thread){target=a;break}}}
      if(!target){
        let clickable=[];try{clickable=row.querySelectorAll?.('button,[role="button"],[tabindex]:not([tabindex="-1"])')||[]}catch(_){}
        for(const el of clickable){if(!el.disabled){target=el;break}}
      }
      (target||row).click();return true;
    }catch(_){return false}
  };
  const resolveChanged = async (row,body,initial) => {
    let info=initial||trustedInfo(row);
    if(info.thread&&info.phone){emit(info.phone,info.thread,body);return}
    const before=String(location.href||''),beforeInfo=voiceURL(before);
    const beforePage=new Map(pageIdentityInfos().map(x=>[infoKey(x),x]));
    const beforeResources=resourceNames();
    if(!clickRow(row)){emit(info.phone,info.thread,body);return}
    for(let i=0;i<24;i++){
      await new Promise(r=>setTimeout(r,100));
      const href=String(location.href||''),current=voiceURL(href),changed=href!==before;
      const selected=selectedConversationInfo();
      if(selected.thread&&selected.phone){
        if(info.phone&&selected.phone!==info.phone){emit(info.phone,'',body);return}
        emit(selected.phone,selected.thread,body);return;
      }
      if(current.thread&&(changed||(info.phone&&current.phone===info.phone)||!beforeInfo.thread)){
        if(info.phone&&current.phone&&info.phone!==current.phone){emit(info.phone,'',body);return}
        emit(current.phone||info.phone,current.thread,body);return;
      }
      const newPage=pageIdentityInfos().filter(x=>!beforePage.has(infoKey(x)));
      if(newPage.length===1){
        const opened=newPage[0];
        if(info.phone&&opened.phone!==info.phone){emit(info.phone,'',body);return}
        emit(opened.phone,opened.thread,body);return;
      }
      const newResources=newResourceInfos(beforeResources);
      if(newResources.length===1){
        const opened=newResources[0];
        if(info.phone&&opened.phone!==info.phone){emit(info.phone,'',body);return}
        emit(opened.phone,opened.thread,body);return;
      }
    }
    info=trustedInfo(row);
    if(!(info.thread&&info.phone)){
      const selected=selectedConversationInfo();if(selected.thread&&selected.phone)info=selected;
    }
    emit(info.phone,info.thread,body);
  };
  const detectorHeartbeat = rowCount => {
    const pageText=norm(document.body?.innerText||'').slice(0,5000);
    const empty=/\b(no messages|no conversations|nothing here yet)\b/i.test(pageText);
    globalThis.__flipAiGoogleVoiceSMSDetectorAt=Date.now();
    globalThis.__flipAiGoogleVoiceSMSDetectorRows=rowCount;
    globalThis.__flipAiGoogleVoiceSMSDetectorReady=rowCount>0||empty;
  };
  const installStatusGate = () => {
    if(globalThis.__flipAiGoogleVoiceSMSStatusGated)return;
    const bridge=globalThis.flipGoogleVoiceSMSStatus;
    if(typeof bridge!=='function')return;
    globalThis.__flipAiGoogleVoiceSMSStatusGated=true;
    globalThis.flipGoogleVoiceSMSStatus=(signed,pageReady,href)=>{
      const detectorFresh=Date.now()-Number(globalThis.__flipAiGoogleVoiceSMSDetectorAt||0)<2500;
      const detectorReady=globalThis.__flipAiGoogleVoiceSMSDetectorReady===true&&detectorFresh;
      return bridge(signed,!!pageReady&&detectorReady,href);
    };
  };
  function scan(){
    if(location.hostname.toLowerCase()!=='voice.google.com')return;
    const now=Date.now();
    for(const [k,t] of state.recent){if(now-t>30000)state.recent.delete(k)}
    const list=rows();detectorHeartbeat(list.length);installStatusGate();
    const active=new Set();
    let index=0;
    for(const row of list){
      const text=norm((row.getAttribute?.('aria-label')||'')+' '+(row.innerText||row.textContent||''));
      const info=trustedInfo(row),body=bodyOf(row,text,info.phone);
      const stable=info.thread||norm(row.getAttribute?.('data-thread-id')||row.getAttribute?.('data-conversation-id')||row.getAttribute?.('data-item-id')||row.getAttribute?.('aria-label')||'')||('row-'+index);
      const key=stable;
      const sig=(info.phone||'unresolved')+'\u0000'+(info.thread||'no-thread')+'\u0000'+body;
      const old=state.rows.get(key)||'';
      state.rows.set(key,sig);active.add(key);index++;
      if(!state.armed||!body||sig===old||/^you\s*:/i.test(body)||state.recent.has(sig)||state.pending.has(sig))continue;
      state.recent.set(sig,now);state.pending.add(sig);
      state.chain=state.chain.then(()=>resolveChanged(row,body,info)).catch(()=>emit(info.phone,info.thread,body)).finally(()=>state.pending.delete(sig));
    }
    for(const key of [...state.rows.keys()])if(!active.has(key))state.rows.delete(key);
    state.armed=true;
  }
  const obs=new MutationObserver(()=>{clearTimeout(globalThis.__flipAiDirectSMSTimer);globalThis.__flipAiDirectSMSTimer=setTimeout(scan,70)});
  const start=()=>{installStatusGate();try{obs.observe(document.documentElement,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['aria-label','aria-selected','aria-current','title','href','class','data-selected','data-phone','data-phone-number','data-number','data-e164','data-item-id','data-thread-id','data-conversation-id','data-message-text','data-message-snippet','data-last-message','value']})}catch(_){}scan();setInterval(scan,700)};
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})()`
