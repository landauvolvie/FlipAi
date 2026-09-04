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
      const phone=digits(text+' '+(row.getAttribute?.('href')||''));
      const body=bodyOf(row,text,phone);
      const sig=phone+'\u0000'+body;
      const old=state.rows.get(row)||'';
      state.rows.set(row,sig);
      if(!state.armed||!phone||!body||sig===old||/^you\s*:/i.test(body))continue;
      const recentKey=phone+'\u0000'+body;
      if(state.recent.has(recentKey))continue;
      state.recent.set(recentKey,now);
      const payload=JSON.stringify({sender:phone,body:body,at:new Date().toISOString()});
      try{ if(typeof globalThis.flipVoiceSMS==='function') globalThis.flipVoiceSMS(payload); }catch(_){}
    }
    state.armed=true;
  }
  const obs=new MutationObserver(()=>{clearTimeout(globalThis.__flipAiDirectSMSTimer);globalThis.__flipAiDirectSMSTimer=setTimeout(scan,80)});
  const start=()=>{try{obs.observe(document.documentElement,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['aria-label','class']})}catch(_){} scan(); setInterval(scan,800)};
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start,{once:true});else start();
})()`
