package main

// googleVoiceSMSInitScript observes only the Messages surface of FlipAi's own
// signed-in Google Voice WebView. The first scan establishes a baseline; only
// later DOM changes are forwarded, so opening FlipAi cannot replay old texts.
// Outgoing rows beginning with "You:" are ignored here and again in Go.
const googleVoiceSMSInitScript = `
(() => {
  if (globalThis.__flipAiDirectSMSInstalled) return;
  globalThis.__flipAiDirectSMSInstalled = true;
  const state = {armed:false, rows:new WeakMap(), recent:new Map()};
  const norm = v => String(v||'').replace(/\s+/g,' ').trim();
  const digits = v => {
    const m=String(v||'').match(/(?:\+?1[\s().-]*)?(?:\d[\s().-]*){10}/g)||[];
    for(const x of m){ const d=x.replace(/\D/g,'').replace(/^1(?=\d{10}$)/,''); if(d.length===10)return d; }
    return '';
  };
  const visible = el => !!el && !!(el.offsetWidth||el.offsetHeight||el.getClientRects().length);
  const rows = () => {
    const sels=['gv-conversation-list-item','gv-message-list-item','[role="listitem"]','a[href*="/messages/"]'];
    const out=[]; const seen=new Set();
    for(const sel of sels){
      let list=[]; try{list=document.querySelectorAll(sel)}catch(_){}
      for(const el of list){ if(!seen.has(el)&&visible(el)){seen.add(el);out.push(el)} }
    }
    return out;
  };
  // A saved contact name may replace the visible number. Sender identity comes
  // only from the conversation link itself or dedicated contact/sender fields.
  // Never scan arbitrary descendants: an SMS body can contain a phone number,
  // a tel: link, or a titled link and none of those may become the sender.
  const phoneOf = row => {
    const candidates=[];
    const identityAttrs=['title','aria-label','href','data-phone','data-number','data-e164','value'];
    const rowAttrs=['title','data-phone','data-number','data-e164'];
    const addAttrs=(el,attrs)=>{for(const a of attrs){try{const v=el.getAttribute?.(a);if(v)candidates.push(v)}catch(_){}}};
    if(row.matches?.('a[href*="/messages/"]')) addAttrs(row,identityAttrs); else addAttrs(row,rowAttrs);
    let conversation=null;try{conversation=row.matches?.('a[href*="/messages/"]')?row:row.querySelector?.('a[href*="/messages/"]')}catch(_){}
    if(conversation)addAttrs(conversation,identityAttrs);
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
    let link=null;
    try{link=row.matches?.('a[href*="/messages/"]')?row:row.querySelector?.('a[href*="/messages/"]')}catch(_){}
    if(!link)return '';
    let raw='';try{raw=String(link.getAttribute('href')||'').trim()}catch(_){}
    if(!raw)return '';
    if(raw.startsWith('/'))return raw.split(/[?#]/,1)[0];
    try{
      const u=new URL(raw,location.href);
      if(u.hostname.toLowerCase()!=='voice.google.com')return '';
      return u.pathname;
    }catch(_){return ''}
  };
  const bodyOf = (row,text,phone) => {
    const preferred=row.querySelector?.('[data-message-text],[class*="snippet"],[class*="message-text"],[aria-label*="message" i]');
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
    for(const row of rows()){
      const text=norm((row.getAttribute?.('aria-label')||'')+' '+(row.innerText||row.textContent||''));
      const phone=phoneOf(row);
      const thread=threadOf(row);
      const body=bodyOf(row,text,phone);
      const sig=(phone||'unresolved')+'\u0000'+(thread||'no-thread')+'\u0000'+body;
      const old=state.rows.get(row)||'';
      state.rows.set(row,sig);
      if(!state.armed||!body||sig===old||/^you\s*:/i.test(body))continue;
      const recentKey=sig;
      if(state.recent.has(recentKey))continue;
      state.recent.set(recentKey,now);
      // Send unresolved identities too. Go logs them as blocked rather than
      // silently hiding an inbound text from Activity.
      const payload=JSON.stringify({sender:phone,thread:thread,body:body,at:new Date().toISOString()});
      try{ if(typeof globalThis.flipVoiceSMS==='function') globalThis.flipVoiceSMS(payload); }catch(_){}
    }
    state.armed=true;
  }
  const obs=new MutationObserver(()=>{clearTimeout(globalThis.__flipAiDirectSMSTimer);globalThis.__flipAiDirectSMSTimer=setTimeout(scan,80)});
  const start=()=>{try{obs.observe(document.documentElement,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['aria-label','title','href','class','data-phone','data-number','data-e164','value']})}catch(_){} scan(); setInterval(scan,800)};
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})()`
