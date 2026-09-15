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

// Copilot has used both ordinary React controls and open-shadow-root web
// components over time. The page driver walks both while still interacting
// only with the website's real composer and Send control.
const copilotChatPageMonitorJS = `(function(){
  if(window.__flipAiCopilotChatMonitor)return;
  window.__flipAiCopilotChatMonitor=true;
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
  const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};
  const composer=()=>first('textarea#userInput,textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],div[contenteditable="true"]');
  const loginPage=()=>/(?:login\.live\.com|login\.microsoftonline\.com)$/i.test(location.hostname)||/(?:\/login|\/signin|\/sign-in)(?:\/|$)/i.test(location.pathname)||!!first('form input[type="email"],form input[name="loginfmt"],form input[autocomplete="username"]');
  const signed=()=>/(^|\.)copilot\.microsoft\.com$/i.test(location.hostname)&&!!composer()&&!loginPage();
  async function tick(){try{if(window.flipCopilotChatStatus)await window.flipCopilotChatStatus(signed(),location.href)}catch(e){}}
  setInterval(tick,1000);addEventListener('load',tick);setTimeout(tick,350);
})();`

const copilotChatSignedInJS = `(()=>{const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}return out};const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};const c=first('textarea#userInput,textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],div[contenteditable="true"]');const loginPage=/(?:login\.live\.com|login\.microsoftonline\.com)$/i.test(location.hostname)||/(?:\/login|\/signin|\/sign-in)(?:\/|$)/i.test(location.pathname)||!!first('form input[type="email"],form input[name="loginfmt"],form input[autocomplete="username"]');return /(^|\.)copilot\.microsoft\.com$/i.test(location.hostname)&&!!c&&!loginPage})()`

const copilotChatTurnJS = `(async(input)=>{
  // One budget for the whole script, fixed when it starts.
  // Waiting for the composer and then starting a fresh ninety seconds is two
  // budgets end to end: on a slow page that ran past the deadline the DevTools
  // layer allows a turn, and the call was abandoned at ninety-five seconds with
  // the model's answer sitting finished in the page.
  const turnDeadline=Date.now()+82000;
  // What the driver actually did, step by step, so a turn that goes wrong says
  // where rather than only that it did. Steps are metadata -- names, timings,
  // counts, lengths -- never the prompt or the reply.
  const T=[],t0=Date.now();
  const mark=s=>{T.push(String(s)+' @'+((Date.now()-t0)/1000).toFixed(1)+'s');return true};
  const trace=()=>T.join(' | ');
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  // The reply's own action row -- "Edit in a page", "Copy", "Good response" --
  // is page furniture, not part of what the model said, and it was going out
  // on the end of the message. Strip controls before reading the text.
  // Scanning reads text the cheap way. Cloning every candidate on every poll
  // to strip its controls cost more than the poll interval on a real
  // conversation, and the turn then could not finish inside its own deadline.
  const rawText=n=>String(n&&(n.innerText||n.textContent)||'').trim();
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
  const chromeControlSel='button,[role="button"],[role="toolbar"],[role="menu"],[class*="action" i],[class*="toolbar" i],[class*="footer" i]';
  const chromeSel=chromeControlSel+','+refBlockSel;
  // The chosen reply gets the careful read: its action row is page furniture.
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
    const clone=n.cloneNode&&n.cloneNode(true);
    if(clone&&clone.querySelectorAll){
      dropCards(clone);
      clone.querySelectorAll(chromeSel+','+activityStripSel+',script,style,template,noscript,svg').forEach(el=>el.remove());
      clone.querySelectorAll(refInlineSel).forEach(el=>{if(!/[a-z]/i.test(el.textContent||''))el.remove()});
      // Never let stripping empty a real message: if everything went, a
      // selector matched the answer, and the raw text beats nothing at all.
      return String(clone.innerText||clone.textContent||'').trim()||rawText(n);
    }
    return rawText(n);
  };
  // A short status line is not an answer. "Searching sources" reached the phone
  // in place of what the model said, because it sat unchanged long enough to
  // look settled.
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
  const interim=t=>{
    const v=String(t||'').replace(/\s+/g,' ').trim().toLowerCase();
    if(!v)return true;
    if(v.length>80)return false;
    return /^(search|think|reason|analy[sz]|work|generat|load|read|brows|look|check|process|plan|writ|draft|creat|gather|review)(ing|ed)?\b/.test(v)
      ||/^(using|calling|running|opening) \S+/.test(v)
      ||/^(one moment|just a moment|please wait)\b/.test(v);
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
  const all=q=>{const out=[];for(const r of roots())out.push(...r.querySelectorAll(q));return Array.from(new Set(out))};
  // The conversation, not the whole page. A saved task in the sidebar and a
  // suggestion chip under the composer are both on the page and neither is
  // anything the model just said. Scan <main> when the page has one.
  const scanRoots=()=>{
    const rs=roots();
    const mains=[];
    for(const r of rs){for(const m of r.querySelectorAll('main'))mains.push(m)}
    return mains.length?mains:rs;
  };
  const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};
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
  const assistants=()=>{
    // rawText, not text: the careful clone-and-strip read is for the one reply
    // FlipAi sends, and running it over every candidate on every poll is what
    // made a turn miss its own deadline.
    const primary=dropContainers(all('[data-content="ai-message"],[data-testid*="assistant" i],[data-testid*="bot" i],[data-message-author-role="assistant"],[data-author="bot"],[data-author="assistant"],[class*="response-message" i],[class*="assistant-message" i]').filter(n=>rawText(n)&&!n.closest('form')));
    if(primary.length)return primary;
    const articles=dropContainers(all('main [role="article"],main [class*="markdown" i],main .markdown,main .prose').filter(n=>rawText(n)&&!n.closest('form')));
    if(articles.length)return articles;
    return genericBlocks();
  };
  const activitySel='aside,nav,header,footer,[role="log"],[role="status"],[role="navigation"],[role="complementary"],[role="banner"],[role="contentinfo"],[role="dialog"],[aria-live],[class*="activity" i],[class*="timeline" i],[class*="step" i],[class*="tool" i],[class*="trace" i],[class*="sidebar" i],[class*="suggestion" i],[data-testid*="suggestion" i],[id*="step" i],[id*="activity" i],[id*="sidebar" i]';
  // Only leaf-ish blocks, and cheaply. Comparing every block against every
  // other to drop containers was quadratic, and on a real conversation it cost
  // more than the poll interval; an ancestor walk over a small set is linear.
  // A container is never a candidate on its own, which is what stopped the
  // whole thread -- every message joined together -- from being sent as one
  // answer.
  // Collecting is in three passes, and the later ones are the safety net. Every
  // exclusion here is a guess about what the page keeps outside its conversation,
  // and a wrong guess left the driver seeing nothing at all -- a turn that ran
  // its whole budget and reported that the model never answered, while the answer
  // was on screen the entire time. If a pass finds nothing, the next gives back
  // what it excluded rather than going blind.
  const collectBlocks=(rs,skipFurniture)=>{
    const found=[];
    for(const r of rs){
      for(const n of r.querySelectorAll('div,p,section,article,li,pre')){
        if(n.closest&&(n.closest('form')||n.closest('[contenteditable="true"]')))continue;
        if(n.querySelector&&n.querySelector('textarea,input,[contenteditable="true"]'))continue;
        if(n.matches&&n.matches(chromeSel))continue;
        if(n.closest&&n.closest(chromeSel))continue;
        if(skipFurniture&&n.closest&&n.closest(activitySel))continue;
        const t=rawText(n);
        if(t.length<2||t.length>20000)continue;
        found.push(n);
      }
    }
    return found;
  };
  const genericBlocks=()=>{
    let found=collectBlocks(scanRoots(),true);
    if(!found.length)found=collectBlocks(roots(),true);
    if(!found.length)found=collectBlocks(roots(),false);
    const set=new Set(found),container=new Set();
    for(const n of found){
      let p=n.parentElement;
      for(let hops=0;p&&hops<40;hops++,p=p.parentElement){if(set.has(p))container.add(p)}
    }
    // The newest message is the last one, so keep the tail. Capping the head
    // stopped the scan before it ever reached this turn's reply.
    return found.filter(n=>!container.has(n)).slice(-400);
  };
  // A message is usually several blocks. Lift a matched block to the element
  // the conversation holds directly, so the whole answer travels rather than
  // only its last paragraph.
  const wholeMessage=(n,box)=>{
    // n===box, or a walk that escapes the box, is how the reply became the whole
    // document: the climb ran past the conversation to <html> and every word on
    // the page was sent as the answer.
    if(!n||!box||n===box||!box.contains(n))return n;
    let cur=n;
    while(cur.parentElement&&cur.parentElement!==box&&box.contains(cur.parentElement))cur=cur.parentElement;
    return cur.parentElement===box?cur:n;
  };
  // The conversation is wherever the prompt just landed. Anchoring to it keeps
  // the answer and the side panels apart without having to know either by name.
  const conversationBox=()=>{
    const mine=genericBlocks().filter(n=>canon(rawText(n))===promptText);
    if(!mine.length)return null;
    let box=mine[mine.length-1].parentElement;
    while(box&&box.children.length<2&&box.parentElement)box=box.parentElement;
    return box;
  };
  const composer=()=>first('textarea#userInput,textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],div[contenteditable="true"]');
  const send=()=>{const xs=all('button[data-testid*="send" i],button[aria-label*="send" i],button[title*="send" i],button[type="submit"]');return xs.find(b=>!b.disabled&&b.offsetParent!==null)||xs.find(b=>!b.disabled)||null};
  const stop=()=>{const xs=all('button[data-testid*="stop" i],button[data-testid*="cancel" i],button[aria-label*="stop" i],button[aria-label*="cancel" i],button[title*="stop" i]');return xs.find(b=>!b.disabled&&b.offsetParent!==null)||null};
  let c=null;
  for(let i=0;i<120&&!c&&Date.now()<turnDeadline-62000;i++){c=composer();if(!c)await sleep(200)}
  mark(c?'composer-found':'composer-missing');
  if(!c)return {ok:false,trace:trace(),detail:'Microsoft Copilot is loaded but FlipAi could not find the prompt box. The Copilot site layout may have changed.',href:location.href};
  const canon=t=>String(t||'').replace(/\s+/g,' ').trim();
  const promptText=canon(input);
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
  const responseForTurn=()=>{
    const named=assistants();
    const box=conversationBox();
    const pick=(current,restrict,allowInterim)=>{
      for(let i=current.length-1;i>=0;i--){
        const n=current[i],t=canon(rawText(n));
        if(!t||t===promptText||beforeTexts.has(t))continue;
        if(restrict&&box&&!box.contains(n))continue;
        if(!allowInterim&&(interim(t)||statusLine(t)))continue;
        const whole=wholeMessage(n,box);
        const chosen=canon(rawText(whole))===promptText?n:whole;
        if(!canon(text(chosen)))continue;
        return chosen;
      }
      return null;
    };
    // A selector that matches something is not the same as a selector that
    // matches the answer. When the named elements yield nothing this turn, read
    // the conversation structurally rather than concluding the model never
    // answered -- that conclusion cost a whole turn while the reply was on screen.
    return pick(named,true,false)||pick(named,false,false)||pick(genericBlocks(),false,false);
  };
  c.focus();
  try{
    if(c instanceof HTMLTextAreaElement||c instanceof HTMLInputElement){
      const proto=c instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
      const setter=Object.getOwnPropertyDescriptor(proto,'value').set;setter.call(c,input);
      c.dispatchEvent(new Event('input',{bubbles:true,composed:true}));c.dispatchEvent(new Event('change',{bubbles:true,composed:true}));
    }else{
      const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);
      document.execCommand('delete',false,null);document.execCommand('insertText',false,input);
      c.dispatchEvent(new InputEvent('input',{bubbles:true,composed:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true,composed:true}));
    }
  }catch(e){c.textContent=input;c.dispatchEvent(new Event('input',{bubbles:true,composed:true}))}
  await sleep(300);
  let b=null;
  for(let i=0;i<80&&!b;i++){b=send();if(!b)await sleep(100)}
  mark('typed');
  if(b){mark('send-click');b.click()}
  else{
    mark('send-enter');
    // Copilot's send control is not always a <button> FlipAi can name, and the
    // turn used to be abandoned here with the prompt typed and never sent.
    // Enter is how a person sends it.
    const form=c.closest&&c.closest('form');
    if(form&&typeof form.requestSubmit==='function'){try{form.requestSubmit()}catch(e){}}
    for(const type of ['keydown','keypress','keyup']){
      c.dispatchEvent(new KeyboardEvent(type,{bubbles:true,composed:true,cancelable:true,key:'Enter',code:'Enter',keyCode:13,which:13}));
    }
  }
  // A one-second pause is not proof that a short line is the answer. A status
  // the page shows while it works sits unchanged exactly that long, and it was
  // texted in place of the reply. A real answer of any length still goes out;
  // a short one just has to hold still for three seconds instead of one.
  const settleNeeded=v=>String(v||'').length>=40?5:12;
  let last='',stable=0,started=false;const deadline=turnDeadline;/*__FLIPAI_BROWSER_TURN__*/
  while(Date.now()<deadline){
    await sleep(250);const node=responseForTurn();
    if(node){if(!started)mark('reply-node-seen');started=true;const now=text(node);if(now===last)stable++;else{last=now;stable=0}if(!stop()&&stable>=settleNeeded(now)&&now&&!statusLine(now)){mark('settled len='+now.length);return {ok:true,trace:trace(),reply:newestPart(node,now),href:location.href}};if(now&&stable>=32)return {ok:true,trace:trace(),reply:newestPart(node,now),href:location.href}}
  }
  mark('deadline started='+started+' candidates='+assistants().length+' blocks='+genericBlocks().length+' stop='+(stop()?'yes':'no')+' lastLen='+last.length);
  return {ok:false,trace:trace(),detail:started?'Microsoft Copilot started answering but did not finish in time.':'Microsoft Copilot did not produce a new response in time.',href:location.href};
})(%s)`

type copilotChatTurnResult struct {
	OK     bool   `json:"ok"`
	Reply  string `json:"reply"`
	Detail string `json:"detail"`
	Href   string `json:"href"`
	// Trace is the page driver's own step log: what it found, what it did,
	// and what the page looked like when it gave up. Metadata only.
	Trace  string `json:"trace"`
}

func copilotChatEval(d voiceDevTools, expression string, awaitPromise bool, out any) error {
	if d == nil {
		return errors.New("the Microsoft Copilot Chat WebView has no in-process control channel")
	}
	var got voiceDevToolsEval
	params := map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": awaitPromise}
	if err := d.Call("Runtime.evaluate", params, &got); err != nil {
		return fmt.Errorf("the Microsoft Copilot Chat WebView did not answer Runtime.evaluate: %w", err)
	}
	if len(got.ExceptionDetails) > 0 && string(got.ExceptionDetails) != "null" {
		return browserPageScriptError("Microsoft Copilot", got.ExceptionDetails)
	}
	if out == nil {
		return nil
	}
	if len(got.Result.Value) == 0 {
		return errors.New("the Microsoft Copilot page returned no value")
	}
	if err := json.Unmarshal(got.Result.Value, out); err != nil {
		return fmt.Errorf("the Microsoft Copilot page returned an unreadable value: %w", err)
	}
	return nil
}

func copilotChatPageIsSignedIn(d voiceDevTools) bool {
	var v bool
	return copilotChatEval(d, copilotChatSignedInJS, true, &v) == nil && v
}

func waitForCopilotChatPageSignedIn(d voiceDevTools, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if copilotChatPageIsSignedIn(d) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func platformStartCopilotChatLogin(dataDir string) error {
	_ = platformStopCopilotChatWorker(dataDir)
	waitForCopilotChatStopped(dataDir, 4*time.Second)
	mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {
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
		return err
	}
	cmd := exec.Command(exe, "--copilot-chat-login")
	if err := cmd.Start(); err != nil {
		mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) { s.LoginActive = false; s.Starting = false; s.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformEnsureCopilotChatWorker(dataDir string) error {
	s := loadCopilotChatRuntime(dataDir)
	if s.LoginActive && (s.Running || s.Starting) {
		return nil
	}
	if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer cancel()
		if _, code, err := copilotChatControlRequest(ctx, s, http.MethodGet, "/health", nil); err == nil && code == http.StatusOK {
			return nil
		}
	}
	if s.Starting && time.Since(s.UpdatedAt) < 10*time.Second {
		return nil
	}
	mutateCopilotChatRuntime(dataDir, func(v *CopilotChatWebRuntime) {
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
		return err
	}
	cmd := exec.Command(exe, "--copilot-chat-worker")
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		mutateCopilotChatRuntime(dataDir, func(v *CopilotChatWebRuntime) { v.Starting = false; v.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformStopCopilotChatWorker(dataDir string) error {
	s := loadCopilotChatRuntime(dataDir)
	for i := 0; i < 20 && s.ControlPort < 1 && s.Starting; i++ {
		time.Sleep(100 * time.Millisecond)
		s = loadCopilotChatRuntime(dataDir)
	}
	if s.ControlPort < 1 || s.ControlToken == "" {
		mutateCopilotChatRuntime(dataDir, func(v *CopilotChatWebRuntime) {
			v.Running, v.Starting, v.Visible, v.LoginActive, v.SignedIn = false, false, false, false, false
			v.ControlPort, v.ControlToken = 0, ""
		})
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := copilotChatControlRequest(ctx, s, http.MethodPost, "/stop", strings.NewReader(`{}`))
	if err != nil {
		mutateCopilotChatRuntime(dataDir, func(v *CopilotChatWebRuntime) {
			v.Running, v.Starting, v.Visible, v.LoginActive, v.SignedIn = false, false, false, false, false
			v.ControlPort, v.ControlToken = 0, ""
		})
	}
	return err
}

func runCopilotChatWebView(dataDir string, visible bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(copilotChatProfilePath(dataDir), 0700); err != nil {
		return err
	}
	opts := webview2.WebViewOptions{Debug: false, AutoFocus: visible, DataPath: copilotChatProfilePath(dataDir), WindowOptions: webview2.WindowOptions{Title: "Connect Microsoft Copilot Chat to FlipAi", Width: 1120, Height: 820, Center: visible}}
	if !visible {
		opts.WindowOptions.Center = false
		opts.WindowOptions.Position = true
		opts.WindowOptions.X = -30000
		opts.WindowOptions.Y = -30000
		opts.WindowOptions.ExStyle = wsExToolWin | wsExNoActivate
		opts.WindowOptions.NoActivate = true
	}
	w := webview2.NewWithOptions(opts)
	if w == nil {
		return errors.New("Microsoft Edge WebView2 Runtime could not create the Microsoft Copilot Chat browser")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(800, 600, webview2.HintMin)
	initial := loadCopilotChatRuntime(dataDir)
	wasSignedIn := false
	hadConnected := initial.Connected
	_ = w.Bind("flipCopilotChatStatus", func(signedIn bool, href string) {
		changed := signedIn != wasSignedIn
		mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {
			s.Running = true
			s.Starting = false
			s.Visible = visible
			s.LoginActive = visible
			s.SignedIn = signedIn
			s.LastURL = href
			if signedIn {
				s.Connected = true
				s.LastError = ""
				if changed || strings.Contains(s.LastEvent, "starting") {
					s.LastEvent = "session-ready"
				}
			} else if changed || strings.Contains(s.LastEvent, "starting") {
				if s.Connected {
					s.LastEvent = "session-restoring"
				} else {
					s.LastEvent = "waiting-for-sign-in"
				}
			}
		})
		if signedIn && !wasSignedIn {
			if hadConnected {
				copilotChatActivity(dataDir, "info", "copilot-chat-session", "Saved Microsoft Copilot Chat sign-in was restored.", 0)
			} else {
				copilotChatActivity(dataDir, "info", "copilot-chat-session", "Microsoft Copilot Chat sign-in was verified and saved in FlipAi's dedicated profile.", 0)
				hadConnected = true
			}
		}
		wasSignedIn = signedIn
	})
	w.Init(copilotChatPageMonitorJS)
	dev := newWebViewDevTools(w)
	port, closer := startCopilotChatControlEndpoint(dataDir, w, dev)
	if closer != nil {
		defer closer.Close()
	}
	mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {
		s.Running = true
		s.Starting = false
		s.Visible = visible
		s.LoginActive = visible
		s.ControlPort = port
		s.SignedIn = false
		s.LastEvent = "browser-starting"
		s.LastError = ""
	})
	w.Navigate(copilotChatWebURL)
	w.Run()
	mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {
		s.Running, s.Starting, s.Visible, s.LoginActive, s.SignedIn = false, false, false, false, false
		s.ControlPort, s.ControlToken = 0, ""
		if s.Connected {
			s.LastEvent = "background-restart-pending"
		} else {
			s.LastEvent = "browser-closed"
		}
	})
	return nil
}

func startCopilotChatControlEndpoint(dataDir string, w webview2.WebView, dev voiceDevTools) (int, io.Closer) {
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
	mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) { s.ControlToken = token; s.ControlPort = port })
	authorized := func(r *http.Request) bool { return token != "" && r.Header.Get("X-FlipAi-Token") == token }
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "signedIn": copilotChatPageIsSignedIn(dev)})
	})
	turn := func(rw http.ResponseWriter, r *http.Request, prompt string, newChat bool) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		if newChat {
			// Navigating destroys this call's own execution context; the
			// readiness wait below decides whether the navigation worked.
			_ = copilotChatEval(dev, browserPageNavigateJS("https://copilot.microsoft.com/"), false, nil)
		}
		if !waitForCopilotChatPageSignedIn(dev, 25*time.Second) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Microsoft Copilot Chat is not ready inside FlipAi. Press Connect and complete sign-in first."})
			return
		}
		cleanPrompt, attachments, marked, markerErr := extractBrowserChatAttachmentMarker(prompt)
		if marked {
			if markerErr != nil {
				rw.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": markerErr.Error()})
				return
			}
			if err := uploadBrowserChatImages(dev, attachments); err != nil {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
				return
			}
			prompt = strings.TrimSpace(cleanPrompt)
			if prompt == "" {
				prompt = browserChatImageOnlyPrompt(len(attachments))
			}
		}
		expr := fmt.Sprintf(copilotChatTurnJS, copilotChatJSString(prompt))
		var got copilotChatTurnResult
		if err := copilotChatEval(dev, expr, true, &got); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "FlipAi could not run the Microsoft Copilot page driver: " + err.Error()})
			return
		}
		cid := copilotChatConversationID(got.Href)
		mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {
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
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": got.OK, "reply": got.Reply, "detail": got.Detail, "conversationId": cid, "trace": got.Trace})
	}
	mux.HandleFunc("/new", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(rw, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		_ = copilotChatEval(dev, browserPageNavigateJS("https://copilot.microsoft.com/"), false, nil)
		if !waitForCopilotChatPageSignedIn(dev, 45*time.Second) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Microsoft Copilot did not restore the saved session after opening a new chat"})
			return
		}
		mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {
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
		if !copilotChatPageIsSignedIn(dev) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Microsoft Copilot Chat is not signed in inside FlipAi."})
			return
		}
		mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {
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
		turn(rw, r, body.Prompt, body.New)
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

func recordCopilotChatWorkerError(dataDir string, err error) {
	if err == nil {
		return
	}
	mutateCopilotChatRuntime(dataDir, func(s *CopilotChatWebRuntime) {
		s.Running, s.Starting, s.Visible, s.LoginActive, s.SignedIn = false, false, false, false, false
		s.LastEvent = "browser-error"
		s.LastError = err.Error()
	})
	copilotChatActivity(dataDir, "error", "copilot-chat-session", "Microsoft Copilot Chat browser stopped with an error: "+err.Error(), 0)
}

func copilotChatWorkerMain(dataDir string, visible bool) {
	if err := runCopilotChatWebView(dataDir, visible); err != nil {
		recordCopilotChatWorkerError(dataDir, err)
	}
}
