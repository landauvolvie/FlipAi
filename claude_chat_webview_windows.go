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

const claudeChatPageMonitorJS = `(function(){
  if(window.__flipAiClaudeChatMonitor)return;
  window.__flipAiClaudeChatMonitor=true;
  const composer=()=>document.querySelector('[data-testid="chat-input"],div.ProseMirror[contenteditable="true"],div[contenteditable="true"][role="textbox"],div[contenteditable="true"]');
  async function tick(){
    try{ if(window.flipClaudeChatStatus) await window.flipClaudeChatStatus(!!composer() && !/\/login(?:[/?#]|$)/i.test(location.pathname), location.href); }catch(e){}
  }
  setInterval(tick,1000); addEventListener('load',tick); setTimeout(tick,350);
})();`

const claudeChatSignedInJS = `(()=>!!document.querySelector('[data-testid="chat-input"],div.ProseMirror[contenteditable="true"],div[contenteditable="true"][role="textbox"],div[contenteditable="true"]') && !/\/login(?:[/?#]|$)/i.test(location.pathname))()`

// Claude's classes change frequently, so every role uses several independent
// selectors. Stable data-testid/ARIA controls win; class names are only fallback.
const claudeChatTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const all=(q)=>Array.from(document.querySelectorAll(q));
  const unique=(xs)=>Array.from(new Set(xs));
  const assistants=()=>unique([
    ...all('[data-testid="assistant-message"]'),
    ...all('[data-testid="chat-message-content"]'),
    ...all('.font-claude-response-body'),
    ...all('.font-claude-message'),
    ...all('.standard-markdown')
  ]).filter(n=>text(n));
  const composer=()=>document.querySelector('[data-testid="chat-input"],div.ProseMirror[contenteditable="true"],div[data-placeholder][contenteditable="true"],div[contenteditable="true"][role="textbox"],div[contenteditable="true"]');
  const send=()=>document.querySelector('button[data-testid="send-button"],button[aria-label="Send Message"],button[aria-label="Send message"],button[aria-label="Send"],button[aria-label*="Send" i]');
  const stop=()=>document.querySelector('button[data-testid="stop-button"],button[aria-label*="Stop" i],[data-is-streaming="true"]');
  let c=null;
  for(let i=0;i<100&&!c;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'Claude is loaded but FlipAi could not find the message composer. The Claude site layout may have changed.',href:location.href};
  const before=assistants();
  const beforeSet=new Set(before);
  const responseForTurn=()=>{
    const current=assistants();
    for(let i=current.length-1;i>=0;i--){if(!beforeSet.has(current[i]))return current[i];}
    return null;
  };
  c.focus();
  try{
    const sel=getSelection(),range=document.createRange();
    range.selectNodeContents(c); sel.removeAllRanges(); sel.addRange(range);
    document.execCommand('delete',false,null);
    document.execCommand('insertText',false,input);
    c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));
    c.dispatchEvent(new Event('change',{bubbles:true}));
  }catch(e){
    c.textContent=input;c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));
  }
  await sleep(180);
  let b=null;
  for(let i=0;i<50&&!b;i++){let x=send();if(x&&!x.disabled)b=x;else await sleep(100);}
  if(!b){
    const form=c.closest('form');const x=form&&form.querySelector('button[type="submit"]');if(x&&!x.disabled)b=x;
  }
  if(!b)return {ok:false,detail:'FlipAi filled the Claude composer but the Send button never became ready.',href:location.href};
  b.click();
  let last='',stable=0,started=false;
  const deadline=Date.now()+90000;
  while(Date.now()<deadline){
    await sleep(250);
    const node=responseForTurn();
    if(node){
      started=true;const now=text(node);
      if(now===last)stable++;else{last=now;stable=0;}
      if(!stop()&&stable>=5)return {ok:true,reply:now||'Claude completed the turn.',href:location.href};
    }
  }
  return {ok:false,detail:started?'Claude started answering but did not finish within 90 seconds.':'Claude did not produce a new assistant response within 90 seconds.',href:location.href};
})(%s)`

type claudeChatTurnResult struct {
	OK bool `json:"ok"`
	Reply string `json:"reply"`
	Detail string `json:"detail"`
	Href string `json:"href"`
}

func claudeChatEval(d voiceDevTools, expression string, awaitPromise bool, out any) error {
	if d == nil { return errors.New("the Claude Chat WebView has no in-process control channel") }
	var got voiceDevToolsEval
	params := map[string]any{"expression":expression,"returnByValue":true,"awaitPromise":awaitPromise}
	if err := d.Call("Runtime.evaluate", params, &got); err != nil { return fmt.Errorf("the Claude Chat WebView did not answer Runtime.evaluate: %w", err) }
	if len(got.ExceptionDetails)>0 && string(got.ExceptionDetails)!="null" { return errors.New("the Claude page script failed") }
	if out==nil { return nil }
	if len(got.Result.Value)==0 { return errors.New("the Claude page returned no value") }
	if err:=json.Unmarshal(got.Result.Value,out);err!=nil{return fmt.Errorf("the Claude page returned an unreadable value: %w",err)}
	return nil
}

func claudeChatPageIsSignedIn(d voiceDevTools) bool { var v bool; return claudeChatEval(d,claudeChatSignedInJS,true,&v)==nil&&v }
func waitForClaudeChatPageSignedIn(d voiceDevTools, timeout time.Duration) bool {
	deadline:=time.Now().Add(timeout)
	for time.Now().Before(deadline){if claudeChatPageIsSignedIn(d){return true};time.Sleep(250*time.Millisecond)}
	return false
}

func platformStartClaudeChatLogin(dataDir string) error {
	_ = platformStopClaudeChatWorker(dataDir); waitForClaudeChatStopped(dataDir,4*time.Second)
	mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.LoginActive=true;s.Starting=true;s.Running=false;s.Visible=false;s.SignedIn=false;s.LastEvent="sign-in-window-starting";s.LastError=""})
	exe,err:=os.Executable();if err!=nil{return err}
	cmd:=exec.Command(exe,"--claude-chat-login")
	if err:=cmd.Start();err!=nil{mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.LoginActive=false;s.Starting=false;s.LastError=err.Error()});return err}
	_ = cmd.Process.Release(); return nil
}

func platformEnsureClaudeChatWorker(dataDir string) error {
	s:=loadClaudeChatRuntime(dataDir)
	if s.LoginActive&&(s.Running||s.Starting){return nil}
	if s.Running&&s.ControlPort>0&&s.ControlToken!=""{
		ctx,cancel:=context.WithTimeout(context.Background(),900*time.Millisecond);defer cancel()
		if _,code,err:=claudeChatControlRequest(ctx,s,http.MethodGet,"/health",nil);err==nil&&code==http.StatusOK{return nil}
	}
	if s.Starting&&time.Since(s.UpdatedAt)<10*time.Second{return nil}
	mutateClaudeChatRuntime(dataDir,func(v *ClaudeChatWebRuntime){v.Starting=true;v.LoginActive=false;v.Running=false;v.Visible=false;v.SignedIn=false;v.ControlPort=0;v.ControlToken="";v.LastEvent="background-starting";v.LastError=""})
	exe,err:=os.Executable();if err!=nil{return err}
	cmd:=exec.Command(exe,"--claude-chat-worker");hideWindow(cmd)
	if err:=cmd.Start();err!=nil{mutateClaudeChatRuntime(dataDir,func(v *ClaudeChatWebRuntime){v.Starting=false;v.LastError=err.Error()});return err}
	_ = cmd.Process.Release();return nil
}

func platformStopClaudeChatWorker(dataDir string) error {
	s:=loadClaudeChatRuntime(dataDir)
	for i:=0;i<20&&s.ControlPort<1&&s.Starting;i++{time.Sleep(100*time.Millisecond);s=loadClaudeChatRuntime(dataDir)}
	if s.ControlPort<1||s.ControlToken==""{mutateClaudeChatRuntime(dataDir,func(v *ClaudeChatWebRuntime){v.Running=false;v.Starting=false;v.Visible=false;v.LoginActive=false;v.SignedIn=false;v.ControlPort=0;v.ControlToken=""});return nil}
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
	_,_,err:=claudeChatControlRequest(ctx,s,http.MethodPost,"/stop",strings.NewReader(`{}`))
	if err!=nil{mutateClaudeChatRuntime(dataDir,func(v *ClaudeChatWebRuntime){v.Running=false;v.Starting=false;v.Visible=false;v.LoginActive=false;v.SignedIn=false;v.ControlPort=0;v.ControlToken=""})}
	return err
}

func runClaudeChatWebView(dataDir string, visible bool) error {
	runtime.LockOSThread();defer runtime.UnlockOSThread()
	if err:=os.MkdirAll(claudeChatProfilePath(dataDir),0700);err!=nil{return err}
	opts:=webview2.WebViewOptions{Debug:false,AutoFocus:visible,DataPath:claudeChatProfilePath(dataDir),WindowOptions:webview2.WindowOptions{Title:"Connect Claude Chat to FlipAi",Width:1120,Height:820,Center:visible}}
	if !visible{opts.WindowOptions.Center=false;opts.WindowOptions.Position=true;opts.WindowOptions.X=-30000;opts.WindowOptions.Y=-30000;opts.WindowOptions.ExStyle=wsExToolWin|wsExNoActivate;opts.WindowOptions.NoActivate=true}
	w:=webview2.NewWithOptions(opts);if w==nil{return errors.New("Microsoft Edge WebView2 Runtime could not create the Claude Chat browser")}
	defer w.Destroy();applyFlipAiWindowIcon(uintptr(w.Window()));w.SetSize(800,600,webview2.HintMin)
	initial:=loadClaudeChatRuntime(dataDir);wasSignedIn:=false;hadConnected:=initial.Connected
	_ = w.Bind("flipClaudeChatStatus",func(signedIn bool,href string){
		changed:=signedIn!=wasSignedIn
		mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.Running=true;s.Starting=false;s.Visible=visible;s.LoginActive=visible;s.SignedIn=signedIn;s.LastURL=href;if signedIn{s.Connected=true;s.LastError="";if changed||strings.Contains(s.LastEvent,"starting"){s.LastEvent="session-ready"}}else if changed||strings.Contains(s.LastEvent,"starting"){if s.Connected{s.LastEvent="session-restoring"}else{s.LastEvent="waiting-for-sign-in"}}})
		if signedIn&&!wasSignedIn{if hadConnected{claudeChatActivity(dataDir,"info","claude-chat-session","Saved Claude Chat sign-in was restored.",0)}else{claudeChatActivity(dataDir,"info","claude-chat-session","Claude Chat sign-in was verified and saved in FlipAi's dedicated profile.",0);hadConnected=true}}
		wasSignedIn=signedIn
	})
	w.Init(claudeChatPageMonitorJS);dev:=newWebViewDevTools(w)
	port,closer:=startClaudeChatControlEndpoint(dataDir,w,dev);if closer!=nil{defer closer.Close()}
	mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.Running=true;s.Starting=false;s.Visible=visible;s.LoginActive=visible;s.ControlPort=port;s.SignedIn=false;s.LastEvent="browser-starting";s.LastError=""})
	w.Navigate(claudeChatWebURL);w.Run()
	mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.Running=false;s.Starting=false;s.Visible=false;s.LoginActive=false;s.SignedIn=false;s.ControlPort=0;s.ControlToken="";if s.Connected{s.LastEvent="background-restart-pending"}else{s.LastEvent="browser-closed"}})
	return nil
}

func startClaudeChatControlEndpoint(dataDir string,w webview2.WebView,dev voiceDevTools)(int,io.Closer){
	if dev==nil{return 0,nil};ln,err:=net.Listen("tcp","127.0.0.1:0");if err!=nil{return 0,nil}
	token,err:=secureRandomToken(24);if err!=nil{_ = ln.Close();return 0,nil};port:=ln.Addr().(*net.TCPAddr).Port
	mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.ControlToken=token;s.ControlPort=port})
	authorized:=func(r *http.Request)bool{return token!=""&&r.Header.Get("X-FlipAi-Token")==token}
	mux:=http.NewServeMux()
	mux.HandleFunc("/health",func(rw http.ResponseWriter,r *http.Request){if !authorized(r){http.Error(rw,"FlipAi token required",http.StatusForbidden);return};_ = json.NewEncoder(rw).Encode(map[string]any{"ok":true,"signedIn":claudeChatPageIsSignedIn(dev)})})
	turn:=func(rw http.ResponseWriter,r *http.Request,prompt string,newChat bool){
		if !authorized(r){http.Error(rw,"FlipAi token required",http.StatusForbidden);return}
		if newChat{var ignored bool;if err:=claudeChatEval(dev,`(()=>{location.href='https://claude.ai/new';return true})()`,false,&ignored);err!=nil{rw.WriteHeader(http.StatusBadGateway);_ = json.NewEncoder(rw).Encode(map[string]any{"ok":false,"detail":err.Error()});return}}
		if !waitForClaudeChatPageSignedIn(dev,25*time.Second){rw.WriteHeader(http.StatusUnauthorized);_ = json.NewEncoder(rw).Encode(map[string]any{"ok":false,"detail":"Claude Chat is not signed in inside FlipAi. Press Connect and complete sign-in first."});return}
		expr:=fmt.Sprintf(claudeChatTurnJS,claudeChatJSString(prompt));var got claudeChatTurnResult
		if err:=claudeChatEval(dev,expr,true,&got);err!=nil{rw.WriteHeader(http.StatusInternalServerError);_ = json.NewEncoder(rw).Encode(map[string]any{"ok":false,"detail":"FlipAi could not run the Claude page driver: "+err.Error()});return}
		cid:=claudeChatConversationID(got.Href);mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.Connected=true;s.SignedIn=true;s.LastURL=got.Href;s.ConversationID=cid;if got.OK{s.LastEvent="turn-complete";s.LastError=""}else{s.LastEvent="turn-failed";s.LastError=got.Detail}})
		status:=http.StatusOK;if !got.OK{status=http.StatusBadGateway};rw.WriteHeader(status);_ = json.NewEncoder(rw).Encode(map[string]any{"ok":got.OK,"reply":got.Reply,"detail":got.Detail,"conversationId":cid})
	}
	mux.HandleFunc("/new",func(rw http.ResponseWriter,r *http.Request){if r.Method!=http.MethodPost{http.Error(rw,"POST required",http.StatusMethodNotAllowed);return};if !authorized(r){http.Error(rw,"FlipAi token required",http.StatusForbidden);return};var ignored bool;if err:=claudeChatEval(dev,`(()=>{location.href='https://claude.ai/new';return true})()`,false,&ignored);err!=nil{rw.WriteHeader(http.StatusBadGateway);_ = json.NewEncoder(rw).Encode(map[string]any{"ok":false,"detail":err.Error()});return};if !waitForClaudeChatPageSignedIn(dev,45*time.Second){rw.WriteHeader(http.StatusUnauthorized);_ = json.NewEncoder(rw).Encode(map[string]any{"ok":false,"detail":"Claude did not restore the saved sign-in after opening a new chat"});return};mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.Connected=true;s.SignedIn=true;s.ConversationID="";s.LastEvent="new-chat-ready";s.LastError=""});_ = json.NewEncoder(rw).Encode(map[string]any{"ok":true})})
	mux.HandleFunc("/test",func(rw http.ResponseWriter,r *http.Request){if r.Method!=http.MethodPost{http.Error(rw,"POST required",http.StatusMethodNotAllowed);return};turn(rw,r,"Reply with exactly: FLIPAI_OK",true)})
	mux.HandleFunc("/chat",func(rw http.ResponseWriter,r *http.Request){if r.Method!=http.MethodPost{http.Error(rw,"POST required",http.StatusMethodNotAllowed);return};var body struct{Prompt string `json:"prompt"`;New bool `json:"new"`};if err:=json.NewDecoder(http.MaxBytesReader(rw,r.Body,64<<10)).Decode(&body);err!=nil{http.Error(rw,err.Error(),http.StatusBadRequest);return};body.Prompt=strings.TrimSpace(body.Prompt);if body.Prompt==""{http.Error(rw,"prompt required",http.StatusBadRequest);return};turn(rw,r,body.Prompt,body.New)})
	mux.HandleFunc("/stop",func(rw http.ResponseWriter,r *http.Request){if !authorized(r){http.Error(rw,"FlipAi token required",http.StatusForbidden);return};_ = json.NewEncoder(rw).Encode(map[string]bool{"ok":true});go func(){time.Sleep(80*time.Millisecond);w.Terminate()}()})
	server:=&http.Server{Handler:mux,ReadHeaderTimeout:4*time.Second,WriteTimeout:115*time.Second};go func(){_ = server.Serve(ln)}();return port,server
}

func recordClaudeChatWorkerError(dataDir string,err error){if err==nil{return};mutateClaudeChatRuntime(dataDir,func(s *ClaudeChatWebRuntime){s.Running=false;s.Starting=false;s.Visible=false;s.LoginActive=false;s.SignedIn=false;s.LastEvent="browser-error";s.LastError=err.Error()});claudeChatActivity(dataDir,"error","claude-chat-session","Claude Chat browser stopped with an error: "+err.Error(),0)}
func claudeChatWorkerMain(dataDir string,visible bool){if err:=runClaudeChatWebView(dataDir,visible);err!=nil{recordClaudeChatWorkerError(dataDir,err)}}
