package main

// googleVoiceSMSBackgroundInitScript is the direct-SMS listener used by the
// dedicated Google Voice SMS WebView. It never opens, selects, or clicks a
// conversation. The visible window is only for first-time sign-in; after that
// this script stays on the Messages list in FlipAi's hidden worker and learns
// the exact sender/thread from Google Voice background transport data. Contact
// names and phone numbers written inside message bodies are never sender identity.
const googleVoiceSMSBackgroundInitScript = `
(() => {
  if (globalThis.__flipAiDirectSMSBackgroundInstalled) return;
  globalThis.__flipAiDirectSMSBackgroundInstalled = true;

  const state = {
    armed:false, rows:new Map(), recent:new Map(), pending:new Set(),
    responses:[], networkAt:0, chain:Promise.resolve()
  };
  const norm=v=>String(v||'').replace(/\s+/g,' ').trim();
  const accountSlot=()=> (String(location.pathname||'').match(/^\/u\/(\d+)/i)||[])[1]||'0';
  const threadForPhone=phone=>phone?('/u/'+accountSlot()+'/messages?itemId='+encodeURIComponent('t.+1'+phone)):'';
  const itemPhones=v=>{
    const out=new Set();
    let s=String(v||'');
    try{s=decodeURIComponent(s)}catch(_){}
    const re=/(?:^|[^A-Za-z0-9])t\.\+1(\d{10})(?:$|[^0-9])/g;
    let m; while((m=re.exec(s))!==null) out.add(m[1]);
    return [...out];
  };
  const identityPhone=(key,value)=>{
    const k=String(key||'').toLowerCase();
    if(!/(?:phone|number|e164|participant|sender|address|contact)/.test(k)) return '';
    const s=String(value||'');
    const m=s.match(/(?:^|[^\d])\+?1[^\d]*(\d{3})[^\d]*(\d{3})[^\d]*(\d{4})(?:$|[^\d])/);
    return m ? m[1]+m[2]+m[3] : '';
  };
  const voiceURL=raw=>{
    raw=String(raw||'').trim(); if(!raw)return {phone:'',thread:''};
    try{
      const u=new URL(raw,location.href);
      if(u.protocol!=='https:'||u.hostname.toLowerCase()!=='voice.google.com'||u.hash)return {phone:'',thread:''};
      const base=String(u.pathname||'').match(/^\/u\/(\d+)\/messages\/?$/i);
      if(!base)return {phone:'',thread:''};
      const keys=[...u.searchParams.keys()],vals=u.searchParams.getAll('itemId');
      if(keys.length!==1||keys[0]!=='itemId'||vals.length!==1)return {phone:'',thread:''};
      const m=vals[0].match(/^t\.\+1(\d{10})$/); if(!m)return {phone:'',thread:''};
      return {phone:m[1],thread:'/u/'+base[1]+'/messages?itemId='+encodeURIComponent(vals[0])};
    }catch(_){return {phone:'',thread:''}}
  };
  const trustedRowInfo=row=>{
    const vals=[],attrs=['href','data-item-id','data-thread-id','data-conversation-id'];
    const add=el=>{for(const a of attrs){try{const v=el?.getAttribute?.(a);if(v)vals.push(v)}catch(_){}}};
    add(row);
    let els=[]; try{els=[...(row?.querySelectorAll?.('a[href*="/messages"],[data-item-id],[data-thread-id],[data-conversation-id]')||[])]}catch(_){}
    for(const el of els){add(el);if(vals.length>160)break}
    for(const v of vals){
      const u=voiceURL(v); if(u.thread)return u;
      const p=itemPhones(v); if(p.length===1)return {phone:p[0],thread:threadForPhone(p[0])};
    }
    return {phone:'',thread:''};
  };

  const previewSelector='[data-message-text],[data-message-snippet],[data-last-message],[class*="snippet" i],[class*="preview" i],[class*="last-message" i],[class*="message-text" i]';
  const isConversationRow=el=>!!el&&(
    el.matches?.('gv-conversation-list-item,gv-message-list-item,gv-thread-list-item')||
    /conversation-(?:list-)?item|conversation-row|thread-(?:list-)?item|thread-row|message-(?:list-)?item|message-row|voice-thread-row/i.test(String(el.className||''))
  );
  const rows=()=>{
    const out=[],seen=new Set(),add=el=>{if(el&&!seen.has(el)){seen.add(el);out.push(el)}};
    const sels=['gv-conversation-list-item','gv-message-list-item','gv-thread-list-item','[class*="conversation-list-item" i]','[class*="conversation-row" i]','[class*="thread-list-item" i]','[class*="thread-row" i]','[class*="voice-thread-row" i]','[data-conversation-id][role="listitem"]','[data-thread-id][role="listitem"]','[data-item-id][role="listitem"]'];
    for(const sel of sels){let list=[];try{list=document.querySelectorAll(sel)}catch(_){}for(const el of list)add(el)}
    let generic=[];try{generic=document.querySelectorAll('[role="listitem"]')}catch(_){}
    for(const el of generic){let p=null;try{p=el.querySelector?.(previewSelector)}catch(_){}if(p||isConversationRow(el))add(el)}
    return out;
  };
  const bodyOf=row=>{
    let preferred=null;try{preferred=row.querySelector?.(previewSelector)}catch(_){}
    const v=norm(preferred?.innerText||preferred?.textContent||preferred?.getAttribute?.('aria-label')||'');
    if(v&&!/^messages?$/i.test(v))return v;
    const lines=String(row?.innerText||row?.textContent||'').split(/\n+/).map(norm).filter(Boolean);
    const skip=/^(messages?|unread|read|today|yesterday|now|just now|\d{1,2}:\d{2}\s*(am|pm)?)$/i;
    for(let i=lines.length-1;i>=0;i--)if(!skip.test(lines[i]))return lines[i];
    return '';
  };

  const bodyMatch=(value,body)=>{
    const a=norm(value),b=norm(body);
    return !!a&&!!b&&(a===b||a.includes(b));
  };
  const candidateFromJSON=(root,body)=>{
    let best=null,bestDepth=-1,visited=0;
    const walk=(node,depth,key)=>{
      if(node==null||visited++>16000)return {hasBody:false,ids:new Set()};
      if(typeof node==='string'){
        const ids=new Set(itemPhones(node));
        const keyed=identityPhone(key,node);if(keyed)ids.add(keyed);
        return {hasBody:bodyMatch(node,body),ids};
      }
      if(typeof node!=='object')return {hasBody:false,ids:new Set()};
      const summary={hasBody:false,ids:new Set()};
      let entries=[];try{entries=Array.isArray(node)?node.map((v,i)=>[String(i),v]):Object.entries(node)}catch(_){return summary}
      for(const [k,v] of entries){
        if(typeof v==='string'){const keyed=identityPhone(k,v);if(keyed)summary.ids.add(keyed)}
        const child=walk(v,depth+1,k);
        if(child.hasBody)summary.hasBody=true;
        for(const p of child.ids)summary.ids.add(p);
      }
      if(summary.hasBody&&summary.ids.size===1&&depth>=bestDepth){
        const phone=[...summary.ids][0];
        best={phone,thread:threadForPhone(phone)};
        bestDepth=depth;
      }
      return summary;
    };
    walk(root,0,'');
    return best;
  };
  const candidateFromText=(text,body)=>{
    text=String(text||'');body=norm(body);
    if(!text||!body||text.length>4*1024*1024)return null;
    let root=null;try{root=JSON.parse(text.replace(/^\)\]\}'\s*\n?/,'').trim())}catch(_){}
    if(root!=null){const c=candidateFromJSON(root,body);if(c)return c}
    const idx=text.lastIndexOf(body);
    if(idx<0)return null;
    const lo=Math.max(0,idx-5000),hi=Math.min(text.length,idx+body.length+5000);
    const window=text.slice(lo,hi);
    const ids=new Set(itemPhones(window));
    const keyedRe=/"(?:phone(?:Number)?|number|e164|participant(?:Id)?|sender(?:Id)?|address)"\s*:\s*"([^"]{1,100})"/gi;
    let m;while((m=keyedRe.exec(window))!==null){const p=identityPhone('phone',m[1]);if(p)ids.add(p)}
    if(ids.size!==1)return null;
    const phone=[...ids][0];
    return {phone,thread:threadForPhone(phone)};
  };
  const cacheResponse=(text,url)=>{
    text=String(text||'');if(!text||text.length>4*1024*1024)return;
    state.responses.push({text,url:String(url||''),at:Date.now()});
    if(state.responses.length>40)state.responses.splice(0,state.responses.length-40);
    state.networkAt=Date.now();
  };
  const networkInfoForBody=body=>{
    const cutoff=Date.now()-45000;
    for(let i=state.responses.length-1;i>=0;i--){
      const x=state.responses[i];if(x.at<cutoff)continue;
      const c=candidateFromText(x.text,body);if(c)return c;
    }
    return {phone:'',thread:''};
  };

  const hookFetch=()=>{
    const native=globalThis.fetch;if(typeof native!=='function'||native.__flipAiWrapped)return;
    const wrapped=async function(...args){
      const res=await native.apply(this,args);
      try{
        const clone=res.clone(),url=String(res.url||args[0]?.url||args[0]||'');
        clone.text().then(t=>cacheResponse(t,url)).catch(()=>{});
      }catch(_){}
      return res;
    };
    try{Object.defineProperty(wrapped,'__flipAiWrapped',{value:true})}catch(_){}
    globalThis.fetch=wrapped;
  };
  const hookXHR=()=>{
    const X=globalThis.XMLHttpRequest;if(!X||X.prototype.__flipAiWrapped)return;
    const open=X.prototype.open,send=X.prototype.send;
    X.prototype.open=function(method,url,...rest){this.__flipAiURL=String(url||'');return open.call(this,method,url,...rest)};
    X.prototype.send=function(...args){
      try{this.addEventListener('load',()=>{try{
        let t='';
        if(this.responseType===''||this.responseType==='text')t=this.responseText||'';
        else if(this.responseType==='json')t=JSON.stringify(this.response);
        if(t)cacheResponse(t,this.__flipAiURL||this.responseURL||'');
      }catch(_){}})}catch(_){}
      return send.apply(this,args);
    };
    try{Object.defineProperty(X.prototype,'__flipAiWrapped',{value:true})}catch(_){}
  };
  const hookWebSocket=()=>{
    const Native=globalThis.WebSocket;if(typeof Native!=='function'||Native.__flipAiWrapped)return;
    const Wrapped=new Proxy(Native,{construct(Target,args,newTarget){
      const ws=Reflect.construct(Target,args,newTarget);
      try{ws.addEventListener('message',ev=>{
        try{
          if(typeof ev.data==='string')cacheResponse(ev.data,args[0]||'websocket');
          else if(ev.data instanceof Blob)ev.data.text().then(t=>cacheResponse(t,args[0]||'websocket')).catch(()=>{});
        }catch(_){}
      })}catch(_){}
      return ws;
    }});
    try{Object.defineProperty(Wrapped,'__flipAiWrapped',{value:true})}catch(_){}
    globalThis.WebSocket=Wrapped;
  };
  const hookEventSource=()=>{
    const Native=globalThis.EventSource;if(typeof Native!=='function'||Native.__flipAiWrapped)return;
    const Wrapped=new Proxy(Native,{construct(Target,args,newTarget){
      const es=Reflect.construct(Target,args,newTarget);
      try{es.addEventListener('message',ev=>{if(typeof ev.data==='string')cacheResponse(ev.data,args[0]||'eventsource')})}catch(_){}
      return es;
    }});
    try{Object.defineProperty(Wrapped,'__flipAiWrapped',{value:true})}catch(_){}
    globalThis.EventSource=Wrapped;
  };
  const installHooks=()=>{
    try{hookFetch()}catch(_){}
    try{hookXHR()}catch(_){}
    try{hookWebSocket()}catch(_){}
    try{hookEventSource()}catch(_){}
  };
  installHooks();

  const emit=(info,body)=>{
    const payload=JSON.stringify({sender:info?.phone||'',thread:info?.thread||'',body:norm(body),at:new Date().toISOString()});
    try{if(typeof globalThis.flipVoiceSMS==='function')globalThis.flipVoiceSMS(payload)}catch(_){}
  };
  const resolveBackground=async(row,body,initial)=>{
    let info=initial||trustedRowInfo(row);
    if(info.phone&&info.thread){emit(info,body);return}
    for(let i=0;i<60;i++){
      info=networkInfoForBody(body);if(info.phone&&info.thread){emit(info,body);return}
      const rowInfo=trustedRowInfo(row);if(rowInfo.phone&&rowInfo.thread){emit(rowInfo,body);return}
      await new Promise(r=>setTimeout(r,100));
    }
    emit({phone:'',thread:''},body);
  };
  const detectorHeartbeat=rowCount=>{
    const pageText=norm(document.body?.innerText||'').slice(0,5000);
    const empty=/\b(no messages|no conversations|nothing here yet)\b/i.test(pageText);
    globalThis.__flipAiGoogleVoiceSMSDetectorAt=Date.now();
    globalThis.__flipAiGoogleVoiceSMSDetectorRows=rowCount;
    globalThis.__flipAiGoogleVoiceSMSNetworkAt=state.networkAt;
    globalThis.__flipAiGoogleVoiceSMSDetectorReady=(rowCount>0||empty)&&globalThis.fetch?.__flipAiWrapped===true;
  };
  const installStatusGate=()=>{
    if(globalThis.__flipAiGoogleVoiceSMSStatusGated)return;
    const bridge=globalThis.flipGoogleVoiceSMSStatus;if(typeof bridge!=='function')return;
    globalThis.__flipAiGoogleVoiceSMSStatusGated=true;
    globalThis.flipGoogleVoiceSMSStatus=(signed,pageReady,href)=>{
      const fresh=Date.now()-Number(globalThis.__flipAiGoogleVoiceSMSDetectorAt||0)<2500;
      const ready=globalThis.__flipAiGoogleVoiceSMSDetectorReady===true&&fresh;
      return bridge(signed,!!pageReady&&ready,href);
    };
  };
  function scan(){
    if(String(location.hostname||'').toLowerCase()!=='voice.google.com')return;
    installHooks();
    const now=Date.now();for(const [k,t] of state.recent)if(now-t>30000)state.recent.delete(k);
    const list=rows();detectorHeartbeat(list.length);installStatusGate();
    const active=new Set();let index=0;
    for(const row of list){
      const info=trustedRowInfo(row),body=bodyOf(row);
      const stable=info.thread||norm(row.getAttribute?.('data-thread-id')||row.getAttribute?.('data-conversation-id')||row.getAttribute?.('data-item-id')||row.getAttribute?.('aria-label')||'')||('row-'+index);
      const sig=(info.phone||'unresolved')+'\u0000'+(info.thread||'no-thread')+'\u0000'+body;
      const old=state.rows.get(stable)||'';state.rows.set(stable,sig);active.add(stable);index++;
      if(!state.armed||!body||sig===old||/^you\s*:/i.test(body)||state.recent.has(sig)||state.pending.has(sig))continue;
      state.recent.set(sig,now);state.pending.add(sig);
      state.chain=state.chain.then(()=>resolveBackground(row,body,info)).catch(()=>emit(info,body)).finally(()=>state.pending.delete(sig));
    }
    for(const key of [...state.rows.keys()])if(!active.has(key))state.rows.delete(key);
    state.armed=true;
  }
  const obs=new MutationObserver(()=>{clearTimeout(globalThis.__flipAiDirectSMSTimer);globalThis.__flipAiDirectSMSTimer=setTimeout(scan,60)});
  const start=()=>{
    installStatusGate();
    try{obs.observe(document.documentElement,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['aria-label','title','href','class','data-item-id','data-thread-id','data-conversation-id','data-message-text','data-message-snippet','data-last-message','value']})}catch(_){}
    scan();setInterval(scan,700);
  };
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})()
`
