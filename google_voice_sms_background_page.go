package main

// googleVoiceSMSBackgroundInitScript is the direct-SMS listener used by the
// dedicated Google Voice SMS WebView. It never opens, selects, or clicks a
// conversation. The visible window is only for first-time sign-in; after that
// this script stays on the Messages list in FlipAi's hidden worker and learns
// the exact t.+1XXXXXXXXXX sender/thread identity from Google Voice's own
// background fetch/XHR response data. Contact names and phone numbers written
// inside message bodies are never sender identity.
const googleVoiceSMSBackgroundInitScript = `
(() => {
  if (globalThis.__flipAiDirectSMSBackgroundInstalled) return;
  globalThis.__flipAiDirectSMSBackgroundInstalled = true;

  const state = {
    armed:false,
    rows:new Map(),
    recent:new Map(),
    pending:new Set(),
    learned:[],
    networkAt:0,
    chain:Promise.resolve()
  };
  const norm=v=>String(v||'').replace(/\s+/g,' ').trim();
  const accountSlot=()=> (String(location.pathname||'').match(/^\/u\/(\d+)/i)||[])[1]||'0';
  const threadForPhone=phone=>phone?('/u/'+accountSlot()+'/messages?itemId='+encodeURIComponent('t.+1'+phone)):'';
  const itemPhones=v=>{
    const out=new Set();
    let s=String(v||'');
    try{s=decodeURIComponent(s)}catch(_){}
    const re=/(?:^|[^A-Za-z0-9])t\.\+1(\d{10})(?:$|[^0-9])/g;
    let m;while((m=re.exec(s))!==null)out.add(m[1]);
    return [...out];
  };
  const itemInfo=v=>{
    const p=itemPhones(v);
    return p.length===1?{phone:p[0],thread:threadForPhone(p[0])}:{phone:'',thread:''};
  };
  const voiceURL=raw=>{
    raw=String(raw||'').trim();if(!raw)return {phone:'',thread:''};
    try{
      const u=new URL(raw,location.href);
      if(u.protocol!=='https:'||u.hostname.toLowerCase()!=='voice.google.com'||u.hash)return {phone:'',thread:''};
      const base=String(u.pathname||'').match(/^\/u\/(\d+)\/messages\/?$/i);
      if(!base)return {phone:'',thread:''};
      const keys=[...u.searchParams.keys()], vals=u.searchParams.getAll('itemId');
      if(keys.length!==1||keys[0]!=='itemId'||vals.length!==1)return {phone:'',thread:''};
      const m=vals[0].match(/^t\.\+1(\d{10})$/);if(!m)return {phone:'',thread:''};
      return {phone:m[1],thread:'/u/'+base[1]+'/messages?itemId='+encodeURIComponent(vals[0])};
    }catch(_){return {phone:'',thread:''}}
  };

  // The list row may itself carry an exact Voice itemId. This is trusted. We
  // intentionally do not scrape visible contact names/numbers from list text.
  const trustedRowInfo=row=>{
    const vals=[];
    const attrs=['href','data-item-id','data-thread-id','data-conversation-id'];
    const add=el=>{for(const a of attrs){try{const v=el?.getAttribute?.(a);if(v)vals.push(v)}catch(_){}}};
    add(row);
    let els=[];try{els=[...(row?.querySelectorAll?.('a[href*="/messages"],[data-item-id],[data-thread-id],[data-conversation-id]')||[])]}catch(_){}
    for(const el of els){add(el);if(vals.length>160)break}
    for(const v of vals){const u=voiceURL(v);if(u.thread)return u;const i=itemInfo(v);if(i.thread)return i}
    return {phone:'',thread:''};
  };

  const previewSelector='[data-message-text],[data-message-snippet],[data-last-message],[class*="snippet" i],[class*="preview" i],[class*="last-message" i],[class*="message-text" i]';
  const isConversationRow=el=>!!el&&(
    el.matches?.('gv-conversation-list-item,gv-message-list-item,gv-thread-list-item')||
    /conversation-(?:list-)?item|conversation-row|thread-(?:list-)?item|thread-row|message-(?:list-)?item|message-row|voice-thread-row/i.test(String(el.className||''))
  );
  const rows=()=>{
    const out=[],seen=new Set();const add=el=>{if(el&&!seen.has(el)){seen.add(el);out.push(el)}};
    const sels=['gv-conversation-list-item','gv-message-list-item','gv-thread-list-item','[class*="conversation-list-item" i]','[class*="conversation-row" i]','[class*="thread-list-item" i]','[class*="thread-row" i]','[class*="voice-thread-row" i]','[data-conversation-id][role="listitem"]','[data-thread-id][role="listitem"]','[data-item-id][role="listitem"]'];
    for(const sel of sels){let list=[];try{list=document.querySelectorAll(sel)}catch(_){}for(const el of list)add(el)}
    let generic=[];try{generic=document.querySelectorAll('[role="listitem"]')}catch(_){}
    for(const el of generic){let p=null;try{p=el.querySelector?.(previewSelector)}catch(_){}if(p||isConversationRow(el))add(el)}
    return out;
  };
  const bodyOf=row=>{
    let preferred=null;try{preferred=row.querySelector?.(previewSelector)}catch(_){}
    let v=norm(preferred?.innerText||preferred?.textContent||preferred?.getAttribute?.('aria-label')||'');
    if(v&&!/^messages?$/i.test(v))return v;
    const lines=String(row?.innerText||row?.textContent||'').split(/\n+/).map(norm).filter(Boolean);
    const skip=/^(messages?|unread|read|today|yesterday|now|just now|\d{1,2}:\d{2}\s*(am|pm)?)$/i;
    for(let i=lines.length-1;i>=0;i--){if(!skip.test(lines[i]))return lines[i]}
    return '';
  };

  const infoKey=i=>i&&i.phone&&i.thread?i.phone+'\u0000'+i.thread:'';
  const uniqueInfos=infos=>{
    const m=new Map();for(const i of infos||[]){const k=infoKey(i);if(k)m.set(k,i)}return [...m.values()];
  };
  const stringHasBody=(s,body)=>{
    s=norm(s);body=norm(body);if(!s||!body)return false;
    return s===body||s.includes(body)||body.includes(s);
  };

  // Search the smallest JSON subtree that contains the exact message text and
  // exactly one Google Voice t.+1 identity. This associates a list-preview
  // update with its sender without opening the conversation.
  const candidateFromJSON=(root,body)=>{
    let best=null,bestDepth=-1,seen=0;
    const walk=(node,depth)=>{
      if(node==null||seen++>12000)return {body:false,phones:new Set()};
      if(typeof node==='string')return {body:stringHasBody(node,body),phones:new Set(itemPhones(node))};
      if(typeof node!=='object')return {body:false,phones:new Set()};
      const summary={body:false,phones:new Set()};
      let vals=[];try{vals=Array.isArray(node)?node:Object.values(node)}catch(_){return summary}
      for(const v of vals){const child=walk(v,depth+1);if(child.body)summary.body=true;for(const p of child.phones)summary.phones.add(p)}
      if(summary.body&&summary.phones.size===1&&depth>=bestDepth){const phone=[...summary.phones][0];best={phone,thread:threadForPhone(phone)};bestDepth=depth}
      return summary;
    };
    walk(root,0);return best;
  };
  const candidateFromText=(text,body)=>{
    text=String(text||'');if(!text||text.length>4*1024*1024)return null;
    let parsed=null;try{parsed=JSON.parse(text)}catch(_){}
    if(parsed!=null){const c=candidateFromJSON(parsed,body);if(c)return c}
    // Google batchexecute payloads are not always a single JSON document. A
    // raw fallback is allowed only when the response contains the message text
    // and exactly one distinct t.+1 identity.
    if(!stringHasBody(text,body)&&!text.includes(JSON.stringify(norm(body)).slice(1,-1)))return null;
    const phones=itemPhones(text);if(phones.length!==1)return null;
    return {phone:phones[0],thread:threadForPhone(phones[0])};
  };
  const remember=(body,info)=>{
    body=norm(body);if(!body||!info?.phone||!info?.thread)return;
    state.learned.push({body,phone:info.phone,thread:info.thread,at:Date.now()});
    if(state.learned.length>160)state.learned.splice(0,state.learned.length-160);
    state.networkAt=Date.now();
  };
  const learnNetworkText=(text,url)=>{
    if(!text)return;
    const now=Date.now();
    // Learn all message/identity pairs discoverable from this response by first
    // extracting plausible text strings, then resolving each within its JSON
    // subtree. This keeps repeated identical SMS bodies safe: the latest match
    // is used and still carries an exact t.+1 identity.
    let root=null;try{root=JSON.parse(text)}catch(_){}
    const bodies=[];
    if(root!=null){
      let seen=0;const collect=node=>{
        if(node==null||seen++>12000)return;
        if(typeof node==='string'){const s=norm(node);if(s&&s.length<=2000&&!/^t\.\+1\d{10}$/.test(s))bodies.push(s);return}
        if(typeof node!=='object')return;let vals=[];try{vals=Array.isArray(node)?node:Object.values(node)}catch(_){return}for(const v of vals)collect(v)
      };collect(root);
      const uniq=[...new Set(bodies)].slice(-400);
      for(const b of uniq){const c=candidateFromJSON(root,b);if(c)remember(b,c)}
    }
    if(!root){
      // Batched Google responses: cache the raw payload and let the exact row
      // body resolve against it later. No identity is emitted here by itself.
      state.learned.push({body:'',raw:String(text).slice(0,4*1024*1024),at:now,url:String(url||'')});
      if(state.learned.length>160)state.learned.splice(0,state.learned.length-160);
      state.networkAt=now;
    }
  };
  const networkInfoForBody=body=>{
    body=norm(body);const cutoff=Date.now()-30000;
    for(let i=state.learned.length-1;i>=0;i--){
      const x=state.learned[i];if(x.at<cutoff)continue;
      if(x.body&&x.body===body&&x.phone&&x.thread)return {phone:x.phone,thread:x.thread};
      if(x.raw){const c=candidateFromText(x.raw,body);if(c)return c}
    }
    return {phone:'',thread:''};
  };

  const hookFetch=()=>{
    const native=globalThis.fetch;if(typeof native!=='function'||native.__flipAiWrapped)return;
    const wrapped=async function(...args){
      const res=await native.apply(this,args);
      try{const clone=res.clone();const url=String(res.url||args[0]?.url||args[0]||'');clone.text().then(t=>learnNetworkText(t,url)).catch(()=>{})}catch(_){}
      return res;
    };
    try{Object.defineProperty(wrapped,'__flipAiWrapped',{value:true})}catch(_){}
    globalThis.fetch=wrapped;
  };
  const hookXHR=()=>{
    const X=globalThis.XMLHttpRequest;if(!X||X.prototype.__flipAiWrapped)return;
    const open=X.prototype.open;
    X.prototype.open=function(method,url,...rest){this.__flipAiURL=String(url||'');return open.call(this,method,url,...rest)};
    X.prototype.addEventListener.call(X.prototype,'noop',()=>{});
    const send=X.prototype.send;
    X.prototype.send=function(...args){
      try{this.addEventListener('load',()=>{try{let t='';if(this.responseType===''||this.responseType==='text')t=this.responseText||'';else if(this.responseType==='json')t=JSON.stringify(this.response);if(t)learnNetworkText(t,this.__flipAiURL||this.responseURL||'')}catch(_){}})}catch(_){}
      return send.apply(this,args);
    };
    try{Object.defineProperty(X.prototype,'__flipAiWrapped',{value:true})}catch(_){}
  };
  hookFetch();hookXHR();

  const emit=(info,body)=>{
    const payload=JSON.stringify({sender:info?.phone||'',thread:info?.thread||'',body:norm(body),at:new Date().toISOString()});
    try{if(typeof globalThis.flipVoiceSMS==='function')globalThis.flipVoiceSMS(payload)}catch(_){}
  };
  const resolveBackground=async(row,body,initial)=>{
    let info=initial||trustedRowInfo(row);
    if(info.phone&&info.thread){emit(info,body);return}
    for(let i=0;i<50;i++){
      info=networkInfoForBody(body);if(info.phone&&info.thread){emit(info,body);return}
      const rowInfo=trustedRowInfo(row);if(rowInfo.phone&&rowInfo.thread){emit(rowInfo,body);return}
      await new Promise(r=>setTimeout(r,100));
    }
    // Fail closed. Go will log this as unresolved/blocked; never guess from a
    // contact name or arbitrary visible number.
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
    hookFetch();hookXHR();
    const now=Date.now();for(const [k,t] of state.recent){if(now-t>30000)state.recent.delete(k)}
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
  const start=()=>{installStatusGate();try{obs.observe(document.documentElement,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['aria-label','title','href','class','data-phone','data-phone-number','data-number','data-e164','data-item-id','data-thread-id','data-conversation-id','data-message-text','data-message-snippet','data-last-message','value']})}catch(_){}scan();setInterval(scan,700)};
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})()`
