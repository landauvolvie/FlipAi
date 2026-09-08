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

const museChatPageMonitorJS = `(function(){
  if(window.__flipAiMuseChatMonitor)return;
  window.__flipAiMuseChatMonitor=true;
  const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}return out};
  const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};
  const composer=()=>first('textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[aria-label*="prompt" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],textarea[placeholder*="prompt" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],[contenteditable="true"][aria-label*="message" i],[contenteditable="true"][aria-label*="prompt" i],div[contenteditable="true"]');
  const loginPage=()=>location.hostname.toLowerCase()==='auth.muse.ai'||/(?:\/login|\/signin|\/sign-in|\/auth)(?:\/|$)/i.test(location.pathname)||!!first('form input[type="email"],form input[autocomplete="username"],form input[name*="email" i]');
  const signed=()=>{const h=location.hostname.toLowerCase();return (h==='muse.ai'||h==='www.muse.ai')&&!!composer()&&!loginPage()};
  async function tick(){try{if(window.flipMuseChatStatus)await window.flipMuseChatStatus(signed(),location.href)}catch(e){}}
  setInterval(tick,1000);addEventListener('load',tick);setTimeout(tick,350);
})();`

const museChatSignedInJS = `(()=>{const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}return out};const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};const c=first('textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[aria-label*="prompt" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],textarea[placeholder*="prompt" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],[contenteditable="true"][aria-label*="message" i],[contenteditable="true"][aria-label*="prompt" i],div[contenteditable="true"]');const h=location.hostname.toLowerCase();const loginPage=h==='auth.muse.ai'||/(?:\/login|\/signin|\/sign-in|\/auth)(?:\/|$)/i.test(location.pathname)||!!first('form input[type="email"],form input[autocomplete="username"],form input[name*="email" i]');return (h==='muse.ai'||h==='www.muse.ai')&&!!c&&!loginPage})()`

const museChatTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}return out};
  const all=q=>{const out=[];for(const r of roots())out.push(...r.querySelectorAll(q));return Array.from(new Set(out))};
  const first=q=>{for(const r of roots()){const n=r.querySelector(q);if(n)return n}return null};
  const composer=()=>first('textarea[data-testid*="composer" i],textarea[data-testid*="input" i],textarea[aria-label*="message" i],textarea[aria-label*="ask" i],textarea[aria-label*="prompt" i],textarea[placeholder*="message" i],textarea[placeholder*="ask" i],textarea[placeholder*="prompt" i],[contenteditable="true"][role="textbox"],[contenteditable="true"][data-testid*="input" i],[contenteditable="true"][aria-label*="message" i],[contenteditable="true"][aria-label*="prompt" i],div[contenteditable="true"]');
  const assistants=()=>{
    const primary=all('[data-content="ai-message"],[data-testid*="assistant" i],[data-testid*="bot" i],[data-message-author-role="assistant"],[data-author="bot"],[data-author="assistant"],[class*="assistant" i],[class*="response" i]').filter(n=>text(n)&&!n.closest('form'));
    if(primary.length)return primary;
    return all('main [role="article"],main article,main [class*="markdown" i],main .markdown,main .prose').filter(n=>text(n)&&!n.closest('form'));
  };
  const send=()=>{const xs=all('button[data-testid*="send" i],button[aria-label*="send" i],button[title*="send" i],button[type="submit"]');return xs.find(b=>!b.disabled&&b.offsetParent!==null)||xs.find(b=>!b.disabled)||null};
  const stop=()=>{const xs=all('button[data-testid*="stop" i],button[data-testid*="cancel" i],button[aria-label*="stop" i],button[aria-label*="cancel" i],button[title*="stop" i]');return xs.find(b=>!b.disabled&&b.offsetParent!==null)||null};
  let c=null;for(let i=0;i<120&&!c;i++){c=composer();if(!c)await sleep(200)}
  if(!c)return {ok:false,detail:'Muse is loaded but FlipAi could not find the prompt box. The Muse site layout may have changed.',href:location.href};
  const before=assistants(),beforeCount=before.length,beforeLast=beforeCount?text(before[beforeCount-1]):'';
  const responseForTurn=()=>{const current=assistants();if(!current.length)return null;const last=current[current.length-1];if(current.length>beforeCount)return last;return text(last)&&text(last)!==beforeLast?last:null};
  c.focus();
  try{
    if(c instanceof HTMLTextAreaElement||c instanceof HTMLInputElement){const proto=c instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;const setter=Object.getOwnPropertyDescriptor(proto,'value').set;setter.call(c,input);c.dispatchEvent(new Event('input',{bubbles:true,composed:true}));c.dispatchEvent(new Event('change',{bubbles:true,composed:true}))}
    else{const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);document.execCommand('delete',false,null);document.execCommand('insertText',false,input);c.dispatchEvent(new InputEvent('input',{bubbles:true,composed:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true,composed:true}))}
  }catch(e){c.textContent=input;c.dispatchEvent(new Event('input',{bubbles:true,composed:true}))}
  await sleep(350);
  let b=null;for(let i=0;i<80&&!b;i++){b=send();if(!b)await sleep(100)}
  if(b)b.click();else if(c.form&&c.form.requestSubmit)c.form.requestSubmit();else{c.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',code:'Enter',bubbles:true,composed:true}));c.dispatchEvent(new KeyboardEvent('keyup',{key:'Enter',code:'Enter',bubbles:true,composed:true}))}
  let last='',stable=0,started=false;const deadline=Date.now()+90000;
  while(Date.now()<deadline){await sleep(250);const node=responseForTurn();if(node){started=true;const now=text(node);if(now===last)stable++;else{last=now;stable=0}if(!stop()&&stable>=5)return {ok:true,reply:now||'Muse completed the turn.',href:location.href}}}
  return {ok:false,detail:started?'Muse started answering but did not finish within 90 seconds.':'Muse did not produce a new response within 90 seconds.',href:location.href};
})(%s)`

type museChatTurnResult struct {
	OK     bool   `json:"ok"`
	Reply  string `json:"reply"`
	Detail string `json:"detail"`
	Href   string `json:"href"`
}

func museChatEval(d voiceDevTools, expression string, awaitPromise bool, out any) error {
	if d == nil {
		return errors.New("the Muse WebView has no in-process control channel")
	}
	var got voiceDevToolsEval
	params := map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": awaitPromise}
	if err := d.Call("Runtime.evaluate", params, &got); err != nil {
		return fmt.Errorf("the Muse WebView did not answer Runtime.evaluate: %w", err)
	}
	if len(got.ExceptionDetails) > 0 && string(got.ExceptionDetails) != "null" {
		return errors.New("the Muse page script failed")
	}
	if out == nil {
		return nil
	}
	if len(got.Result.Value) == 0 {
		return errors.New("the Muse page returned no value")
	}
	if err := json.Unmarshal(got.Result.Value, out); err != nil {
		return fmt.Errorf("the Muse page returned an unreadable value: %w", err)
	}
	return nil
}

func museChatPageIsSignedIn(d voiceDevTools) bool {
	var v bool
	return museChatEval(d, museChatSignedInJS, true, &v) == nil && v
}

func waitForMuseChatPageSignedIn(d voiceDevTools, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if museChatPageIsSignedIn(d) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func platformStartMuseChatLogin(dataDir string) error {
	_ = platformStopMuseChatWorker(dataDir)
	waitForMuseChatStopped(dataDir, 4*time.Second)
	mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) {
		s.LoginActive, s.Starting, s.Running, s.Visible, s.SignedIn = true, true, false, false, false
		s.LastEvent, s.LastError = "sign-in-window-starting", ""
	})
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--muse-chat-login")
	if err := cmd.Start(); err != nil {
		mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) { s.LoginActive, s.Starting = false, false; s.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformEnsureMuseChatWorker(dataDir string) error {
	s := loadMuseChatRuntime(dataDir)
	if s.LoginActive && (s.Running || s.Starting) {
		return nil
	}
	if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
		defer cancel()
		if _, code, err := museChatControlRequest(ctx, s, http.MethodGet, "/health", nil); err == nil && code == http.StatusOK {
			return nil
		}
	}
	if s.Starting && time.Since(s.UpdatedAt) < 10*time.Second {
		return nil
	}
	mutateMuseChatRuntime(dataDir, func(v *MuseChatWebRuntime) {
		v.Starting, v.LoginActive, v.Running, v.Visible, v.SignedIn = true, false, false, false, false
		v.ControlPort, v.ControlToken = 0, ""
		v.LastEvent, v.LastError = "background-starting", ""
	})
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--muse-chat-worker")
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		mutateMuseChatRuntime(dataDir, func(v *MuseChatWebRuntime) { v.Starting = false; v.LastError = err.Error() })
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

func platformStopMuseChatWorker(dataDir string) error {
	s := loadMuseChatRuntime(dataDir)
	for i := 0; i < 20 && s.ControlPort < 1 && s.Starting; i++ {
		time.Sleep(100 * time.Millisecond)
		s = loadMuseChatRuntime(dataDir)
	}
	if s.ControlPort < 1 || s.ControlToken == "" {
		mutateMuseChatRuntime(dataDir, func(v *MuseChatWebRuntime) {
			v.Running, v.Starting, v.Visible, v.LoginActive, v.SignedIn = false, false, false, false, false
			v.ControlPort, v.ControlToken = 0, ""
		})
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := museChatControlRequest(ctx, s, http.MethodPost, "/stop", strings.NewReader(`{}`))
	if err != nil {
		mutateMuseChatRuntime(dataDir, func(v *MuseChatWebRuntime) {
			v.Running, v.Starting, v.Visible, v.LoginActive, v.SignedIn = false, false, false, false, false
			v.ControlPort, v.ControlToken = 0, ""
		})
	}
	return err
}

func runMuseChatWebView(dataDir string, visible bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(museChatProfilePath(dataDir), 0700); err != nil {
		return err
	}
	opts := webview2.WebViewOptions{Debug: false, AutoFocus: visible, DataPath: museChatProfilePath(dataDir), WindowOptions: webview2.WindowOptions{Title: "Connect Muse to FlipAi", Width: 1120, Height: 820, Center: visible}}
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
		return errors.New("Microsoft Edge WebView2 Runtime could not create the Muse browser")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(800, 600, webview2.HintMin)
	initial := loadMuseChatRuntime(dataDir)
	wasSignedIn := false
	hadConnected := initial.Connected
	_ = w.Bind("flipMuseChatStatus", func(signedIn bool, href string) {
		changed := signedIn != wasSignedIn
		mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) {
			s.Running, s.Starting, s.Visible, s.LoginActive, s.SignedIn = true, false, visible, visible, signedIn
			s.LastURL = href
			if signedIn {
				s.Connected = true
				s.LastError = ""
				if changed || strings.Contains(s.LastEvent, "starting") { s.LastEvent = "session-ready" }
			} else if changed || strings.Contains(s.LastEvent, "starting") {
				if s.Connected { s.LastEvent = "session-restoring" } else { s.LastEvent = "waiting-for-sign-in" }
			}
		})
		if signedIn && !wasSignedIn {
			if hadConnected { museChatActivity(dataDir, "info", "muse-chat-session", "Saved Muse sign-in was restored.", 0) } else { museChatActivity(dataDir, "info", "muse-chat-session", "Muse sign-in was verified and saved in FlipAi's dedicated profile.", 0); hadConnected = true }
		}
		wasSignedIn = signedIn
	})
	w.Init(museChatPageMonitorJS)
	dev := newWebViewDevTools(w)
	port, closer := startMuseChatControlEndpoint(dataDir, w, dev)
	if closer != nil { defer closer.Close() }
	mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) {
		s.Running, s.Starting, s.Visible, s.LoginActive, s.SignedIn = true, false, visible, visible, false
		s.ControlPort = port
		s.LastEvent, s.LastError = "browser-starting", ""
	})
	w.Navigate(museChatWebURL)
	w.Run()
	mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) {
		s.Running, s.Starting, s.Visible, s.LoginActive, s.SignedIn = false, false, false, false, false
		s.ControlPort, s.ControlToken = 0, ""
		if s.Connected { s.LastEvent = "background-restart-pending" } else { s.LastEvent = "browser-closed" }
	})
	return nil
}

func startMuseChatControlEndpoint(dataDir string, w webview2.WebView, dev voiceDevTools) (int, io.Closer) {
	if dev == nil { return 0, nil }
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { return 0, nil }
	token, err := secureRandomToken(24)
	if err != nil { _ = ln.Close(); return 0, nil }
	port := ln.Addr().(*net.TCPAddr).Port
	mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) { s.ControlToken, s.ControlPort = token, port })
	authorized := func(r *http.Request) bool { return token != "" && r.Header.Get("X-FlipAi-Token") == token }
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "signedIn": museChatPageIsSignedIn(dev)})
	})
	turn := func(rw http.ResponseWriter, r *http.Request, prompt string, newChat bool) {
		if !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }
		if newChat {
			var ignored bool
			if err := museChatEval(dev, `(()=>{location.href='https://muse.ai/';return true})()`, false, &ignored); err != nil { rw.WriteHeader(http.StatusBadGateway); _ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()}); return }
		}
		if !waitForMuseChatPageSignedIn(dev, 25*time.Second) { rw.WriteHeader(http.StatusUnauthorized); _ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Muse is not ready inside FlipAi. Press Connect and complete sign-in first."}); return }
		cleanPrompt, attachments, marked, markerErr := extractBrowserChatAttachmentMarker(prompt)
		if marked {
			if markerErr != nil { rw.WriteHeader(http.StatusBadRequest); _ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": markerErr.Error()}); return }
			if err := uploadBrowserChatImages(dev, attachments); err != nil { rw.WriteHeader(http.StatusBadGateway); _ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()}); return }
			prompt = strings.TrimSpace(cleanPrompt)
			if prompt == "" { prompt = browserChatImageOnlyPrompt(len(attachments)) }
		}
		expr := fmt.Sprintf(museChatTurnJS, museChatJSString(prompt))
		var got museChatTurnResult
		if err := museChatEval(dev, expr, true, &got); err != nil { rw.WriteHeader(http.StatusInternalServerError); _ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "FlipAi could not run the Muse page driver: " + err.Error()}); return }
		cid := museChatConversationID(got.Href)
		mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) {
			s.Connected, s.SignedIn, s.LastURL, s.ConversationID = true, true, got.Href, cid
			if got.OK { s.LastEvent, s.LastError = "turn-complete", "" } else { s.LastEvent, s.LastError = "turn-failed", got.Detail }
		})
		status := http.StatusOK
		if !got.OK { status = http.StatusBadGateway }
		rw.WriteHeader(status)
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": got.OK, "reply": got.Reply, "detail": got.Detail, "conversationId": cid})
	}
	mux.HandleFunc("/new", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { http.Error(rw, "POST required", http.StatusMethodNotAllowed); return }
		if !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }
		var ignored bool
		if err := museChatEval(dev, `(()=>{location.href='https://muse.ai/';return true})()`, false, &ignored); err != nil { rw.WriteHeader(http.StatusBadGateway); _ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()}); return }
		if !waitForMuseChatPageSignedIn(dev, 45*time.Second) { rw.WriteHeader(http.StatusUnauthorized); _ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Muse did not restore the saved session after opening a new chat"}); return }
		mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) { s.Connected, s.SignedIn, s.ConversationID, s.LastEvent, s.LastError = true, true, "", "new-chat-ready", "" })
		_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { http.Error(rw, "POST required", http.StatusMethodNotAllowed); return }
		turn(rw, r, "Reply with exactly: FLIPAI_OK", true)
	})
	mux.HandleFunc("/chat", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { http.Error(rw, "POST required", http.StatusMethodNotAllowed); return }
		var body struct { Prompt string `json:"prompt"`; New bool `json:"new"` }
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 64<<10)).Decode(&body); err != nil { http.Error(rw, err.Error(), http.StatusBadRequest); return }
		body.Prompt = strings.TrimSpace(body.Prompt)
		if body.Prompt == "" { http.Error(rw, "prompt required", http.StatusBadRequest); return }
		turn(rw, r, body.Prompt, body.New)
	})
	mux.HandleFunc("/stop", func(rw http.ResponseWriter, r *http.Request) {
		if !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }
		_ = json.NewEncoder(rw).Encode(map[string]bool{"ok": true})
		go func() { time.Sleep(80 * time.Millisecond); w.Terminate() }()
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 4 * time.Second, WriteTimeout: 115 * time.Second}
	go func() { _ = server.Serve(ln) }()
	return port, server
}

func recordMuseChatWorkerError(dataDir string, err error) {
	if err == nil { return }
	mutateMuseChatRuntime(dataDir, func(s *MuseChatWebRuntime) {
		s.Running, s.Starting, s.Visible, s.LoginActive, s.SignedIn = false, false, false, false, false
		s.LastEvent, s.LastError = "browser-error", err.Error()
	})
	museChatActivity(dataDir, "error", "muse-chat-session", "Muse browser stopped with an error: "+err.Error(), 0)
}

func museChatWorkerMain(dataDir string, visible bool) {
	if err := runMuseChatWebView(dataDir, visible); err != nil { recordMuseChatWorkerError(dataDir, err) }
}
