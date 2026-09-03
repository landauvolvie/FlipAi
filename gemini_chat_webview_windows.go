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

const geminiChatPageMonitorJS = `(function(){
  if(window.__flipAiGeminiChatMonitor)return;
  window.__flipAiGeminiChatMonitor=true;
  const composer=()=>document.querySelector('rich-textarea .ql-editor[contenteditable="true"],rich-textarea [contenteditable="true"],div.ql-editor[contenteditable="true"],[contenteditable="true"][role="textbox"],[contenteditable="true"][aria-label*="prompt" i],textarea[aria-label*="prompt" i],textarea');
  const signIn=()=>Array.from(document.querySelectorAll('a,button')).find(n=>/sign in/i.test(((n.getAttribute('aria-label')||'')+' '+(n.innerText||'')).trim()));
  const signed=()=>location.hostname==='gemini.google.com' && !!composer() && !signIn();
  async function tick(){
    try{ if(window.flipGeminiChatStatus) await window.flipGeminiChatStatus(signed(), location.href); }catch(e){}
  }
  setInterval(tick,1000); addEventListener('load',tick); setTimeout(tick,350);
})();`

const geminiChatSignedInJS = `(()=>{const c=document.querySelector('rich-textarea .ql-editor[contenteditable="true"],rich-textarea [contenteditable="true"],div.ql-editor[contenteditable="true"],[contenteditable="true"][role="textbox"],[contenteditable="true"][aria-label*="prompt" i],textarea[aria-label*="prompt" i],textarea');const signIn=Array.from(document.querySelectorAll('a,button')).find(n=>/sign in/i.test(((n.getAttribute('aria-label')||'')+' '+(n.innerText||'')).trim()));return location.hostname==='gemini.google.com'&&!!c&&!signIn})()`

const geminiChatTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const all=q=>Array.from(document.querySelectorAll(q));
  const unique=xs=>Array.from(new Set(xs));
  const assistants=()=>{
    const primary=unique([...all('model-response'),...all('[data-test-id="model-response"]'),...all('[data-testid="model-response"]')]).filter(n=>text(n));
    if(primary.length)return primary;
    return unique([...all('.model-response-text'),...all('.markdown-main-panel'),...all('[class*="model-response"]')]).filter(n=>text(n));
  };
  const composer=()=>document.querySelector('rich-textarea .ql-editor[contenteditable="true"],rich-textarea [contenteditable="true"],div.ql-editor[contenteditable="true"],[contenteditable="true"][role="textbox"],[contenteditable="true"][aria-label*="prompt" i],textarea[aria-label*="prompt" i],textarea');
  const send=()=>{
    const candidates=unique([...all('button[aria-label*="Send" i]'),...all('button[mattooltip*="Send" i]'),...all('button[data-test-id*="send" i]'),...all('button[data-testid*="send" i]'),...all('button.send-button')]);
    return candidates.find(b=>!b.disabled && b.offsetParent!==null)||candidates.find(b=>!b.disabled)||null;
  };
  const stop=()=>{const xs=unique([...all('button[aria-label*="Stop" i]'),...all('button[mattooltip*="Stop" i]'),...all('button[data-test-id*="stop" i]'),...all('button[data-testid*="stop" i]')]);return xs.find(b=>!b.disabled&&b.offsetParent!==null)||null};
  const responseFinishedChrome=node=>{
    if(!node)return false;
    const root=node.closest('model-response,[data-test-id="model-response"],[data-testid="model-response"]')||node;
    const scope=root.parentElement||root;
    const buttons=unique([...Array.from(root.querySelectorAll('button')),...Array.from(scope.querySelectorAll('button'))]);
    return buttons.some(b=>/good response|bad response|regenerate|copy response|more options|share/i.test(((b.getAttribute('aria-label')||'')+' '+(b.getAttribute('mattooltip')||'')+' '+(b.innerText||'')).trim()));
  };
  let c=null;
  for(let i=0;i<120&&!c;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'Gemini is loaded but FlipAi could not find the prompt box. The Gemini site layout may have changed.',href:location.href};
  const before=assistants();
  const beforeSet=new Set(before);
  const responseForTurn=()=>{const current=assistants();for(let i=current.length-1;i>=0;i--){if(!beforeSet.has(current[i]))return current[i]}return null};
  c.focus();
  try{
    if(c instanceof HTMLTextAreaElement || c instanceof HTMLInputElement){
      const proto=c instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
      const setter=Object.getOwnPropertyDescriptor(proto,'value').set;setter.call(c,input);
      c.dispatchEvent(new Event('input',{bubbles:true}));c.dispatchEvent(new Event('change',{bubbles:true}));
    }else{
      const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);
      document.execCommand('delete',false,null);
      const lines=String(input).replace(/\r\n?/g,'\n').split('\n');
      for(let i=0;i<lines.length;i++){
        if(i>0)document.execCommand('insertLineBreak',false,null);
        if(lines[i])document.execCommand('insertText',false,lines[i]);
      }
      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true}));
    }
  }catch(e){c.textContent=input;c.dispatchEvent(new Event('input',{bubbles:true}))}
  await sleep(250);
  let b=null;
  for(let i=0;i<60&&!b;i++){b=send();if(!b)await sleep(100);}
  if(!b)return {ok:false,detail:'FlipAi filled the Gemini prompt box but the Send button never became ready.',href:location.href};
  b.click();
  let last='',stable=0,started=false;
  const deadline=Date.now()+90000;
  while(Date.now()<deadline){
    await sleep(250);
    const node=responseForTurn();
    if(node){
      started=true;
      const now=text(node);
      if(now===last)stable++;else{last=now;stable=0}
      if(now&&responseFinishedChrome(node)&&stable>=2)return {ok:true,reply:now,href:location.href};
      if(now&&!stop()&&stable>=5)return {ok:true,reply:now,href:location.href};
      if(now&&stable>=16)return {ok:true,reply:now,href:location.href};
    }
  }
  return {ok:false,detail:started?'Gemini started answering but did not finish within 90 seconds.':'Gemini did not produce a new response within 90 seconds.',href:location.href};
})(%s)`

type geminiChatTurnResult struct {
	OK     bool   `json:"ok"`
	Reply  string `json:"reply"`
	Detail string `json:"detail"`
	Href   string `json:"href"`
}

func geminiChatEval(d voiceDevTools, expression string, awaitPromise bool, out any) error {
	if d == nil {
		return errors.New("the Gemini Chat WebView has no in-process control channel")
	}
	var got voiceDevToolsEval
	params := map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": awaitPromise}
	if err := d.Call("Runtime.evaluate", params, &got); err != nil {
		return fmt.Errorf("the Gemini Chat WebView did not answer Runtime.evaluate: %w", err)
	}
	if len(got.ExceptionDetails) > 0 && string(got.ExceptionDetails) != "null" {
		return errors.New("the Gemini page script failed")
	}
	if out == nil {
		return nil
	}
	if len(got.Result.Value) == 0 {
		return errors.New("the Gemini page returned no value")
	}
	if err := json.Unmarshal(got.Result.Value, out); err != nil {
		return fmt.Errorf("the Gemini page returned an unreadable value: %w", err)
	}
	return nil
}

func geminiChatPageIsSignedIn(d voiceDevTools) bool {
	var v bool
	return geminiChatEval(d, geminiChatSignedInJS, true, &v) == nil && v
}
func waitForGeminiChatPageSignedIn(d voiceDevTools, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if geminiChatPageIsSignedIn(d) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func platformStartGeminiChatLogin(dataDir string) error {
	_ = platformStopGeminiChatWorker(dataDir)
	waitForGeminiChatStopped(dataDir, 4*time.Second)
	mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) {
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
	cmd := exec.Command(exe, "--gemini-chat-login")
	if err := cmd.Start(); err != nil {
		mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) { s.LoginActive = false; s.Starting = false; s.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformEnsureGeminiChatWorker(dataDir string) error {
	s := loadGeminiChatRuntime(dataDir)
	if s.LoginActive && (s.Running || s.Starting) {
		return nil
	}
	if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer cancel()
		if _, code, err := geminiChatControlRequest(ctx, s, http.MethodGet, "/health", nil); err == nil && code == http.StatusOK {
			return nil
		}
	}
	if s.Starting && time.Since(s.UpdatedAt) < 10*time.Second {
		return nil
	}
	mutateGeminiChatRuntime(dataDir, func(v *GeminiChatWebRuntime) {
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
	cmd := exec.Command(exe, "--gemini-chat-worker")
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		mutateGeminiChatRuntime(dataDir, func(v *GeminiChatWebRuntime) { v.Starting = false; v.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformStopGeminiChatWorker(dataDir string) error {
	s := loadGeminiChatRuntime(dataDir)
	for i := 0; i < 20 && s.ControlPort < 1 && s.Starting; i++ {
		time.Sleep(100 * time.Millisecond)
		s = loadGeminiChatRuntime(dataDir)
	}
	if s.ControlPort < 1 || s.ControlToken == "" {
		mutateGeminiChatRuntime(dataDir, func(v *GeminiChatWebRuntime) {
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
	_, _, err := geminiChatControlRequest(ctx, s, http.MethodPost, "/stop", strings.NewReader(`{}`))
	if err != nil {
		mutateGeminiChatRuntime(dataDir, func(v *GeminiChatWebRuntime) {
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

func runGeminiChatWebView(dataDir string, visible bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(geminiChatProfilePath(dataDir), 0700); err != nil {
		return err
	}
	opts := webview2.WebViewOptions{Debug: false, AutoFocus: visible, DataPath: geminiChatProfilePath(dataDir), WindowOptions: webview2.WindowOptions{Title: "Connect Gemini Chat to FlipAi", Width: 1120, Height: 820, Center: visible}}
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
		return errors.New("Microsoft Edge WebView2 Runtime could not create the Gemini Chat browser")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(800, 600, webview2.HintMin)
	initial := loadGeminiChatRuntime(dataDir)
	wasSignedIn := false
	hadConnected := initial.Connected
	_ = w.Bind("flipGeminiChatStatus", func(signedIn bool, href string) {
		changed := signedIn != wasSignedIn
		mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) {
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
				geminiChatActivity(dataDir, "info", "gemini-chat-session", "Saved Gemini Chat sign-in was restored.", 0)
			} else {
				geminiChatActivity(dataDir, "info", "gemini-chat-session", "Gemini Chat sign-in was verified and saved in FlipAi's dedicated profile.", 0)
				hadConnected = true
			}
		}
		wasSignedIn = signedIn
	})
	w.Init(geminiChatPageMonitorJS)
	dev := newWebViewDevTools(w)
	port, closer := startGeminiChatControlEndpoint(dataDir, w, dev)
	if closer != nil {
		defer closer.Close()
	}
	mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) {
		s.Running = true
		s.Starting = false
		s.Visible = visible
		s.LoginActive = visible
		s.ControlPort = port
		s.SignedIn = false
		s.LastEvent = "browser-starting"
		s.LastError = ""
	})
	w.Navigate(geminiChatWebURL)
	w.Run()
	mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) {
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
	return nil
}

func startGeminiChatControlEndpoint(dataDir string, w webview2.WebView, dev voiceDevTools) (int, io.Closer) {
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
	mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) { s.ControlToken = token; s.ControlPort = port })
	authorized := func(r *http.Request) bool { return token != "" && r.Header.Get("X-FlipAi-Token") == token }
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "signedIn": geminiChatPageIsSignedIn(dev)})
	})
	turn := func(rw http.ResponseWriter, r *http.Request, prompt string, newChat bool) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		if newChat {
			var ignored bool
			if err := geminiChatEval(dev, `(()=>{location.href='https://gemini.google.com';return true})()`, false, &ignored); err != nil {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
				return
			}
		}
		if !waitForGeminiChatPageSignedIn(dev, 25*time.Second) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Gemini Chat is not signed in inside FlipAi. Press Connect and complete sign-in first."})
			return
		}
		expr := fmt.Sprintf(geminiChatTurnJS, geminiChatJSString(prompt))
		var got geminiChatTurnResult
		if err := geminiChatEval(dev, expr, true, &got); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "FlipAi could not run the Gemini page driver: " + err.Error()})
			return
		}
		cid := geminiChatConversationID(got.Href)
		mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) {
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
		if r.Method != http.MethodPost {
			http.Error(rw, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		var ignored bool
		if err := geminiChatEval(dev, `(()=>{location.href='https://gemini.google.com';return true})()`, false, &ignored); err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
			return
		}
		if !waitForGeminiChatPageSignedIn(dev, 45*time.Second) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Gemini did not restore the saved sign-in after opening a new chat"})
			return
		}
		mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) {
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
		turn(rw, r, "Reply with exactly: FLIPAI_OK", true)
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

func recordGeminiChatWorkerError(dataDir string, err error) {
	if err == nil {
		return
	}
	mutateGeminiChatRuntime(dataDir, func(s *GeminiChatWebRuntime) {
		s.Running = false
		s.Starting = false
		s.Visible = false
		s.LoginActive = false
		s.SignedIn = false
		s.LastEvent = "browser-error"
		s.LastError = err.Error()
	})
	geminiChatActivity(dataDir, "error", "gemini-chat-session", "Gemini Chat browser stopped with an error: "+err.Error(), 0)
}
func geminiChatWorkerMain(dataDir string, visible bool) {
	if err := runGeminiChatWebView(dataDir, visible); err != nil {
		recordGeminiChatWorkerError(dataDir, err)
	}
}
