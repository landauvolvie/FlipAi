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

// chatGPTTurnJS deliberately uses ChatGPT's own page controls inside FlipAi's
// private WebView. It does not use Windows accessibility, global keyboard/mouse
// input, the user's visible ChatGPT app, or coordinates.
const chatGPTTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
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
      if(!stop()&&stable>=5)return {ok:true,reply:now||'I generated the image.',href:location.href};
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

// chatGPTEval uses the same in-process WebView2 DevTools channel as the Google
// Voice browser but has ChatGPT-specific diagnostics. No port is exposed and no
// remote debugging switch is enabled.
func chatGPTEval(d voiceDevTools, expression string, awaitPromise bool, out any) error {
	if d == nil {
		return errors.New("the ChatGPT WebView has no in-process control channel")
	}
	var got voiceDevToolsEval
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  awaitPromise,
	}
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
	// The tray supervisor and a user turn can notice a missing worker at nearly
	// the same moment. Starting is persisted before spawning so the second one
	// waits for the first child rather than opening the profile twice.
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
	// A just-spawned child may not have published its control port yet. Give it
	// a short chance to do so; this makes Quit/Disconnect deterministic instead
	// of leaving an orphan that still owns the WebView2 profile.
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
		signed := chatGPTPageIsSignedIn(dev)
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "signedIn": signed})
	})
	turn := func(rw http.ResponseWriter, r *http.Request, prompt string, newChat bool) {
		if !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		if newChat {
			var ignored bool
			if err := chatGPTEval(dev, `(()=>{location.href='https://chatgpt.com/';return true})()`, false, &ignored); err != nil {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
				return
			}
		}
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
		var ignored bool
		if err := chatGPTEval(dev, `(()=>{location.href='https://chatgpt.com/';return true})()`, false, &ignored); err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
			return
		}
		if !waitForChatGPTPageSignedIn(dev, 45*time.Second) {
			rw.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "ChatGPT did not restore the saved sign-in after opening a new chat"})
			return
		}
		mutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {
			s.Connected = true; s.SignedIn = true; s.ConversationID = ""
			s.LastEvent = "new-chat-ready"; s.LastError = ""
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
		go func() {
			time.Sleep(80 * time.Millisecond)
			w.Terminate()
		}()
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
