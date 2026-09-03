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

const grokChatPageMonitorJS = `(function(){
  if(window.__flipAiGrokChatMonitor)return;
  window.__flipAiGrokChatMonitor=true;
  const composer=()=>document.querySelector('div.ProseMirror[contenteditable="true"][role="textbox"],div.tiptap.ProseMirror[contenteditable="true"],[data-testid="grokInput"][contenteditable="true"],[data-testid="grokInput"],[contenteditable="true"][role="textbox"],textarea[placeholder],textarea');
  const signIn=()=>Array.from(document.querySelectorAll('a,button')).find(n=>/sign in|log in/i.test(((n.getAttribute('aria-label')||'')+' '+(n.innerText||'')).trim()));
  const signed=()=>location.hostname==='grok.com' && !!composer() && !signIn();
  async function tick(){
    try{ if(window.flipGrokChatStatus) await window.flipGrokChatStatus(signed(), location.href); }catch(e){}
  }
  setInterval(tick,1000); addEventListener('load',tick); setTimeout(tick,350);
})();`

const grokChatSignedInJS = `(()=>{const c=document.querySelector('rich-textarea .ql-editor[contenteditable="true"],rich-textarea [contenteditable="true"],div.ql-editor[contenteditable="true"],[contenteditable="true"][role="textbox"],[contenteditable="true"][aria-label*="prompt" i],textarea[aria-label*="prompt" i],textarea');const signIn=Array.from(document.querySelectorAll('a,button')).find(n=>/sign in|log in/i.test(((n.getAttribute('aria-label')||'')+' '+(n.innerText||'')).trim()));return location.hostname==='grok.com'&&!!c&&!signIn})()`

const grokChatTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const all=q=>Array.from(document.querySelectorAll(q));
  const unique=xs=>Array.from(new Set(xs));
  const assistants=()=>{
    const primary=unique([...all('[data-testid="grokResponse"]'),...all('[data-testid*="response" i]'),...all('[data-message-author-role="assistant"]')]).filter(n=>text(n));
    if(primary.length)return primary;
    return unique([...all('.markdown'),...all('[class*="markdown"]'),...all('.prose')]).filter(n=>text(n) && !n.closest('form'));
  };
  const composer=()=>document.querySelector('div.ProseMirror[contenteditable="true"][role="textbox"],div.tiptap.ProseMirror[contenteditable="true"],[data-testid="grokInput"][contenteditable="true"],[data-testid="grokInput"],[contenteditable="true"][role="textbox"],textarea[placeholder],textarea');
  const send=()=>{
    const candidates=unique([...all('button[data-testid="chat-submit"]'),...all('button[data-testid="grokSend"]'),...all('button[aria-label*="send" i]'),...all('button[type="submit"]')]);
    return candidates.find(b=>!b.disabled && b.offsetParent!==null)||candidates.find(b=>!b.disabled)||null;
  };
  const stop=()=>document.querySelector('button[data-testid*="stop" i],button[aria-label*="stop" i]');
  let c=null;
  for(let i=0;i<120&&!c;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'Grok is loaded but FlipAi could not find the prompt box. The Grok site layout may have changed.',href:location.href};
  const before=assistants();
  const beforeSet=new Set(before);
  const responseForTurn=()=>{const current=assistants();for(let i=current.length-1;i>=0;i--){if(!beforeSet.has(current[i]))return current[i]}return null};
  c.focus();
  try{
    if(c instanceof HTMLTextAreaElement || c instanceof HTMLInputElement){
      const proto=c instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
      const setter=Object.getOwnPropertyDescriptor(proto,'value').set;setter.call(c,input);
      c.dispatchEvent(new Event('input',{bubbles:true}));c.dispatchEvent(new Event('change',{bubbles:true}));
    }else if(c.editor&&c.editor.commands){
      if(c.editor.commands.clearContent)c.editor.commands.clearContent();
      c.editor.commands.insertContent(input);
      c.dispatchEvent(new Event('input',{bubbles:true}));
    }else{
      const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);
      document.execCommand('delete',false,null);document.execCommand('insertText',false,input);
      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true}));
    }
  }catch(e){c.textContent=input;c.dispatchEvent(new Event('input',{bubbles:true}))}
  await sleep(250);
  let b=null;
  for(let i=0;i<60&&!b;i++){b=send();if(!b)await sleep(100);}
  if(!b)return {ok:false,detail:'FlipAi filled the Grok prompt box but the Send button never became ready.',href:location.href};
  b.click();
  let last='',stable=0,started=false;
  const deadline=Date.now()+90000;
  while(Date.now()<deadline){
    await sleep(250);
    const node=responseForTurn();
    if(node){started=true;const now=text(node);if(now===last)stable++;else{last=now;stable=0}if(!stop()&&stable>=5)return {ok:true,reply:now||'Grok completed the turn.',href:location.href}}
  }
  return {ok:false,detail:started?'Grok started answering but did not finish within 90 seconds.':'Grok did not produce a new response within 90 seconds.',href:location.href};
})(%s)`

type grokChatTurnResult struct {
	OK     bool   `json:"ok"`
	Reply  string `json:"reply"`
	Detail string `json:"detail"`
	Href   string `json:"href"`
}

func grokChatEval(d voiceDevTools, expression string, awaitPromise bool, out any) error {
	if d == nil {
		return errors.New("the Grok Chat WebView has no in-process control channel")
	}
	var got voiceDevToolsEval
	params := map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": awaitPromise}
	if err := d.Call("Runtime.evaluate", params, &got); err != nil {
		return fmt.Errorf("the Grok Chat WebView did not answer Runtime.evaluate: %w", err)
	}
	if len(got.ExceptionDetails) > 0 && string(got.ExceptionDetails) != "null" {
		return errors.New("the Grok page script failed")
	}
	if out == nil {
		return nil
	}
	if len(got.Result.Value) == 0 {
		return errors.New("the Grok page returned no value")
	}
	if err := json.Unmarshal(got.Result.Value, out); err != nil {
		return fmt.Errorf("the Grok page returned an unreadable value: %w", err)
	}
	return nil
}

func grokChatPageIsSignedIn(d voiceDevTools) bool {
	var v bool
	return grokChatEval(d, grokChatSignedInJS, true, &v) == nil && v
}
func waitForGrokChatPageSignedIn(d voiceDevTools, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if grokChatPageIsSignedIn(d) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func platformStartGrokChatLogin(dataDir string) error {
	_ = platformStopGrokChatWorker(dataDir)
	waitForGrokChatStopped(dataDir, 4*time.Second)
	mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) {
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
	cmd := exec.Command(exe, "--grok-chat-login")
	if err := cmd.Start(); err != nil {
		mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) { s.LoginActive = false; s.Starting = false; s.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformEnsureGrokChatWorker(dataDir string) error {
	s := loadGrokChatRuntime(dataDir)
	if s.LoginActive && (s.Running || s.Starting) {
		return nil
	}
	if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer cancel()
		if _, code, err := grokChatControlRequest(ctx, s, http.MethodGet, "/health", nil); err == nil && code == http.StatusOK {
			return nil
		}
	}
	if s.Starting && time.Since(s.UpdatedAt) < 10*time.Second {
		return nil
	}
	mutateGrokChatRuntime(dataDir, func(v *GrokChatWebRuntime) {
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
	cmd := exec.Command(exe, "--grok-chat-worker")
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		mutateGrokChatRuntime(dataDir, func(v *GrokChatWebRuntime) { v.Starting = false; v.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformStopGrokChatWorker(dataDir string) error {
	s := loadGrokChatRuntime(dataDir)
	for i := 0; i < 20 && s.ControlPort < 1 && s.Starting; i++ {
		time.Sleep(100 * time.Millisecond)
		s = loadGrokChatRuntime(dataDir)
	}
	if s.ControlPort < 1 || s.ControlToken == "" {
		mutateGrokChatRuntime(dataDir, func(v *GrokChatWebRuntime) {
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
	_, _, err := grokChatControlRequest(ctx, s, http.MethodPost, "/stop", strings.NewReader(`{}`))
	if err != nil {
		mutateGrokChatRuntime(dataDir, func(v *GrokChatWebRuntime) {
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

func runGrokChatWebView(dataDir string, visible bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(grokChatProfilePath(dataDir), 0700); err != nil {
		return err
	}
	opts := webview2.WebViewOptions{Debug: false, AutoFocus: visible, DataPath: grokChatProfilePath(dataDir), WindowOptions: webview2.WindowOptions{Title: "Connect Grok Chat to FlipAi", Width: 1120, Height: 820, Center: visible}}
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
		return errors.New("Microsoft Edge WebView2 Runtime could not create the Grok Chat browser")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(800, 600, webview2.HintMin)
	initial := loadGrokChatRuntime(dataDir)
	wasSignedIn := false
	hadConnected := initial.Connected
	_ = w.Bind("flipGrokChatStatus", func(signedIn bool, href string) {
		changed := signedIn != wasSignedIn
		mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) {
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
				grokChatActivity(dataDir, "info", "grok-chat-session", "Saved Grok Chat sign-in was restored.", 0)
			} else {
				grokChatActivity(dataDir, "info", "grok-chat-session", "Grok Chat sign-in was verified and saved in FlipAi's dedicated profile.", 0)
				hadConnected = true
			}
		}
		wasSignedIn = signedIn
	})
	w.Init(grokChatPageMonitorJS)
	dev := newWebViewDevTools(w)
	port, closer := startGrokChatControlEndpoint(dataDir, w, dev)
	if closer != nil {
		defer closer.Close()
	}
	mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) {
		s.Running = true
		s.Starting = false
		s.Visible = visible
		s.LoginActive = visible
		s.ControlPort = port
		s.SignedIn = false
		s.LastEvent = "browser-starting"
		s.LastError = ""
	})
	w.Navigate(grokChatWebURL)
	w.Run()
	mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) {
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

func startGrokChatControlEndpoint(dataDir string, w webview2.WebView, dev voiceDevTools) (int, io.Closer) {
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
	mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) { s.ControlToken = token; s.ControlPort = port })
	authorized := func(r *http.Request) bool { return token != "" && r.Header.Get("X-FlipAi-Token") == token }
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "signedIn": grokChatPageIsSignedIn(dev)})
	})
	turn := func(rw http.ResponseWriter, r *http.Request, prompt string, newChat bool) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		if newChat {
			var ignored bool
			if err := grokChatEval(dev, `(()=>{location.href='https://grok.com';return true})()`, false, &ignored); err != nil {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
				return
			}
		}
		if !waitForGrokChatPageSignedIn(dev, 25*time.Second) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Grok Chat is not signed in inside FlipAi. Press Connect and complete sign-in first."})
			return
		}
		expr := fmt.Sprintf(grokChatTurnJS, grokChatJSString(prompt))
		var got grokChatTurnResult
		if err := grokChatEval(dev, expr, true, &got); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "FlipAi could not run the Grok page driver: " + err.Error()})
			return
		}
		cid := grokChatConversationID(got.Href)
		mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) {
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
		if err := grokChatEval(dev, `(()=>{location.href='https://grok.com';return true})()`, false, &ignored); err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
			return
		}
		if !waitForGrokChatPageSignedIn(dev, 45*time.Second) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Grok did not restore the saved sign-in after opening a new chat"})
			return
		}
		mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) {
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

func recordGrokChatWorkerError(dataDir string, err error) {
	if err == nil {
		return
	}
	mutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) {
		s.Running = false
		s.Starting = false
		s.Visible = false
		s.LoginActive = false
		s.SignedIn = false
		s.LastEvent = "browser-error"
		s.LastError = err.Error()
	})
	grokChatActivity(dataDir, "error", "grok-chat-session", "Grok Chat browser stopped with an error: "+err.Error(), 0)
}
func grokChatWorkerMain(dataDir string, visible bool) {
	if err := runGrokChatWebView(dataDir, visible); err != nil {
		recordGrokChatWorkerError(dataDir, err)
	}
}
