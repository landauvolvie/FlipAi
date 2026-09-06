package main

// googleVoiceSMSInitScript observes the Messages surface of FlipAi's dedicated
// signed-in Google Voice SMS WebView. Real Google Voice conversation links use
// /messages?itemId=t.%2B1XXXXXXXXXX, and the preview text may be a sibling of
// that link rather than a child of the anchor. The detector therefore anchors
// identity on the same-site conversation URL, then expands only to the smallest
// surrounding conversation tile that owns the preview.
//
// The first scan establishes a baseline; only later DOM changes are forwarded,
// so opening FlipAi cannot replay old texts. Outgoing rows beginning with
// "You:" are ignored here and again in Go.
const googleVoiceSMSInitScript = `
(() => {
  if (globalThis.__flipAiDirectSMSInstalled) return;
  globalThis.__flipAiDirectSMSInstalled = true;
  const state = {armed:false, rows:new Map(), recent:new Map()};
  const norm = v => String(v||'').replace(/\s+/g,' ').trim();
  const digits = v => {
    const m=String(v||'').match(/(?:\+?1[\s().-]*)?(?:\d[\s().-]*){10}/g)||[];
    for(const x of m){ const d=x.replace(/\D/g,'').replace(/^1(?=\d{10}$)/,''); if(d.length===10)return d; }
    return '';
  };
  const voiceThread = link => {
    if(!link)return {thread:'',phone:''};
    let raw='';try{raw=String(link.getAttribute?.('href')||'').trim()}catch(_){}
    if(!raw)return {thread:'',phone:''};
    try{
      const u=new URL(raw,location.href);
      if(u.protocol!=='https:'||u.hostname.toLowerCase()!=='voice.google.com')return {thread:'',phone:''};
      const path=String(u.pathname||'');
      const base=path.match(/^\/u\/(\d+)\/messages\/?$/i);
      if(base){
        const keys=[...u.searchParams.keys()];
        const vals=u.searchParams.getAll('itemId');
        if(keys.length!==1||keys[0]!=='itemId'||vals.length!==1)return {thread:'',phone:''};
        const item=vals[0];
        const m=item.match(/^t\.\+1(\d{10})$/);
        if(!m)return {thread:'',phone:''};
        const clean='/u/'+base[1]+'/messages?itemId='+encodeURIComponent(item);
        return {thread:clean,phone:m[1]};
      }
      if(/^\/u\/\d+\/messages\/[^/?#]+$/i.test(path) && !u.search && !u.hash && !path.includes('..')){
        return {thread:path,phone:''};
      }
    }catch(_){}
    return {thread:'',phone:''};
  };
  const messageLinks = () => {
    let list=[];try{list=document.querySelectorAll('a[href*="/messages"]')}catch(_){}
    return [...list].filter(x=>!!voiceThread(x).thread);
  };
  const previewSelector='[data-message-text],[data-message-snippet],[data-last-message],[class*="snippet" i],[class*="preview" i],[class*="last-message" i],[class*="message-text" i]';
  const semanticRow = el => !!el && (
    el.matches?.('gv-conversation-list-item,gv-message-list-item,[role="listitem"],[data-conversation-id],[data-thread-id],[data-item-id]') ||
    /conversation|message-row|thread-row/i.test(String(el.className||''))
  );
  const validLinksInside = el => {
    if(!el)return [];
    const out=[];
    if(el.matches?.('a[href*="/messages"]') && voiceThread(el).thread)out.push(el);
    let links=[];try{links=el.querySelectorAll?.('a[href*="/messages"]')||[]}catch(_){}
    for(const a of links){if(voiceThread(a).thread&&!out.includes(a))out.push(a)}
    return out;
  };
  // In current Voice markup the contact/name anchor and the latest-message
  // preview can be siblings. Walk upward from the trusted conversation link,
  // but never cross into a container holding multiple conversation links.
  const containerOf = link => {
    let best=link, node=link;
    for(let depth=0;depth<7;depth++){
      if(!node)break;
      const links=validLinksInside(node);
      if(links.length>1)break;
      const text=norm(node.innerText||node.textContent||'');
      let preview=null;try{preview=node.querySelector?.(previewSelector)}catch(_){}
      if(preview && text.length<6000) return node;
      if((semanticRow(node)||text.length<1200) && links.length===1)best=node;
      node=node.parentElement;
    }
    return best;
  };
  const rows = () => {
    const out=[]; const seen=new Set();
    for(const link of messageLinks()){
      const row=containerOf(link);
      if(row&&!seen.has(row)){seen.add(row);out.push(row)}
    }
    return out;
  };
  // Sender identity is taken first from the Google Voice itemId URL. A saved
  // contact name may replace the visible number, and a message body may itself
  // contain unrelated phone numbers, so arbitrary descendant text is never an
  // identity source.
  const phoneOf = row => {
    const links=validLinksInside(row);
    for(const link of links){const info=voiceThread(link);if(info.phone)return info.phone}
    const candidates=[];
    const identityAttrs=['title','aria-label','href','data-phone','data-number','data-e164','value'];
    const rowAttrs=['title','data-phone','data-number','data-e164'];
    const addAttrs=(el,attrs)=>{for(const a of attrs){try{const v=el.getAttribute?.(a);if(v)candidates.push(v)}catch(_){}}};
    if(row.matches?.('a[href*="/messages"]')) addAttrs(row,identityAttrs); else addAttrs(row,rowAttrs);
    let identities=[];try{identities=row.querySelectorAll?.('[class*="contact" i],[class*="sender" i],[class*="recipient" i],[data-contact],[data-recipient],[data-phone],[data-number],[data-e164]')||[]}catch(_){}
    for(const el of identities){
      addAttrs(el,identityAttrs);
      const v=norm(el.innerText||el.textContent||'');if(v)candidates.push(v);
      if(candidates.length>220)break;
    }
    for(const v of candidates){const p=digits(v);if(p)return p;}
    return '';
  };
  const threadOf = row => {
    for(const link of validLinksInside(row)){const info=voiceThread(link);if(info.thread)return info.thread}
    return '';
  };
  const bodyOf = (row,text,phone) => {
    let preferred=null;try{preferred=row.querySelector?.(previewSelector)}catch(_){}
    let v=norm(preferred?.innerText||preferred?.textContent||preferred?.getAttribute?.('aria-label')||'');
    if(v && !/^messages?$/i.test(v)) return v;
    const lines=String(row.innerText||text||'').split(/\n+/).map(norm).filter(Boolean);
    const skip=/^(messages?|unread|read|today|yesterday|now|just now|\d{1,2}:\d{2}\s*(am|pm)?)$/i;
    for(let i=lines.length-1;i>=0;i--){
      const line=lines[i];
      if(skip.test(line)||line===phone||digits(line)===phone)continue;
      return line;
    }
    return '';
  };
  function scan(){
    if(location.hostname.toLowerCase()!=='voice.google.com') return;
    const now=Date.now();
    for(const [k,t] of state.recent){if(now-t>30000)state.recent.delete(k)}
    const active=new Set();
    for(const row of rows()){
      const text=norm((row.getAttribute?.('aria-label')||'')+' '+(row.innerText||row.textContent||''));
      const phone=phoneOf(row);
      const thread=threadOf(row);
      const body=bodyOf(row,text,phone);
      const key=thread||('row:'+phone+':'+text.slice(0,120));
      const sig=(phone||'unresolved')+'\u0000'+(thread||'no-thread')+'\u0000'+body;
      const old=state.rows.get(key)||'';
      state.rows.set(key,sig);active.add(key);
      if(!state.armed||!body||sig===old||/^you\s*:/i.test(body))continue;
      if(state.recent.has(sig))continue;
      state.recent.set(sig,now);
      // Send unresolved identities too. Go logs them as blocked rather than
      // silently hiding an inbound text from Activity.
      const payload=JSON.stringify({sender:phone,thread:thread,body:body,at:new Date().toISOString()});
      try{ if(typeof globalThis.flipVoiceSMS==='function') globalThis.flipVoiceSMS(payload); }catch(_){}
    }
    for(const key of [...state.rows.keys()]){if(!active.has(key))state.rows.delete(key)}
    state.armed=true;
  }
  const obs=new MutationObserver(()=>{clearTimeout(globalThis.__flipAiDirectSMSTimer);globalThis.__flipAiDirectSMSTimer=setTimeout(scan,80)});
  const start=()=>{try{obs.observe(document.documentElement,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['aria-label','title','href','class','data-phone','data-number','data-e164','data-message-text','data-message-snippet','data-last-message','value']})}catch(_){} scan(); setInterval(scan,800)};
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})()`
