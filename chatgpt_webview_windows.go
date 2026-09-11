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
const chatGPTSelectModeJS = `(async(wanted)=>{
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
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const clean=s=>String(s||'')
    .replace(/Unable to display this message due to an error\.?\s*Reload the page to try again\.?/gi,' ')
    .replace(/Unable to display this message due to an error\.?/gi,' ')
    .replace(/\s+/g,' ').trim();
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
      clone.querySelectorAll('button,canvas,svg,iframe,[role="button"],[role="tab"],[role="tabpanel"],[role="slider"],[role="progressbar"],[data-testid*="widget" i],[data-testid*="weather" i],[data-testid*="chart" i],[data-testid*="carousel" i],[data-testid*="feedback" i]').forEach(el=>el.remove());
      return clean(clone.innerText||clone.textContent||'');
    }
    return clean(n.innerText||n.textContent||'');
  };
  const users=()=>Array.from(document.querySelectorAll('[data-message-author-role="user"]'));
  const assistants=()=>Array.from(document.querySelectorAll('[data-message-author-role="assistant"]'));
  const composer=()=>document.querySelector('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"][data-virtualkeyboard],[contenteditable="true"]');
  const send=()=>document.querySelector('button[data-testid="send-button"],button[aria-label="Send prompt"],button[aria-label^="Send" i]');
  const stop=()=>document.querySelector('button[data-testid="stop-button"],button[aria-label^="Stop" i]');
  let c=null;
  for(let i=0;i<100&&!c;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'ChatGPT is loaded but FlipAi could not find the message composer. The site layout may have changed.',href:location.href};
  const beforeUserCount=users().length;
  const beforeAssistantCount=assistants().length;
  const assistantForThisTurn=()=>{
    const us=users();
    const as=assistants();
    const newUser=us.length>beforeUserCount?us[us.length-1]:null;
    if(newUser){
      for(let i=as.length-1;i>=0;i--){
        if(newUser.compareDocumentPosition(as[i])&Node.DOCUMENT_POSITION_FOLLOWING)return as[i];
      }
    }
    return as.length>beforeAssistantCount?as[as.length-1]:null;
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
  if(!b)return {ok:false,detail:'FlipAi filled the ChatGPT composer but the Send button never became ready.',href:location.href};
  b.click();
  let last='',stable=0,started=false;
  const deadline=Date.now()+90000;
  while(Date.now()<deadline){
    await sleep(250);
    const node=assistantForThisTurn();
    if(node){
      started=true;
      const now=text(node);
      if(now===last)stable++;else{last=now;stable=0;}
      if(!stop()&&stable>=5&&now)return {ok:true,reply:now,href:location.href};
    }
  }
  return {ok:false,detail:started?'ChatGPT started answering but did not finish within 90 seconds.':'ChatGPT did not produce an assistant response within 90 seconds.',href:location.href};
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
		return errors.New("the ChatGPT page script failed")
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
		var ignored bool
		if err := chatGPTEval(dev, `(()=>{location.href='https://chatgpt.com/';return true})()`, false, &ignored); err != nil {
			return chatGPTTurnResult{OK: false, Detail: "FlipAi could not open regular ChatGPT Chat: " + err.Error()}
		}
		time.Sleep(650 * time.Millisecond)
		if !waitForChatGPTPageSignedIn(dev, 45*time.Second) {
			return chatGPTTurnResult{OK: false, Detail: "ChatGPT did not restore the saved sign-in while switching from Work to Chat"}
		}
		return evalMode()
	}

	waitForFreshComposer := func() chatGPTTurnResult {
		composerReady := false
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if err := chatGPTEval(dev, `(()=>!!document.querySelector('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"]'))()`, false, &composerReady); err == nil && composerReady {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !composerReady {
			return chatGPTTurnResult{OK: false, Detail: "ChatGPT opened a new chat but the fresh composer did not become ready"}
		}
		return chatGPTTurnResult{OK: true}
	}

	openFreshChat := func() chatGPTTurnResult {
		// O NEW: already works reliably in the user's real ChatGPT account. Make it
		// the single reset primitive: the root is always a fresh regular ChatGPT
		// Chat conversation, independent of whether the previous page was Chat or Work.
		var ignored bool
		if err := chatGPTEval(dev, `(()=>{location.href='https://chatgpt.com/';return true})()`, false, &ignored); err != nil {
			return chatGPTTurnResult{OK: false, Detail: "FlipAi could not open a fresh ChatGPT Chat session: " + err.Error()}
		}
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
		expr := fmt.Sprintf(chatGPTTurnJS, chatGPTJSString(prompt))
		var got chatGPTTurnResult
		if err := chatGPTEval(dev, expr, true, &got); err != nil {
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
