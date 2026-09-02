package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	chatGPTWebURL             = "https://chatgpt.com/"
	chatGPTWebRuntimeFile     = "chatgpt-web-runtime.json"
	chatGPTWebRequestFile     = "chatgpt-web-desktop-request.json"
	chatGPTWebProfileDir      = "chatgpt-web-profile"
	chatGPTWebConversationFile = "chatgpt-web-conversation.json"
	chatGPTWebStartupTimeout  = 18 * time.Second
	chatGPTWebTurnTimeout     = 12 * time.Minute
)

type chatGPTWebRuntime struct {
	Running        bool      `json:"running"`
	Port           int       `json:"port,omitempty"`
	ControlToken   string    `json:"controlToken,omitempty"`
	SignedIn       bool      `json:"signedIn"`
	ComposerReady  bool      `json:"composerReady"`
	CurrentURL     string    `json:"currentUrl,omitempty"`
	ConversationID string    `json:"conversationId,omitempty"`
	LastCapture    string    `json:"lastCapture,omitempty"`
	LastDurationMS int64     `json:"lastDurationMs,omitempty"`
	LastError      string    `json:"lastError,omitempty"`
	LastEvent      string    `json:"lastEvent,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt,omitempty"`
}

type chatGPTWebStatus struct {
	Running        bool   `json:"running"`
	SignedIn       bool   `json:"signedIn"`
	ComposerReady  bool   `json:"composerReady"`
	CurrentURL     string `json:"currentUrl,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	LastCapture    string `json:"lastCapture,omitempty"`
	LastDurationMS int64  `json:"lastDurationMs,omitempty"`
	LastError      string `json:"lastError,omitempty"`
	LastEvent      string `json:"lastEvent,omitempty"`
	Detail         string `json:"detail,omitempty"`
}

type chatGPTWebTurnRequest struct {
	Text           string `json:"text"`
	ConversationID string `json:"conversationId,omitempty"`
	New            bool   `json:"new,omitempty"`
}

type chatGPTWebTurnResult struct {
	Reply          string `json:"reply,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	Capture        string `json:"capture,omitempty"`
	DurationMS     int64  `json:"durationMs,omitempty"`
}

type chatGPTDesktopRequest struct {
	Action string    `json:"action"`
	At     time.Time `json:"at"`
}

var chatGPTWebRuntimeMu sync.Mutex

func chatGPTWebRuntimePath(dataDir string) string { return filepath.Join(dataDir, chatGPTWebRuntimeFile) }
func chatGPTWebRequestPath(dataDir string) string { return filepath.Join(dataDir, chatGPTWebRequestFile) }
func chatGPTWebProfilePath(dataDir string) string { return filepath.Join(dataDir, chatGPTWebProfileDir) }
func chatGPTWebConversationPath(dataDir string) string {
	return filepath.Join(dataDir, chatGPTWebConversationFile)
}

func loadChatGPTWebRuntime(dataDir string) chatGPTWebRuntime {
	chatGPTWebRuntimeMu.Lock()
	defer chatGPTWebRuntimeMu.Unlock()
	var s chatGPTWebRuntime
	if b, err := os.ReadFile(chatGPTWebRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveChatGPTWebRuntime(dataDir string, s chatGPTWebRuntime) error {
	chatGPTWebRuntimeMu.Lock()
	defer chatGPTWebRuntimeMu.Unlock()
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := chatGPTWebRuntimePath(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeChatGPTDesktopRequest(dataDir, action string) error {
	if action != "show" && action != "background" && action != "shutdown" {
		return fmt.Errorf("unknown ChatGPT desktop action %q", action)
	}
	b, err := json.Marshal(chatGPTDesktopRequest{Action: action, At: time.Now()})
	if err != nil {
		return err
	}
	path := chatGPTWebRequestPath(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func takeChatGPTDesktopRequest(dataDir string) string {
	path := chatGPTWebRequestPath(dataDir)
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	var req chatGPTDesktopRequest
	if json.Unmarshal(b, &req) != nil || time.Since(req.At) > 45*time.Second {
		return ""
	}
	return req.Action
}

func chatGPTConversationIDFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "c" {
			id := strings.TrimSpace(parts[i+1])
			if id != "" && !strings.ContainsAny(id, "?#/\\ ") {
				return id
			}
		}
	}
	return ""
}

type chatGPTWebClient struct {
	dataDir   string
	statePath string
}

func newChatGPTWebClient(dataDir, statePath string) *chatGPTWebClient {
	return &chatGPTWebClient{dataDir: dataDir, statePath: statePath}
}

func (c *chatGPTWebClient) activity(level, msg string, d time.Duration) {
	if c == nil || c.statePath == "" {
		return
	}
	log := activityLogForStatePath(c.statePath)
	if d > 0 {
		log.AddTimed(level, "chatgpt", msg, "", "G", "", d)
	} else {
		log.Add(level, "chatgpt", msg, "", "G", "")
	}
}

func (c *chatGPTWebClient) runtimeRequest(ctx context.Context, method, path string, body any, out any) error {
	rt := loadChatGPTWebRuntime(c.dataDir)
	if !rt.Running || rt.Port <= 0 || strings.TrimSpace(rt.ControlToken) == "" {
		return errors.New("the ChatGPT WebView is not running")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", rt.Port, path), rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-FlipAi-Token", rt.ControlToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: chatGPTWebTurnTimeout + time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct{ Error string `json:"error"` }
		_ = json.Unmarshal(raw, &e)
		if strings.TrimSpace(e.Error) == "" {
			e.Error = strings.TrimSpace(string(raw))
		}
		if e.Error == "" {
			e.Error = resp.Status
		}
		return errors.New(e.Error)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

func (c *chatGPTWebClient) rawStatus(ctx context.Context) (chatGPTWebStatus, error) {
	var s chatGPTWebStatus
	if err := c.runtimeRequest(ctx, http.MethodGet, "/status", nil, &s); err != nil {
		return s, err
	}
	return s, nil
}

func (c *chatGPTWebClient) Status(ctx context.Context) chatGPTWebStatus {
	rt := loadChatGPTWebRuntime(c.dataDir)
	if !rt.Running || rt.Port <= 0 || rt.ControlToken == "" {
		return chatGPTWebStatus{LastError: rt.LastError, LastEvent: rt.LastEvent, Detail: "ChatGPT background session is not running."}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	s, err := c.rawStatus(probeCtx)
	if err != nil {
		return chatGPTWebStatus{LastError: "ChatGPT WebView stopped responding: " + truncate(err.Error(), 180), LastEvent: rt.LastEvent, Detail: "The saved browser profile is intact; Connect or the next ChatGPT SMS will restart it."}
	}
	if s.SignedIn {
		s.Detail = "Signed in. FlipAi keeps this WebView parked off-screen between turns."
	} else if s.Running {
		s.Detail = "WebView is running but ChatGPT sign-in is not complete."
	}
	return s
}

func (c *chatGPTWebClient) ensureRunning(ctx context.Context, show bool) error {
	probeCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	if s, err := c.rawStatus(probeCtx); err == nil && s.Running {
		cancel()
		if show {
			showCtx, showCancel := context.WithTimeout(ctx, 3*time.Second)
			defer showCancel()
			return c.runtimeRequest(showCtx, http.MethodPost, "/show", map[string]any{}, nil)
		}
		return nil
	}
	cancel()
	action := "background"
	if show {
		action = "show"
	}
	if err := requestChatGPTWebDesktopAction(c.dataDir, action); err != nil {
		return err
	}
	c.activity("info", "ChatGPT WebView start requested on the signed-in desktop", 0)
	deadline := time.Now().Add(chatGPTWebStartupTimeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		probeCtx, pcancel := context.WithTimeout(ctx, 900*time.Millisecond)
		s, err := c.rawStatus(probeCtx)
		pcancel()
		if err == nil && s.Running {
			return nil
		}
		time.Sleep(180 * time.Millisecond)
	}
	return errors.New("ChatGPT WebView did not start on the interactive Windows desktop. Make sure FlipAi is open in your signed-in Windows session and Microsoft Edge WebView2 Runtime is installed")
}

func (c *chatGPTWebClient) Connect(ctx context.Context) (chatGPTWebStatus, error) {
	started := time.Now()
	if err := c.ensureRunning(ctx, true); err != nil {
		c.activity("error", "ChatGPT Connect failed: "+truncate(err.Error(), 200), time.Since(started))
		return chatGPTWebStatus{}, err
	}
	s := c.Status(ctx)
	if s.SignedIn {
		c.activity("success", "ChatGPT Connect verified the existing signed-in WebView session", time.Since(started))
	} else {
		c.activity("info", "ChatGPT sign-in window opened; waiting for the user to finish sign-in", time.Since(started))
	}
	return s, nil
}

func (c *chatGPTWebClient) waitSignedIn(ctx context.Context, d time.Duration) (chatGPTWebStatus, error) {
	deadline := time.Now().Add(d)
	var last chatGPTWebStatus
	for time.Now().Before(deadline) {
		last = c.Status(ctx)
		if last.Running && last.SignedIn && last.ComposerReady {
			return last, nil
		}
		if ctx.Err() != nil {
			return last, ctx.Err()
		}
		time.Sleep(250 * time.Millisecond)
	}
	return last, errors.New("ChatGPT is not signed in yet. Press Connect ChatGPT and finish sign-in in the FlipAi ChatGPT window")
}

func (c *chatGPTWebClient) Chat(ctx context.Context, text, conversationID string, newConversation bool) (chatGPTWebTurnResult, error) {
	var out chatGPTWebTurnResult
	text = strings.TrimSpace(text)
	if text == "" {
		return out, errors.New("empty ChatGPT message")
	}
	if err := c.ensureRunning(ctx, false); err != nil {
		return out, err
	}
	if _, err := c.waitSignedIn(ctx, 14*time.Second); err != nil {
		return out, err
	}
	started := time.Now()
	c.activity("info", "ChatGPT turn handed to the background WebView", 0)
	turnCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		turnCtx, cancel = context.WithTimeout(ctx, chatGPTWebTurnTimeout)
		defer cancel()
	}
	if err := c.runtimeRequest(turnCtx, http.MethodPost, "/chat", chatGPTWebTurnRequest{Text: text, ConversationID: conversationID, New: newConversation}, &out); err != nil {
		c.activity("error", "ChatGPT turn failed: "+truncate(err.Error(), 220), time.Since(started))
		return out, err
	}
	if strings.TrimSpace(out.Reply) == "" {
		err := errors.New("ChatGPT returned no assistant text")
		c.activity("error", err.Error(), time.Since(started))
		return out, err
	}
	capture := out.Capture
	if capture == "" {
		capture = "unknown"
	}
	c.activity("success", "ChatGPT reply captured via "+capture+" and returned to FlipAi", time.Since(started))
	return out, nil
}

func (c *chatGPTWebClient) Test(ctx context.Context) (chatGPTWebTurnResult, error) {
	return c.Chat(ctx, "Reply with exactly: FlipAi ChatGPT connection is working.", "", true)
}

func (c *chatGPTWebClient) Disconnect(ctx context.Context) error {
	started := time.Now()
	rt := loadChatGPTWebRuntime(c.dataDir)
	if rt.Running && rt.Port > 0 && rt.ControlToken != "" {
		stopCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		_ = c.runtimeRequest(stopCtx, http.MethodPost, "/shutdown", map[string]any{}, nil)
		cancel()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			probeCtx, pcancel := context.WithTimeout(ctx, 350*time.Millisecond)
			_, err := c.rawStatus(probeCtx)
			pcancel()
			if err != nil {
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	_ = writeChatGPTDesktopRequest(c.dataDir, "shutdown")
	// WebView2 helper processes may keep profile handles briefly after the
	// native window closes. Retrying is safer than pretending Disconnect worked.
	profile := chatGPTWebProfilePath(c.dataDir)
	var lastErr error
	for i := 0; i < 20; i++ {
		if err := os.RemoveAll(profile); err == nil {
			lastErr = nil
			break
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = os.Remove(chatGPTWebRuntimePath(c.dataDir))
	_ = os.Remove(chatGPTWebConversationPath(c.dataDir))
	if lastErr != nil {
		c.activity("error", "ChatGPT disconnected but its private WebView profile could not be removed: "+truncate(lastErr.Error(), 180), time.Since(started))
		return lastErr
	}
	c.activity("success", "ChatGPT disconnected and FlipAi's private ChatGPT WebView profile was removed", time.Since(started))
	return nil
}

// chatGPTWebInitScript is injected into FlipAi's private ChatGPT WebView before
// every page load. It never reads cookies, Local Storage, IndexedDB, request
// headers, tokens or passwords. It sends a prompt through the page's own DOM
// and captures only the assistant reply, preferring the completed network/SSE
// response and falling back to the new assistant DOM node.
const chatGPTWebInitScript = `(function(){
  if (globalThis.__flipAiChatGPTInstalled) return;
  globalThis.__flipAiChatGPTInstalled = true;
  let activeTurn = null;
  let lastState = '';
  let domTimer = 0;
  let lastDomText = '';
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const safeCall = (name, value) => { try { const f=globalThis[name]; if(typeof f==='function') Promise.resolve(f(value)).catch(()=>{}); } catch(_){} };
  function composer(){
    return document.querySelector('#prompt-textarea') ||
      document.querySelector('textarea[data-id="root"]') ||
      document.querySelector('textarea') ||
      document.querySelector('[contenteditable="true"][data-lexical-editor="true"]') ||
      document.querySelector('[contenteditable="true"]');
  }
  function assistantNodes(){
    const sels=['[data-message-author-role="assistant"]','article[data-turn="assistant"]','[data-testid^="conversation-turn-"] [data-message-author-role="assistant"]'];
    const out=[]; const seen=new Set();
    for(const sel of sels){ for(const n of document.querySelectorAll(sel)){ if(!seen.has(n)){seen.add(n);out.push(n);} } }
    return out;
  }
  function conversationID(href){
    try { const p=new URL(href).pathname.split('/').filter(Boolean); for(let i=0;i+1<p.length;i++) if(p[i]==='c') return p[i+1]||''; } catch(_){}
    return '';
  }
  function state(){
    const c=composer(); const href=location.href;
    return {running:true,composerReady:!!c,signedIn:!!c && /chatgpt\.com$/i.test(location.hostname),href,conversationId:conversationID(href)};
  }
  function reportState(){
    const s=state(), key=JSON.stringify(s); if(key!==lastState){ lastState=key; safeCall('flipChatGPTState',s); }
  }
  function textFromContent(content){
    if(!content) return '';
    if(typeof content==='string') return content;
    if(Array.isArray(content.parts)) return content.parts.filter(x=>typeof x==='string').join('\n');
    if(typeof content.text==='string') return content.text;
    return '';
  }
  function parseConversationBody(body){
    let best='', cid='';
    const consider=o=>{
      if(!o || typeof o!=='object') return;
      if(typeof o.conversation_id==='string') cid=o.conversation_id;
      if(typeof o.conversationId==='string') cid=o.conversationId;
      const m=o.message;
      if(m && m.author && m.author.role==='assistant') { const t=textFromContent(m.content); if(t.length>=best.length) best=t; }
      if(o.author && o.author.role==='assistant') { const t=textFromContent(o.content); if(t.length>=best.length) best=t; }
      if(o.data && typeof o.data==='object') consider(o.data);
    };
    for(const raw of String(body||'').split(/\r?\n/)){
      const line=raw.trim(); if(!line) continue;
      let payload=line; if(line.startsWith('data:')) payload=line.slice(5).trim();
      if(!payload || payload==='[DONE]') continue;
      try { consider(JSON.parse(payload)); } catch(_){}
    }
    if(!best){ try { consider(JSON.parse(String(body||''))); } catch(_){} }
    return {text:best.trim(),conversationId:cid};
  }
  function finish(capture,text,cid){
    if(!activeTurn || activeTurn.done || !String(text||'').trim()) return;
    activeTurn.done=true;
    safeCall('flipChatGPTReply',{turnId:activeTurn.id,text:String(text).trim(),capture,href:location.href,conversationId:cid||conversationID(location.href)});
  }
  function isConversationURL(raw){
    try { const u=new URL(String(raw),location.href); return /chatgpt\.com$/i.test(u.hostname) && u.pathname.includes('conversation'); } catch(_) { return String(raw||'').includes('/conversation'); }
  }
  const originalFetch=globalThis.fetch;
  if(typeof originalFetch==='function') globalThis.fetch=async function(){
    const res=await originalFetch.apply(this,arguments);
    try {
      const input=arguments[0], raw=typeof input==='string'?input:(input&&input.url)||'';
      if(activeTurn && isConversationURL(raw)){
        const clone=res.clone(); clone.text().then(body=>{ const p=parseConversationBody(body); if(p.text) finish('network',p.text,p.conversationId); }).catch(()=>{});
      }
    } catch(_){}
    return res;
  };
  if(globalThis.XMLHttpRequest){
    const xo=XMLHttpRequest.prototype.open, xs=XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.open=function(method,url){ this.__flipAiURL=String(url||''); return xo.apply(this,arguments); };
    XMLHttpRequest.prototype.send=function(){
      try { if(activeTurn && isConversationURL(this.__flipAiURL)) this.addEventListener('load',()=>{ try { const p=parseConversationBody(this.responseText); if(p.text) finish('network-xhr',p.text,p.conversationId); } catch(_){} },{once:true}); } catch(_){}
      return xs.apply(this,arguments);
    };
  }
  function setComposerText(el,text){
    if(!el) return false;
    if(el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement){
      const proto=el instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
      const setter=Object.getOwnPropertyDescriptor(proto,'value') && Object.getOwnPropertyDescriptor(proto,'value').set;
      if(setter) setter.call(el,text); else el.value=text;
      el.dispatchEvent(new Event('input',{bubbles:true})); el.dispatchEvent(new Event('change',{bubbles:true})); return true;
    }
    if(el.isContentEditable || el.getAttribute('contenteditable')==='true'){
      el.textContent=text;
      try { el.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:text})); } catch(_) { el.dispatchEvent(new Event('input',{bubbles:true})); }
      return true;
    }
    return false;
  }
  function submitComposer(el){
    const form=el && el.closest && el.closest('form');
    if(form && typeof form.requestSubmit==='function'){ form.requestSubmit(); return true; }
    const btn=document.querySelector('[data-testid="send-button"]') || document.querySelector('button[aria-label*="Send" i]') || document.querySelector('button[data-testid*="send" i]');
    if(btn && !btn.disabled){ btn.click(); return true; }
    return false;
  }
  function domCheck(){
    if(!activeTurn || activeTurn.done) return;
    const nodes=assistantNodes(); if(nodes.length<=activeTurn.baseAssistantCount) return;
    const text=String(nodes[nodes.length-1].innerText||nodes[nodes.length-1].textContent||'').trim();
    if(!text) return;
    if(text!==lastDomText){ lastDomText=text; clearTimeout(domTimer); domTimer=setTimeout(domCheck,1100); return; }
    const stop=document.querySelector('[data-testid="stop-button"]') || document.querySelector('button[aria-label*="Stop" i]');
    if(stop){ clearTimeout(domTimer); domTimer=setTimeout(domCheck,700); return; }
    finish('dom',text,'');
  }
  new MutationObserver(()=>{ reportState(); if(activeTurn && !activeTurn.done){ clearTimeout(domTimer); domTimer=setTimeout(domCheck,500); } }).observe(document.documentElement,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['disabled','contenteditable']});
  globalThis.__flipAiChatGPTSubmit=async function(turnId,text){
    const deadline=Date.now()+18000; let el=null;
    while(Date.now()<deadline && !(el=composer())) await sleep(150);
    if(!el){ safeCall('flipChatGPTError',{turnId,code:'composer-not-found',detail:'ChatGPT composer did not appear within 18 seconds'}); return false; }
    activeTurn={id:String(turnId),done:false,baseAssistantCount:assistantNodes().length,startedAt:Date.now()}; lastDomText='';
    if(!setComposerText(el,String(text||''))){ safeCall('flipChatGPTError',{turnId,code:'composer-write-failed',detail:'ChatGPT composer could not be updated'}); return false; }
    await sleep(120);
    if(!submitComposer(el)){ safeCall('flipChatGPTError',{turnId,code:'send-control-not-found',detail:'ChatGPT send control was not available'}); return false; }
    safeCall('flipChatGPTSubmitted',{turnId:String(turnId),href:location.href,conversationId:conversationID(location.href)});
    clearTimeout(domTimer); domTimer=setTimeout(domCheck,900);
    return true;
  };
  reportState(); setInterval(reportState,1500);
})();`