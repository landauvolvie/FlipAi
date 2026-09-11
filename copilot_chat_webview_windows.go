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
  const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}return out};
  const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};
  const composer=()=>first('textarea#userInput,textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],div[contenteditable="true"]');
  const loginPage=()=>/(?:login\.live\.com|login\.microsoftonline\.com)$/i.test(location.hostname)||/(?:\/login|\/signin|\/sign-in)(?:\/|$)/i.test(location.pathname)||!!first('form input[type="email"],form input[name="loginfmt"],form input[autocomplete="username"]');
  const signed=()=>/(^|\.)copilot\.microsoft\.com$/i.test(location.hostname)&&!!composer()&&!loginPage();
  async function tick(){try{if(window.flipCopilotChatStatus)await window.flipCopilotChatStatus(signed(),location.href)}catch(e){}}
  setInterval(tick,1000);addEventListener('load',tick);setTimeout(tick,350);
})();`

const copilotChatSignedInJS = `(()=>{const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}return out};const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};const c=first('textarea#userInput,textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],div[contenteditable="true"]');const loginPage=/(?:login\.live\.com|login\.microsoftonline\.com)$/i.test(location.hostname)||/(?:\/login|\/signin|\/sign-in)(?:\/|$)/i.test(location.pathname)||!!first('form input[type="email"],form input[name="loginfmt"],form input[autocomplete="username"]');return /(^|\.)copilot\.microsoft\.com$/i.test(location.hostname)&&!!c&&!loginPage})()`

const copilotChatTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}return out};
  const all=q=>{const out=[];for(const r of roots())out.push(...r.querySelectorAll(q));return Array.from(new Set(out))};
  const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};
  const assistants=()=>{
    const primary=all('[data-content="ai-message"],[data-testid*="assistant" i],[data-testid*="bot" i],[data-message-author-role="assistant"],[data-author="bot"],[data-author="assistant"],[class*="response-message" i],[class*="assistant-message" i]').filter(n=>text(n)&&!n.closest('form'));
    if(primary.length)return primary;
    const articles=all('main [role="article"],main [class*="markdown" i],main .markdown,main .prose').filter(n=>text(n)&&!n.closest('form'));
    return articles;
  };
  const composer=()=>first('textarea#userInput,textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],div[contenteditable="true"]');
  const send=()=>{const xs=all('button[data-testid*="send" i],button[aria-label*="send" i],button[title*="send" i],button[type="submit"]');return xs.find(b=>!b.disabled&&b.offsetParent!==null)||xs.find(b=>!b.disabled)||null};
  const stop=()=>{const xs=all('button[data-testid*="stop" i],button[data-testid*="cancel" i],button[aria-label*="stop" i],button[aria-label*="cancel" i],button[title*="stop" i]');return xs.find(b=>!b.disabled&&b.offsetParent!==null)||null};
  let c=null;
  for(let i=0;i<120&&!c;i++){c=composer();if(!c)await sleep(200)}
  if(!c)return {ok:false,detail:'Microsoft Copilot is loaded but FlipAi could not find the prompt box. The Copilot site layout may have changed.',href:location.href};
  const before=assistants();const beforeCount=before.length;const beforeLast=beforeCount?text(before[beforeCount-1]):'';
  const responseForTurn=()=>{const current=assistants();if(!current.length)return null;const last=current[current.length-1];if(current.length>beforeCount)return last;return text(last)&&text(last)!==beforeLast?last:null};
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
  if(!b)return {ok:false,detail:'FlipAi filled the Microsoft Copilot prompt box but the Send button never became ready.',href:location.href};
  b.click();
  let last='',stable=0,started=false;const deadline=Date.now()+90000;
  while(Date.now()<deadline){
    await sleep(250);const node=responseForTurn();
    if(node){started=true;const now=text(node);if(now===last)stable++;else{last=now;stable=0}if(!stop()&&stable>=5)return {ok:true,reply:now||'Microsoft Copilot completed the turn.',href:location.href}}
  }
  return {ok:false,detail:started?'Microsoft Copilot started answering but did not finish within 90 seconds.':'Microsoft Copilot did not produce a new response within 90 seconds.',href:location.href};
})(%s)`

type copilotChatTurnResult struct {
	OK     bool   `json:"ok"`
	Reply  string `json:"reply"`
	Detail string `json:"detail"`
	Href   string `json:"href"`
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
		return errors.New("the Microsoft Copilot page script failed")
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
			var ignored bool
			if err := copilotChatEval(dev, `(()=>{location.href='https://copilot.microsoft.com/';return true})()`, false, &ignored); err != nil {
				rw.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
				return
			}
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
		if err := copilotChatEval(dev, `(()=>{location.href='https://copilot.microsoft.com/';return true})()`, false, &ignored); err != nil {
			rw.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
			return
		}
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
