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
    return !!document.querySelector('[data-testid="profile-button"],button[aria-label*="Profile"],nav a[href*="/settings"]');
  }
  async function tick(){
    try{ if(window.flipChatGPTStatus) await window.flipChatGPTStatus(await signed(), location.href); }catch(e){}
  }
  setInterval(tick,1500); addEventListener('load',tick); setTimeout(tick,500);
})();`

const chatGPTSignedInJS = `(async()=>{
  try{
    const r=await fetch('/api/auth/session',{credentials:'include',cache:'no-store'});
    if(r.ok){const j=await r.json();if(j&&j.user)return true;}
  }catch(e){}
  return !!document.querySelector('#prompt-textarea,[data-testid="prompt-textarea"],[contenteditable="true"]');
})()`

// chatGPTTurnJS deliberately uses ChatGPT's own page controls inside FlipAi's
// private WebView. It does not use Windows accessibility, global keyboard/mouse
// input, the user's visible ChatGPT app, or coordinates.
const chatGPTTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const assistants=()=>Array.from(document.querySelectorAll('[data-message-author-role="assistant"]'));
  const current=()=>{const a=assistants();return a.length?text(a[a.length-1]):''};
  const composer=()=>document.querySelector('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"][data-virtualkeyboard],[contenteditable="true"]');
  const send=()=>document.querySelector('button[data-testid="send-button"],button[aria-label="Send prompt"],button[aria-label^="Send" i]');
  const stop=()=>document.querySelector('button[data-testid="stop-button"],button[aria-label^="Stop" i]');
  let c=null;
  for(let i=0;i<100&&!c;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'ChatGPT is loaded but FlipAi could not find the message composer. The site layout may have changed.',href:location.href};
  const before=current();
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
    const now=current();
    if(now&&now!==before)started=true;
    if(started&&now){
      if(now===last)stable++;else{last=now;stable=0;}
      if(!stop()&&stable>=5)return {ok:true,reply:now,href:location.href};
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

func platformStartChatGPTLogin(dataDir string) error {
	_ = platformStopChatGPTWorker(dataDir)
	waitForChatGPTStopped(dataDir, 4*time.Second)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--chatgpt-login")
	return cmd.Start()
}

func platformEnsureChatGPTWorker(dataDir string) error {
	s := loadChatGPTRuntime(dataDir)
	if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer cancel()
		if _, code, err := chatGPTControlRequest(ctx, s, http.MethodGet, "/health", nil); err == nil && code == http.StatusOK {
			return nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--chatgpt-worker")
	hideWindow(cmd)
	return cmd.Start()
}

func platformStopChatGPTWorker(dataDir string) error {
	s := loadChatGPTRuntime(dataDir)
	if s.ControlPort < 1 || s.ControlToken == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := chatGPTControlRequest(ctx, s, http.MethodPost, "/stop", strings.NewReader(`{}`))
	if err != nil {
		mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) { v.Running = false; v.ControlPort = 0; v.ControlToken = "" })
	}
	return nil
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

	var wasSignedIn bool
	_ = w.Bind("flipChatGPTStatus", func(signedIn bool, href string) {
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
			s.Running = true
			s.Visible = visible
			s.SignedIn = signedIn
			s.LastURL = href
			s.LastEvent = "page-ready"
			s.LastError = ""
		})
		if signedIn && !wasSignedIn {
			wasSignedIn = true
			chatGPTActivity(dataDir, "info", "chatgpt-session", "ChatGPT sign-in was verified inside FlipAi's dedicated browser profile.", 0)
		}
	})
	w.Init(chatGPTPageMonitorJS)
	dev := newWebViewDevTools(w)
	port, closer := startChatGPTControlEndpoint(dataDir, w, dev)
	if closer != nil {
		defer closer.Close()
	}
	mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
		s.Running = true; s.Visible = visible; s.ControlPort = port; s.LastEvent = "browser-starting"; s.LastError = ""
	})
	if visible {
		chatGPTActivity(dataDir, "info", "chatgpt-session", "Dedicated ChatGPT sign-in browser opened.", 0)
	} else {
		chatGPTActivity(dataDir, "info", "chatgpt-session", "Dedicated ChatGPT background browser started.", 0)
	}
	w.Navigate(chatGPTWebURL)
	w.Run()
	mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
		s.Running = false; s.Visible = false; s.ControlPort = 0; s.ControlToken = ""; s.LastEvent = "browser-closed"
	})
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
		if !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }
		var signed bool
		_ = voiceEval(dev, chatGPTSignedInJS, true, &signed)
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "signedIn": signed})
	})
	turn := func(rw http.ResponseWriter, r *http.Request, prompt string, newChat bool) {
		if !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }
		if newChat {
			var ignored bool
			_ = voiceEval(dev, `(()=>{location.href='https://chatgpt.com/';return true})()`, false, &ignored)
			time.Sleep(900*time.Millisecond)
		}
		var signed bool
		if err := voiceEval(dev, chatGPTSignedInJS, true, &signed); err != nil || !signed {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "ChatGPT is not signed in inside FlipAi. Press Connect ChatGPT and complete sign-in first."})
			return
		}
		expr := fmt.Sprintf(chatGPTTurnJS, chatGPTJSString(prompt))
		var got chatGPTTurnResult
		if err := voiceEval(dev, expr, true, &got); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "FlipAi could not run the ChatGPT page driver: " + err.Error()})
			return
		}
		cid := chatGPTConversationID(got.Href)
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
			s.SignedIn = true; s.LastURL = got.Href; s.ConversationID = cid
			if got.OK { s.LastEvent = "turn-complete"; s.LastError = "" } else { s.LastEvent = "turn-failed"; s.LastError = got.Detail }
		})
		status := http.StatusOK
		if !got.OK { status = http.StatusBadGateway }
		rw.WriteHeader(status)
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": got.OK, "reply": got.Reply, "detail": got.Detail, "conversationId": cid})
	}
	mux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { http.Error(rw, "POST required", 405); return }
		turn(rw, r, "Reply with exactly: FLIPAI_OK", true)
	})
	mux.HandleFunc("/chat", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { http.Error(rw, "POST required", 405); return }
		var body struct { Prompt string `json:"prompt"`; New bool `json:"new"` }
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 64<<10)).Decode(&body); err != nil { http.Error(rw, err.Error(), 400); return }
		body.Prompt = strings.TrimSpace(body.Prompt)
		if body.Prompt == "" { http.Error(rw, "prompt required", 400); return }
		turn(rw, r, body.Prompt, body.New)
	})
	mux.HandleFunc("/stop", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }
		_ = json.NewEncoder(rw).Encode(map[string]bool{"ok": true})
		go func(){ time.Sleep(80*time.Millisecond); w.Terminate() }()
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 4 * time.Second, WriteTimeout: 105 * time.Second}
	go func(){ _ = server.Serve(ln) }()
	return port, server
}

func recordChatGPTWorkerError(dataDir string, err error) {
	if err == nil { return }
	mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) { s.Running = false; s.LastEvent = "browser-error"; s.LastError = err.Error() })
	chatGPTActivity(dataDir, "error", "chatgpt-session", "ChatGPT browser stopped with an error: "+err.Error(), 0)
}

func chatGPTWorkerMain(dataDir string, visible bool) {
	if err := runChatGPTWebView(dataDir, visible); err != nil {
		recordChatGPTWorkerError(dataDir, err)
	}
}
