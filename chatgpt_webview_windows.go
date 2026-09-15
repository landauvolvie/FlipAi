//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

const chatGPTPageMonitorJS = `(function(){
  if(window.__flipAiChatGPTMonitor)return;
  window.__flipAiChatGPTMonitor=true;
  async function signed(){
    try{
      const r=await fetch('/api/auth/session',{credentials:'include',cache:'no-store'});
      if(r.ok){const j=await r.json();if(j&&j.user)return true;}
    }catch(e){}
    return !!document.querySelector('[data-testid="profile-button"],button[aria-label*="Profile"],nav a[href*="/settings"],#prompt-textarea,[data-testid="prompt-textarea"]');
  }
  async function tick(){
    try{ if(window.flipChatGPTStatus) await window.flipChatGPTStatus(await signed(), location.href); }catch(e){}
  }
  setInterval(tick,1000); addEventListener('load',tick); setTimeout(tick,350);
})();`

const chatGPTSignedInJS = `(async()=>{
  try{
    const r=await fetch('/api/auth/session',{credentials:'include',cache:'no-store'});
    if(r.ok){const j=await r.json();if(j&&j.user)return true;}
  }catch(e){}
  return !!document.querySelector('#prompt-textarea,[data-testid="prompt-textarea"],[contenteditable="true"]');
})()`

// Work is a real ChatGPT experience, not a model name. FlipAi must switch the
// page into that experience before it fills the composer. The selector is
// deliberately accessibility/text based rather than coordinate based because
// ChatGPT's DOM classes change frequently.
// chatGPTSelectModeJS carries a marker so the DevTools layer gives it a
// deadline that outlasts its own. It retries ChatGPT's experience picker for
// more than twelve seconds, and the generic page-probe deadline is eight, so a
// slow picker made the call time out before the script had finished trying --
// reported as "the ChatGPT WebView did not answer Runtime.evaluate", with
// ChatGPT never seeing the message at all.
//
// It is deliberately not a browser-turn marker: switching experience is not a
// model turn, and must not start a long-turn watcher or a media scan.
const chatGPTSelectModeJS = `/*` + browserLongPageCallMarker + `*/(async(wanted)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const norm=s=>String(s||'').replace(/\s+/g,' ').trim().toLowerCase();
  const visible=n=>!!n&&n.getClientRects().length>0&&getComputedStyle(n).visibility!=='hidden';
  const stateSelected=n=>{
    if(!n||!n.getAttribute)return false;
    const vals=[n.getAttribute('aria-selected'),n.getAttribute('aria-pressed'),n.getAttribute('aria-checked'),n.getAttribute('aria-current'),n.getAttribute('data-state')].map(norm);
    if(vals.some(v=>v==='true'||v==='active'||v==='on'||v==='checked'||v==='selected'||v==='page'))return true;
    return /(^|\s)(active|selected|current)(\s|$)/i.test(String(n.className||''));
  };
  const selected=n=>{
    for(let cur=n,i=0;cur&&i<5;cur=cur.parentElement,i++)if(stateSelected(cur))return true;
    return false;
  };
  const clickable=n=>{
    if(!n)return null;
    return n.closest&&n.closest('button,a,[role="button"],[role="tab"],[role="menuitem"],[role="option"],[role="link"],[aria-haspopup],[tabindex]')||n;
  };
  const controls=()=>Array.from(document.querySelectorAll('button,a,[role="button"],[role="tab"],[role="menuitem"],[role="option"],[role="link"],[aria-label],[title],[data-testid],[aria-haspopup],[tabindex]')).filter(visible);
  const values=n=>[n&&n.innerText,n&&n.textContent,n&&n.getAttribute&&n.getAttribute('aria-label'),n&&n.getAttribute&&n.getAttribute('title')].map(norm).filter(Boolean);
  const isName=(n,name)=>values(n).some(v=>v===name||v===name+' mode'||v===name+' beta'||v.startsWith(name+' ·')||v.startsWith(name+' -'));
  const named=name=>{
    const found=controls().filter(n=>isName(n,name)).map(clickable);
    const leaves=Array.from(document.querySelectorAll('span,div,p')).filter(n=>visible(n)&&norm(n.textContent)===name).map(clickable);
    return Array.from(new Set(found.concat(leaves))).filter(visible);
  };
  const findSelected=name=>named(name).find(selected)||null;
  const composerReady=()=>!!document.querySelector('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"][data-virtualkeyboard],[contenteditable="true"]');
  wanted=norm(wanted)==='work'?'work':'chat';
  const other=wanted==='work'?'chat':'work';

  // Auth can restore before ChatGPT finishes mounting the fresh composer and
  // experience toggle. Wait for the actual interactive UI, not only auth.
  for(let i=0;i<100&&!composerReady();i++)await sleep(200);

  if(findSelected(wanted))return {ok:true,mode:wanted,href:location.href};

  let target=named(wanted)[0]||null;
  if(!target){
    const current=findSelected(other)||named(other)[0]||null;
    if(current){
      current.click();
      for(let i=0;i<20&&!target;i++){await sleep(150);target=named(wanted)[0]||null;}
    }
  }
  if(!target){
    // Some ChatGPT builds expose Chat/Work through one combined toggle whose
    // children are not buttons until it is opened. Open only a short, explicit
    // control that names both experiences, then look for the exact Work item.
    const picker=controls().find(n=>{
      const v=values(n).join(' ');
      return v.length<100&&/(^|\s)chat(\s|$)/.test(v)&&/(^|\s)work(\s|$)/.test(v);
    });
    if(picker){
      picker.click();
      for(let i=0;i<20&&!target;i++){await sleep(150);target=named(wanted)[0]||null;}
    }
  }
  if(target){
    target.click();
    for(let i=0;i<40;i++){
      await sleep(150);
      if(findSelected(wanted))return {ok:true,mode:wanted,href:location.href};
      const wantedExact=named(wanted), otherExact=named(other);
      if(wantedExact.length===1&&otherExact.length===0)return {ok:true,mode:wanted,href:location.href};
      const href=norm(location.href);
      if(wanted==='work'&&(/(^|[/?#=&_-])work([/?#=&_-]|$)/.test(href)))return {ok:true,mode:wanted,href:location.href};
    }
  }

  if(wanted==='chat'){
    // Never call the page "Chat" merely because Work lacks selected-state
    // attributes. The Work header itself is enough evidence that we must
    // leave Work before an O: command can be sent.
    const workEvidence=named('work');
    if(workEvidence.length===0)return {ok:true,mode:'chat',href:location.href,legacy:true};
  }
  return {ok:false,detail:'FlipAi could not verify ChatGPT '+(wanted==='work'?'Work':'Chat')+' mode. The task was not sent so it cannot accidentally run in the wrong experience.',href:location.href};
})(%s)`

// Prefer ChatGPT's own New chat control for explicit NEW requests. The Work
// shell can keep the sidebar collapsed/off-screen, so this intentionally searches
// mounted controls even when CSS says they are not currently visible. Calling
// HTMLElement.click() still goes through ChatGPT's own React handler.
const chatGPTClickNewChatJS = `(()=>{
  const norm=s=>String(s||'').replace(/\s+/g,' ').trim().toLowerCase();
  const controls=Array.from(document.querySelectorAll('button,a,[role="button"],[role="link"],[aria-label],[title],[data-testid],[href]'));
  const vals=n=>[n.innerText,n.textContent,n.getAttribute&&n.getAttribute('aria-label'),n.getAttribute&&n.getAttribute('title')].map(norm).filter(Boolean);
  const testid=n=>norm(n.getAttribute&&n.getAttribute('data-testid'));
  const href=n=>norm(n.getAttribute&&n.getAttribute('href'));
  let target=controls.find(n=>vals(n).some(v=>v==='new chat'||v==='new conversation'))||null;
  if(!target)target=controls.find(n=>/(^|[-_])(new|create)([-_].*)?chat($|[-_])/.test(testid(n)))||null;
  if(!target)target=controls.find(n=>href(n)==='/'&&(n.closest&&n.closest('nav,aside')||testid(n).includes('chat')))||null;
  if(!target){
    const leaf=Array.from(document.querySelectorAll('span,div,p')).find(n=>norm(n.textContent)==='new chat'||norm(n.textContent)==='new conversation')||null;
    if(leaf)target=leaf.closest&&leaf.closest('button,a,[role="button"],[role="link"]')||leaf;
  }
  if(!target)return false;
  target.click();
  return true;
})()`

// chatGPTTurnJS deliberately uses ChatGPT's own page controls inside FlipAi's
// private WebView. It does not use Windows accessibility, global keyboard/mouse
// input, the user's visible ChatGPT app, or coordinates.
const chatGPTTurnJS = `(async(input)=>{
  // One budget for the whole script, fixed when it starts.
  // Waiting for the composer and then starting a fresh ninety seconds is two
  // budgets end to end: on a slow page that ran past the deadline the DevTools
  // layer allows a turn, and the call was abandoned at ninety-five seconds with
  // the model's answer sitting finished in the page.
  const turnDeadline=Date.now()+82000;
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const clean=s=>String(s||'')
    .replace(/Unable to display this message due to an error\.?\s*Reload the page to try again\.?/gi,' ')
    .replace(/Unable to display this message due to an error\.?/gi,' ')
    .replace(/\s+/g,' ').trim();
  // Source cards are page furniture; the linked words inside a sentence are
  // part of the answer. Matching "citation"/"source"/"card" on any element at
  // all deleted both, so an answer lost the phrases it had linked. Only a
  // block-level container can be a card; an <a> or <span> in a sentence stays.
  const refKeys=['[class*="citation" i]','[class*="source" i]','[class*="reference" i]','[class*="card" i]','[data-testid*="citation" i]','[data-testid*="source" i]','[data-testid*="card" i]'];
  const joinSel=(tags,keys)=>{const out=[];for(const t of tags)for(const k of keys)out.push(t+k);return out.join(',')};
  const refBlockSel=joinSel(['div','aside','section','nav','ul','ol','footer','table','details'],refKeys);
  // Inline reference chrome worth dropping is the bare marker -- a superscript
  // number, "[1]" -- never a linked phrase. Letters mean it is prose.
  const refInlineSel=joinSel(['a','span','cite','sup','small'],refKeys)+',sup';
  // A card is not a sentence. Providers put an offer of connectors to switch on,
  // and a tile for a file they produced, inside the turn; both are controls with
  // words on them, and they were arriving in the text message. Their own buttons
  // are the evidence, so this runs before the buttons are stripped.
  // Narrower than activitySel, on purpose. A streaming answer is commonly
  // announced through [aria-live] or [role=status], and cutting those out of the
  // chosen reply would delete the message itself. The running tool log is not
  // the answer, though, and it was arriving as part of one.
  const activityStripSel='aside,[role="log"],[class*="activity" i],[class*="timeline" i],[class*="step" i],[class*="tool" i],[class*="trace" i],[id*="step" i],[id*="activity" i]';
  const dropCards=clone=>{
    const labelled=(el,word)=>Array.from(el.querySelectorAll('button,a,[role="button"]')).some(b=>new RegExp('^'+word+'$','i').test(String(b.innerText||b.textContent||'').replace(/\s+/g,' ').trim()));
    for(const el of Array.from(clone.querySelectorAll('div,section,aside,article,figure'))){
      if(!clone.contains(el))continue;
      const t=String(el.innerText||el.textContent||'').replace(/\s+/g,' ').trim();
      if(!t)continue;
      if(/^connectors? that could help\b/i.test(t)){el.remove();continue}
      if(t.length<=300&&(labelled(el,'download')||labelled(el,'connect')))el.remove();
    }
  };
  const text=n=>{
    if(!n)return '';
    // ChatGPT renders the normal written answer in a Markdown subtree and rich
    // cards/widgets as sibling UI. SMS must carry the written answer, not the
    // widget's accessibility/visual text (weather grids, charts, controls, etc.).
    const prose=n.querySelector&&n.querySelector('.markdown');
    if(prose)return clean(prose.innerText||prose.textContent||'');
    // Keep a defensive fallback for alternate layouts, but strip common rich UI
    // containers and interactive controls before reading the assistant wrapper.
    const clone=n.cloneNode&&n.cloneNode(true);
    if(clone&&clone.querySelectorAll){
      dropCards(clone);
      clone.querySelectorAll('button,canvas,svg,iframe,[role="button"],[role="toolbar"],[role="menu"],[role="tab"],[role="tabpanel"],[role="slider"],[role="progressbar"],[class*="action" i],[class*="toolbar" i],[class*="footer" i],[data-testid*="widget" i],[data-testid*="weather" i],[data-testid*="chart" i],[data-testid*="carousel" i],[data-testid*="feedback" i],script,style,template,noscript,'+activityStripSel+','+refBlockSel).forEach(el=>el.remove());
      clone.querySelectorAll(refInlineSel).forEach(el=>{if(!/[a-z]/i.test(el.textContent||''))el.remove()});
      // Never let stripping empty a real message: if everything went, a
      // selector matched the answer, and the raw text beats nothing at all.
      return clean(clone.innerText||clone.textContent||'')||clean(n.innerText||n.textContent||'');
    }
    return clean(n.innerText||n.textContent||'');
  };
  // Finding shadow roots means walking every element on the page, and this is
  // called several times per poll. On a large conversation that cost more than
  // the poll interval, so the turn could not finish inside its own deadline and
  // the whole page call was abandoned. Scan once; if the page has no shadow
  // roots -- most do not -- stay on the fast path for the rest of the turn.
  let rootsCache=null,rootsAt=0;
  const roots=()=>{
    const now=Date.now();
    if(rootsCache&&(rootsCache.length===1||now-rootsAt<3000))return rootsCache;
    const out=[document],seen=new Set(out);
    for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}
    rootsCache=out;rootsAt=now;return out;
  };
  // The conversation, not the whole page. A saved task in the sidebar and a
  // suggestion chip under the composer are both on the page and neither is
  // anything the model just said -- one of them was texted as the answer to
  // "hi my friend". Scan <main> when the page has one.
  const scanRoots=()=>{
    const rs=roots();
    const mains=[];
    for(const r of rs){for(const m of r.querySelectorAll('main'))mains.push(m)}
    return mains.length?mains:rs;
  };
  const queryAll=sel=>{const out=[];for(const r of roots())out.push(...r.querySelectorAll(sel));return Array.from(new Set(out))};
  const chromeSel='button,[role="button"],[role="toolbar"],[role="menu"],[class*="action" i],[class*="toolbar" i],[class*="footer" i],'+refBlockSel;
  // Scanning reads text the cheap way; text() above does the careful read and
  // is reserved for the reply FlipAi actually sends. Running the careful one
  // over every candidate on every poll is what made the turn miss its deadline.
  const rawText=n=>String(n&&(n.innerText||n.textContent)||'').trim();
  // The page's own busy line is not an answer, and it was texted in place of one.
  // The page writes its own name into that line, and reading two elements as one
  // runs the name into the next word -- "Museis working" -- so the line is judged
  // by its verb, never by what stands in front of it.
  const statusLine=t=>{
    const v=String(t||'').replace(/\s+/g,' ').trim().toLowerCase();
    if(!v||v.length>60)return false;
    const m=v.match(/(is\s+)?(working|thinking|typing|writing|responding|generating)\b(.*)$/);
    if(!m)return false;
    const head=v.slice(0,v.length-m[0].length).trim();
    const tail=String(m[3]||'').trim();
    // A name or nothing in front of the verb, and nothing of substance after it.
    return head.length<=24&&(tail===''||/^(on it|on that|on your request)[.!\u2026]*$/.test(tail));
  };
  // A short status line is not an answer, and neither is a running tool log.
  const interim=t=>{
    const v=String(t||'').replace(/\s+/g,' ').trim().toLowerCase();
    if(!v)return true;
    if(v.length>80)return false;
    return /^(search|think|reason|analy[sz]|work|generat|load|read|brows|look|check|process|plan|writ|draft|creat|gather|review)(ing|ed)?\b/.test(v)
      ||/^(using|calling|running|opening) \S+/.test(v)
      ||/^(one moment|just a moment|please wait)\b/.test(v);
  };
  // Last resort when ChatGPT names nothing FlipAi recognizes: read the
  // conversation structurally. Without this the turn produced nothing at all
  // and was reported as the model having stopped without answering.
  const activitySel='aside,nav,header,footer,[role="log"],[role="status"],[role="navigation"],[role="complementary"],[role="banner"],[role="contentinfo"],[role="dialog"],[aria-live],[class*="activity" i],[class*="timeline" i],[class*="step" i],[class*="tool" i],[class*="trace" i],[class*="sidebar" i],[class*="suggestion" i],[data-testid*="suggestion" i],[id*="step" i],[id*="activity" i],[id*="sidebar" i]';
  // Bounded and linear. Comparing every block against every other was
  // quadratic, and on a real ChatGPT conversation that cost more per poll than
  // the poll interval, so the turn could not finish inside its own deadline.
  const genericBlocks=()=>{
    const found=[];
    for(const r of scanRoots()){
      for(const n of r.querySelectorAll('div,p,section,article,li,pre')){
        if(n.closest&&(n.closest('form')||n.closest('[contenteditable="true"]')))continue;
        if(n.querySelector&&n.querySelector('textarea,input,[contenteditable="true"]'))continue;
        if(n.matches&&n.matches(chromeSel))continue;
        if(n.closest&&n.closest(chromeSel))continue;
        if(n.closest&&n.closest(activitySel))continue;
        const t=rawText(n);
        if(t.length<2||t.length>20000)continue;
        found.push(n);
      }
    }
    const set=new Set(found),container=new Set();
    for(const n of found){
      let p=n.parentElement;
      for(let hops=0;p&&hops<40;hops++,p=p.parentElement){if(set.has(p))container.add(p)}
    }
    // The newest message is the last one, so keep the tail.
    return found.filter(n=>!container.has(n)).slice(-400);
  };
  // A named match can be a wrapper around every message just as easily as one
  // reply, and nothing dropped it. A candidate that contains another candidate
  // is a container, never a message.
  const dropContainers=list=>{
    if(list.length<2)return list;
    const set=new Set(list),container=new Set();
    for(const n of list){let p=n.parentElement;for(let hops=0;p&&hops<40;hops++,p=p.parentElement){if(set.has(p))container.add(p)}}
    const out=list.filter(n=>!container.has(n));
    return out.length?out:list;
  };
  const users=()=>queryAll('[data-message-author-role="user"]');
  const assistants=()=>{
    const named=dropContainers(queryAll('[data-message-author-role="assistant"]'));
    return named.length?named:genericBlocks();
  };
  const composer=()=>queryAll('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"][data-virtualkeyboard],[contenteditable="true"],textarea')[0]||null;
  const send=()=>queryAll('button[data-testid="send-button"],button[aria-label="Send prompt"],button[aria-label^="Send" i],button[type="submit"]').find(b=>!b.disabled)||null;
  const stop=()=>queryAll('button[data-testid="stop-button"],button[aria-label^="Stop" i]')[0]||null;
  let c=null;
  for(let i=0;i<100&&!c&&Date.now()<turnDeadline-62000;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'ChatGPT is loaded but FlipAi could not find the message composer. The site layout may have changed.',href:location.href};
  const canon=t=>String(t||'').replace(/\s+/g,' ').trim();
  const promptText=canon(input);
  const beforeUserCount=users().length;
  const beforeAssistantCount=assistants().length;
  const beforeTexts=new Set(assistants().map(n=>canon(rawText(n))));
  // Last line of defence. If the node FlipAi matched spans this turn's own
  // prompt, it is a conversation container and not one message, however it was
  // matched -- so everything up to and including the prompt is history, and the
  // answer is what follows.
  const olderSnippets=Array.from(beforeTexts).filter(t=>t.length>=40);
  const spansPrompt=n=>{
    if(!n||!n.querySelectorAll||!promptText)return false;
    for(const e of n.querySelectorAll('div,p,section,article,li,span,pre,td')){if(canon(rawText(e))===promptText)return true}
    return false;
  };
  // The latest boundary that still leaves something after it. The plain last
  // occurrence is not it: an answer often repeats the question, so cutting
  // after that left nothing and the whole history was sent instead.
  const lastUsefulEnd=(v,s)=>{
    if(!s)return -1;
    let at=-1,i=v.indexOf(s);
    for(let guard=0;i>=0&&guard<200;guard++){
      const end=i+s.length;
      if(v.slice(end).trim())at=end;
      i=v.indexOf(s,i+1);
    }
    return at;
  };
  const newestPart=(node,value)=>{
    const v=canon(value);
    if(!v||!spansPrompt(node))return v;
    let cut=0;
    for(const s of [promptText].concat(olderSnippets)){
      const end=lastUsefulEnd(v,s);
      if(end>cut)cut=end;
    }
    const tail=cut>0?v.slice(cut).trim():'';
    return tail||v;
  };
  // A reply is a message, not the one paragraph inside it that happens to be
  // the deepest block. Once ChatGPT put a linked sentence in its own paragraph,
  // the message stopped being a leaf and only that paragraph was sent -- the
  // written answer above it was dropped. Lift the match back to the message.
  const wholeMessage=(n,box)=>{
    // n===box, or a walk that escapes the box, is how the reply became the whole
    // document: the climb ran past the conversation to <html> and every word on
    // the page was sent as the answer.
    if(!n||!box||n===box||!box.contains(n))return n;
    let cur=n;
    while(cur.parentElement&&cur.parentElement!==box&&box.contains(cur.parentElement))cur=cur.parentElement;
    return cur.parentElement===box?cur:n;
  };
  // The conversation is wherever the prompt just landed. It does not move
  // during a turn, so find it once: another full page scan on every poll is
  // what stopped a turn from finishing inside its own deadline.
  let boxCache=null;
  const conversationBox=()=>{
    if(boxCache&&boxCache.isConnected)return boxCache;
    const mine=genericBlocks().filter(n=>canon(rawText(n))===promptText);
    if(!mine.length)return null;
    let box=mine[mine.length-1].parentElement;
    while(box&&box.children.length<2&&box.parentElement)box=box.parentElement;
    boxCache=box;
    return box;
  };
  const assistantForThisTurn=()=>{
    const us=users();
    const as=assistants();
    const newUser=us.length>beforeUserCount?us[us.length-1]:null;
    if(newUser){
      for(let i=as.length-1;i>=0;i--){
        if(newUser.compareDocumentPosition(as[i])&Node.DOCUMENT_POSITION_FOLLOWING)return as[i];
      }
    }
    // Prefer new, non-prompt, non-status text over "whatever is last": the last
    // block on a page is easily a control strip, whose text disappears once its
    // buttons are stripped, leaving an empty reply that never settles.
    const box=conversationBox();
    for(let i=as.length-1;i>=0;i--){
      const t=canon(rawText(as[i]));
      if(!t||t===promptText||beforeTexts.has(t)||interim(t)||statusLine(t))continue;
      const whole=wholeMessage(as[i],box);
      const chosen=canon(rawText(whole))===promptText?as[i]:whole;
      if(!canon(text(chosen)))continue;
      return chosen;
    }
    // A new block appeared but every one of them read as a status. Take the
    // newest that is still not the prompt and not already on screen: without
    // those two checks this branch handed back the user's own message, and the
    // prompt was texted to them as the answer.
    if(as.length>beforeAssistantCount){
      for(let i=as.length-1;i>=0;i--){
        const t=canon(rawText(as[i]));
        if(!t||t===promptText||beforeTexts.has(t)||statusLine(t))continue;
        const whole=wholeMessage(as[i],box);
        const chosen=canon(rawText(whole))===promptText?as[i]:whole;
        if(canon(text(chosen)))return chosen;
      }
    }
    return null;
  };
  c.focus();
  if(c.tagName==='TEXTAREA'||c.tagName==='INPUT'){
    const setter=Object.getOwnPropertyDescriptor(c.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set;
    setter.call(c,input); c.dispatchEvent(new Event('input',{bubbles:true}));
  }else{
    c.innerHTML='';
    const p=document.createElement('p');p.textContent=input;c.appendChild(p);
    c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));
  }
  await sleep(120);
  let b=null;
  for(let i=0;i<50&&!b;i++){b=send();if(!b||b.disabled){b=null;await sleep(100);}}
  if(b){b.click()}
  else{
    // ChatGPT's send control is not always a button FlipAi can name. Enter is
    // how a person sends it, and abandoning the turn here left the prompt
    // typed into the composer and never sent.
    const form=c.closest&&c.closest('form');
    if(form&&typeof form.requestSubmit==='function'){try{form.requestSubmit()}catch(e){}}
    for(const type of ['keydown','keypress','keyup']){
      c.dispatchEvent(new KeyboardEvent(type,{bubbles:true,composed:true,cancelable:true,key:'Enter',code:'Enter',keyCode:13,which:13}));
    }
  }
  let last='',stable=0,started=false;
  // A one-second pause is not proof that a short line is the answer. A status
  // the page shows while it works sits unchanged exactly that long, and it was
  // texted in place of the reply. A real answer of any length still goes out;
  // a short one just has to hold still for three seconds instead of one.
  const settleNeeded=v=>String(v||'').length>=40?5:12;
  const deadline=turnDeadline;/*__FLIPAI_BROWSER_TURN__*/
  while(Date.now()<deadline){
    await sleep(250);
    const node=assistantForThisTurn();
    if(node){
      started=true;
      const now=text(node);
      if(now===last)stable++;else{last=now;stable=0;}
      if(interim(now)){stable=0;continue}
      if(!stop()&&stable>=settleNeeded(now)&&now&&!statusLine(now))return {ok:true,reply:newestPart(node,now),href:location.href};
      // A stale Stop control must not hold a fully settled answer forever.
      if(now&&stable>=32)return {ok:true,reply:newestPart(node,now),href:location.href};
    }
  }
  return {ok:false,detail:started?'ChatGPT started answering but did not finish in time.':'ChatGPT did not produce an assistant response in time.',href:location.href};
})(%s)`

type chatGPTTurnResult struct {
	OK     bool   `json:"ok"`
	Reply  string `json:"reply"`
	Detail string `json:"detail"`
	Href   string `json:"href"`
}

func chatGPTEval(d voiceDevTools, expression string, awaitPromise bool, out any) error {
	if d == nil {
		return errors.New("the ChatGPT WebView has no in-process control channel")
	}
	var got voiceDevToolsEval
	params := map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": awaitPromise}
	if err := d.Call("Runtime.evaluate", params, &got); err != nil {
		return fmt.Errorf("the ChatGPT WebView did not answer Runtime.evaluate: %w", err)
	}
	if len(got.ExceptionDetails) > 0 && string(got.ExceptionDetails) != "null" {
		return browserPageScriptError("ChatGPT", got.ExceptionDetails)
	}
	if out == nil {
		return nil
	}
	if len(got.Result.Value) == 0 {
		return errors.New("the ChatGPT page returned no value")
	}
	if err := json.Unmarshal(got.Result.Value, out); err != nil {
		return fmt.Errorf("the ChatGPT page returned an unreadable value: %w", err)
	}
	return nil
}

func chatGPTPageIsSignedIn(d voiceDevTools) bool {
	var signed bool
	return chatGPTEval(d, chatGPTSignedInJS, true, &signed) == nil && signed
}

func waitForChatGPTPageSignedIn(d voiceDevTools, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if chatGPTPageIsSignedIn(d) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// chatGPTComposerReadyJS asks only whether the page can currently take a
// prompt. A signed-in ChatGPT is not the same thing as a ChatGPT that has
// mounted its composer: the experience switch can leave the page on a shell
// that has neither, and the page driver then fails inside the WebView with the
// message never reaching ChatGPT at all.
const chatGPTComposerReadyJS = `(()=>!!document.querySelector('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"]'))()`

// chatGPTGoHomeJS lands on the canonical ChatGPT root. Navigating destroys the
// execution context this call runs in, so it is never awaited and its failure
// is never an error; whether it worked is decided by waiting for the page.
var chatGPTGoHomeJS = browserPageNavigateJS("https://chatgpt.com/")

// chatGPTPageCouldNotRunScript reports whether the WebView refused to run the
// expression at all -- a destroyed or navigating execution context, which
// surfaces as "Runtime.evaluate failed in the WebView page (0x80070057)".
//
// This is the one failure where FlipAi knows ChatGPT never saw the message:
// the script did not start, so nothing was typed and nothing was sent.
func chatGPTPageCouldNotRunScript(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "did not answer Runtime.evaluate") ||
		strings.Contains(msg, "failed in the WebView page") ||
		strings.Contains(msg, "returned no value")
}

func platformStartChatGPTLogin(dataDir string) error {
	_ = platformStopChatGPTWorker(dataDir)
	waitForChatGPTStopped(dataDir, 4*time.Second)
	mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
		s.LoginActive = true
		s.Starting = true
		s.Running = false
		s.Visible = false
		s.SignedIn = false
		s.LastEvent = "sign-in-window-starting"
		s.LastError = ""
	})
	exe, err := os.Executable()
	if err != nil {
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) { s.LoginActive = false; s.Starting = false })
		return err
	}
	cmd := exec.Command(exe, "--chatgpt-login")
	if err := cmd.Start(); err != nil {
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) { s.LoginActive = false; s.Starting = false; s.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformEnsureChatGPTWorker(dataDir string) error {
	s := loadChatGPTRuntime(dataDir)
	if s.LoginActive && (s.Running || s.Starting) {
		return nil
	}
	if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer cancel()
		if _, code, err := chatGPTControlRequest(ctx, s, http.MethodGet, "/health", nil); err == nil && code == http.StatusOK {
			return nil
		}
	}
	if s.Starting && time.Since(s.UpdatedAt) < 10*time.Second {
		return nil
	}
	mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) {
		v.Starting = true
		v.LoginActive = false
		v.Running = false
		v.Visible = false
		v.SignedIn = false
		v.ControlPort = 0
		v.ControlToken = ""
		v.LastEvent = "background-starting"
		v.LastError = ""
	})
	exe, err := os.Executable()
	if err != nil {
		mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) { v.Starting = false; v.LastError = err.Error() })
		return err
	}
	cmd := exec.Command(exe, "--chatgpt-worker")
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) { v.Starting = false; v.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformStopChatGPTWorker(dataDir string) error {
	s := loadChatGPTRuntime(dataDir)
	for i := 0; i < 20 && s.ControlPort < 1 && s.Starting; i++ {
		time.Sleep(100 * time.Millisecond)
		s = loadChatGPTRuntime(dataDir)
	}
	if s.ControlPort < 1 || s.ControlToken == "" {
		mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) {
			v.Running = false
			v.Starting = false
			v.Visible = false
			v.LoginActive = false
			v.SignedIn = false
			v.ControlPort = 0
			v.ControlToken = ""
		})
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := chatGPTControlRequest(ctx, s, http.MethodPost, "/stop", strings.NewReader(`{}`))
	if err != nil {
		mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) {
			v.Running = false
			v.Starting = false
			v.Visible = false
			v.LoginActive = false
			v.SignedIn = false
			v.ControlPort = 0
			v.ControlToken = ""
		})
	}
	return err
}

func runChatGPTWebView(dataDir string, visible bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(chatGPTProfilePath(dataDir), 0700); err != nil {
		return err
	}
	opts := webview2.WebViewOptions{
		Debug: false, AutoFocus: visible, DataPath: chatGPTProfilePath(dataDir),
		WindowOptions: webview2.WindowOptions{Title: "Connect ChatGPT to FlipAi", Width: 1120, Height: 820, Center: visible},
	}
	if !visible {
		opts.WindowOptions.Center = false
		opts.WindowOptions.Position = true
		opts.WindowOptions.X, opts.WindowOptions.Y = -30000, -30000
		opts.WindowOptions.ExStyle = wsExToolWin | wsExNoActivate
		opts.WindowOptions.NoActivate = true
	}
	w := webview2.NewWithOptions(opts)
	if w == nil {
		return errors.New("Microsoft Edge WebView2 Runtime could not create the ChatGPT browser")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(800, 600, webview2.HintMin)

	initial := loadChatGPTRuntime(dataDir)
	wasSignedIn := false
	hadConnected := initial.Connected
	_ = w.Bind("flipChatGPTStatus", func(signedIn bool, href string) {
		stateChanged := signedIn != wasSignedIn
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
			s.Running = true
			s.Starting = false
			s.Visible = visible
			s.LoginActive = visible
			s.SignedIn = signedIn
			s.LastURL = href
			if signedIn {
				s.Connected = true
				s.LastError = ""
				if stateChanged || s.LastEvent == "browser-starting" || s.LastEvent == "background-starting" {
					s.LastEvent = "session-ready"
				}
			} else if stateChanged || s.LastEvent == "browser-starting" || s.LastEvent == "background-starting" {
				if s.Connected {
					s.LastEvent = "session-restoring"
				} else {
					s.LastEvent = "waiting-for-sign-in"
				}
			}
		})
		if signedIn && !wasSignedIn {
			if hadConnected {
				chatGPTActivity(dataDir, "info", "chatgpt-session", "Saved ChatGPT sign-in was restored inside FlipAi's background browser.", 0)
			} else {
				chatGPTActivity(dataDir, "info", "chatgpt-session", "ChatGPT sign-in was verified and saved in FlipAi's dedicated browser profile.", 0)
				hadConnected = true
			}
		}
		wasSignedIn = signedIn
	})
	w.Init(chatGPTPageMonitorJS)
	dev := newWebViewDevTools(w)
	port, closer := startChatGPTControlEndpoint(dataDir, w, dev)
	if closer != nil {
		defer closer.Close()
	}
	mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
		s.Running = true
		s.Starting = false
		s.Visible = visible
		s.LoginActive = visible
		s.ControlPort = port
		s.SignedIn = false
		s.LastEvent = "browser-starting"
		s.LastError = ""
	})
	if visible {
		chatGPTActivity(dataDir, "info", "chatgpt-session", "Dedicated ChatGPT sign-in browser opened.", 0)
	} else {
		chatGPTActivity(dataDir, "info", "chatgpt-session", "Dedicated ChatGPT background browser started off-screen.", 0)
	}
	w.Navigate(chatGPTWebURL)
	w.Run()
	mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
		s.Running = false
		s.Starting = false
		s.Visible = false
		s.LoginActive = false
		s.SignedIn = false
		s.ControlPort = 0
		s.ControlToken = ""
		if s.Connected {
			s.LastEvent = "background-restart-pending"
		} else {
			s.LastEvent = "browser-closed"
		}
	})
	if visible && loadChatGPTRuntime(dataDir).Connected {
		chatGPTActivity(dataDir, "info", "chatgpt-session", "ChatGPT sign-in window closed; the saved session will continue invisibly in the background.", 0)
	}
	return nil
}

func startChatGPTControlEndpoint(dataDir string, w webview2.WebView, dev voiceDevTools) (int, io.Closer) {
	if dev == nil {
		return 0, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil
	}
	token, err := secureRandomToken(24)
	if err != nil {
		_ = ln.Close()
		return 0, nil
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) { s.ControlToken = token; s.ControlPort = port })
	authorized := func(r *http.Request) bool { return token != "" && r.Header.Get("X-FlipAi-Token") == token }
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "signedIn": chatGPTPageIsSignedIn(dev)})
	})

	ensureMode := func(mode string) chatGPTTurnResult {
		mode = strings.ToLower(strings.TrimSpace(mode))
		if mode == "" {
			mode = browserModeChat
		}
		if mode != browserModeChat && mode != browserModeWork {
			return chatGPTTurnResult{OK: false, Detail: "unsupported ChatGPT browser mode: " + mode}
		}
		evalMode := func() chatGPTTurnResult {
			expr := fmt.Sprintf(chatGPTSelectModeJS, chatGPTJSString(mode))
			var got chatGPTTurnResult
			if err := chatGPTEval(dev, expr, true, &got); err != nil {
				return chatGPTTurnResult{OK: false, Detail: "FlipAi could not switch the ChatGPT experience: " + err.Error()}
			}
			return got
		}
		got := evalMode()
		if got.OK || mode != browserModeChat {
			return got
		}

		// O: means regular ChatGPT Chat, even when the shared browser is currently
		// sitting in Work. If ChatGPT's mode picker cannot safely prove the switch,
		// the canonical root is a safe Chat boundary. This may start a fresh Chat
		// conversation when crossing out of Work, which is preferable to ever
		// sending an O: request into the Work conversation.
		// Navigating destroys the execution context this very call is running
		// in, so the call frequently never answers -- which is not a failure,
		// it is the navigation working. Reporting it as one is why an O:
		// message came back as "could not open regular ChatGPT Chat: the
		// WebView did not answer Runtime.evaluate" without ChatGPT ever seeing
		// the message. Whether the navigation worked is decided below, by
		// waiting for the page it lands on.
		_ = chatGPTEval(dev, chatGPTGoHomeJS, false, nil)
		time.Sleep(650 * time.Millisecond)
		if !waitForChatGPTPageSignedIn(dev, 45*time.Second) {
			return chatGPTTurnResult{OK: false, Detail: "ChatGPT did not restore the saved sign-in while switching from Work to Chat"}
		}
		return evalMode()
	}

	waitForComposer := func(timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for {
			ready := false
			if err := chatGPTEval(dev, chatGPTComposerReadyJS, false, &ready); err == nil && ready {
				return true
			}
			if !time.Now().Before(deadline) {
				return false
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	waitForFreshComposer := func() chatGPTTurnResult {
		if !waitForComposer(20 * time.Second) {
			return chatGPTTurnResult{OK: false, Detail: "ChatGPT opened a new chat but the fresh composer did not become ready"}
		}
		return chatGPTTurnResult{OK: true}
	}

	// recoverPage puts the WebView back on a page that can take a prompt. It is
	// the answer to a page whose execution context is gone or whose shell never
	// mounted a composer: without it the turn is simply lost, and ChatGPT never
	// sees the message. Its waits are deliberately short so a recovered turn
	// still finishes inside the worker's own request budget.
	recoverPage := func() bool {
		_ = chatGPTEval(dev, chatGPTGoHomeJS, false, nil)
		time.Sleep(650 * time.Millisecond)
		if !waitForChatGPTPageSignedIn(dev, 20*time.Second) {
			return false
		}
		return waitForComposer(12 * time.Second)
	}

	openFreshChat := func() chatGPTTurnResult {
		// O NEW: already works reliably in the user's real ChatGPT account. Make it
		// the single reset primitive: the root is always a fresh regular ChatGPT
		// Chat conversation, independent of whether the previous page was Chat or Work.
		// Navigating destroys this call's own execution context; the readiness
		// wait below decides whether the navigation worked.
		_ = chatGPTEval(dev, chatGPTGoHomeJS, false, nil)
		time.Sleep(650 * time.Millisecond)
		if !waitForChatGPTPageSignedIn(dev, 45*time.Second) {
			return chatGPTTurnResult{OK: false, Detail: "ChatGPT did not restore the saved sign-in after opening a fresh Chat session"}
		}
		if ready := waitForFreshComposer(); !ready.OK {
			return ready
		}
		if chat := ensureMode(browserModeChat); !chat.OK {
			return chat
		}
		return chatGPTTurnResult{OK: true}
	}

	prepareFresh := func(mode string) chatGPTTurnResult {
		mode = strings.ToLower(strings.TrimSpace(mode))
		if mode == "" {
			mode = browserModeChat
		}
		if mode == browserModeWork {
			// OW NEW: is deliberately built from the two operations that are proven
			// reliable in the live UI: O NEW: creates a fresh regular Chat, then OW:
			// switches that blank conversation into Work. This guarantees the task can
			// never reuse the old Work conversation.
			if fresh := openFreshChat(); !fresh.OK {
				return fresh
			}
			if work := ensureMode(browserModeWork); !work.OK {
				return chatGPTTurnResult{OK: false, Detail: "FlipAi opened a fresh ChatGPT conversation but could not switch that new conversation into Work. The task was not sent."}
			}
			return waitForFreshComposer()
		}
		return openFreshChat()
	}

	turn := func(rw http.ResponseWriter, r *http.Request, prompt string, newChat bool, mode string) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		startedAt := time.Now()
		if newChat {
			modeResult := prepareFresh(mode)
			if !modeResult.OK {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": modeResult.Detail})
				return
			}
		} else {
			if !waitForChatGPTPageSignedIn(dev, 20*time.Second) {
				s := loadChatGPTRuntime(dataDir)
				detail := "ChatGPT is not signed in inside FlipAi. Press Connect ChatGPT and complete sign-in first."
				if s.Connected {
					detail = "The saved ChatGPT session is still restoring or ChatGPT has expired it. Retry once; use Connect ChatGPT only if the saved account session no longer restores."
				}
				rw.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": detail})
				return
			}
			modeResult := ensureMode(mode)
			if !modeResult.OK {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": modeResult.Detail})
				return
			}
		}
		// Signed in is not the same as ready. When the experience switch leaves
		// the page on a shell with no composer, the driver cannot type anything
		// and the WebView rejects the call outright; the message was then lost
		// with ChatGPT never seeing it. Land on a page that can take a prompt
		// first.
		if !waitForComposer(5 * time.Second) {
			if !recoverPage() {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "ChatGPT is signed in but its page never showed a prompt box. The message was not sent."})
				return
			}
			if modeResult := ensureMode(mode); !modeResult.OK {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": modeResult.Detail})
				return
			}
		}
		expr := fmt.Sprintf(chatGPTTurnJS, chatGPTJSString(prompt))
		var got chatGPTTurnResult
		err := chatGPTEval(dev, expr, true, &got)
		// A page that could not run the script at all never typed the prompt, so
		// sending it again cannot deliver it twice. Recover onto the canonical
		// root -- a conversation the abandoned attempt could not have touched --
		// and send it once there, but only while enough of the worker's request
		// budget is left for the whole turn to still finish.
		if err != nil && chatGPTPageCouldNotRunScript(err) && time.Since(startedAt) < 60*time.Second {
			if recoverPage() {
				if modeResult := ensureMode(mode); modeResult.OK {
					got = chatGPTTurnResult{}
					err = chatGPTEval(dev, expr, true, &got)
				}
			}
		}
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "FlipAi could not run the ChatGPT page driver: " + err.Error()})
			return
		}
		cid := chatGPTConversationID(got.Href)
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
			s.Connected = true
			s.SignedIn = true
			s.LastURL = got.Href
			s.ConversationID = cid
			if got.OK {
				s.LastEvent = "turn-complete"
				s.LastError = ""
			} else {
				s.LastEvent = "turn-failed"
				s.LastError = got.Detail
			}
		})
		status := http.StatusOK
		if !got.OK {
			status = http.StatusBadGateway
		}
		rw.WriteHeader(status)
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": got.OK, "reply": got.Reply, "detail": got.Detail, "conversationId": cid})
	}

	mux.HandleFunc("/new", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(rw, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(rw, r.Body, 16<<10)).Decode(&body)
		modeResult := prepareFresh(body.Mode)
		if !modeResult.OK {
			rw.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": modeResult.Detail})
			return
		}
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
			s.Connected = true
			s.SignedIn = true
			s.ConversationID = ""
			s.LastEvent = "new-chat-ready"
			s.LastError = ""
		})
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(rw, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		if !chatGPTPageIsSignedIn(dev) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "ChatGPT is not signed in inside FlipAi."})
			return
		}
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
			s.Connected, s.SignedIn, s.LastEvent, s.LastError = true, true, "health-check-ok", ""
		})
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "detail": "signed-in browser session ready"})
	})
	mux.HandleFunc("/chat", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(rw, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Prompt string `json:"prompt"`
			New    bool   `json:"new"`
			Mode   string `json:"mode"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 64<<10)).Decode(&body); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		body.Prompt = strings.TrimSpace(body.Prompt)
		if body.Prompt == "" {
			http.Error(rw, "prompt required", http.StatusBadRequest)
			return
		}
		turn(rw, r, body.Prompt, body.New, body.Mode)
	})
	mux.HandleFunc("/stop", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]bool{"ok": true})
		go func() { time.Sleep(80 * time.Millisecond); w.Terminate() }()
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 4 * time.Second, WriteTimeout: 115 * time.Second}
	go func() { _ = server.Serve(ln) }()
	return port, server
}

func recordChatGPTWorkerError(dataDir string, err error) {
	if err == nil {
		return
	}
	mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
		s.Running = false
		s.Starting = false
		s.Visible = false
		s.LoginActive = false
		s.SignedIn = false
		s.LastEvent = "browser-error"
		s.LastError = err.Error()
	})
	chatGPTActivity(dataDir, "error", "chatgpt-session", "ChatGPT browser stopped with an error: "+err.Error(), 0)
}

func chatGPTWorkerMain(dataDir string, visible bool) {
	if err := runChatGPTWebView(dataDir, visible); err != nil {
		recordChatGPTWorkerError(dataDir, err)
	}
}
