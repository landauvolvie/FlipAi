//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const googleVoiceSMSUITurnMarker = "__FLIPAI_GV_UI_SEND__"

type googleVoiceSMSOutboundRequest struct {
	ID          string    `json:"id"`
	Phone       string    `json:"phone"`
	Thread      string    `json:"thread,omitempty"`
	ExactThread bool      `json:"exactThread,omitempty"`
	Body        string    `json:"body"`
	Created     time.Time `json:"created"`
}

type googleVoiceSMSOutboundResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type googleVoiceSMSUIPageState struct {
	Ready  bool   `json:"ready"`
	Phone  string `json:"phone"`
	Href   string `json:"href"`
	Detail string `json:"detail,omitempty"`
}

type googleVoiceSMSUISendResult struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

func googleVoiceSMSOutboxDir(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-outbox")
}

func requestGoogleVoiceText(ctx context.Context, dataDir, phone, body string) error {
	return requestGoogleVoiceTextTarget(ctx, dataDir, phone, "", body, false)
}

func requestGoogleVoiceTextThread(ctx context.Context, dataDir, phone, thread, body string) error {
	thread = normalizeGoogleVoiceSMSThread(thread)
	if thread == "" {
		return errors.New("Google Voice reply blocked: exact conversation thread is required")
	}
	return requestGoogleVoiceTextTarget(ctx, dataDir, phone, thread, body, true)
}

func requestGoogleVoiceTextTarget(ctx context.Context, dataDir, phone, thread, body string, exactThread bool) error {
	phone = normalizeUSPhone(phone)
	body = strings.TrimSpace(body)
	if exactThread {
		thread = normalizeGoogleVoiceSMSThread(thread)
		if thread == "" {
			return errors.New("Google Voice reply blocked: exact conversation thread is required")
		}
	} else {
		thread = ""
	}
	if phone == "" || body == "" {
		return errors.New("Google Voice SMS needs a recipient and text")
	}
	if err := platformEnsureGoogleVoiceSMSWorker(dataDir); err != nil {
		return err
	}
	readyDeadline := time.Now().Add(15 * time.Second)
	for {
		s := loadGoogleVoiceSMSRuntime(dataDir)
		if googleVoiceSMSConnected(s) {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(readyDeadline) {
			if s.LastError != "" {
				return errors.New(s.LastError)
			}
			return errors.New("Google Voice SMS background connection is not ready; reconnect it under Connections")
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err := os.MkdirAll(googleVoiceSMSOutboxDir(dataDir), 0700); err != nil {
		return err
	}
	id, err := secureRandomToken(12)
	if err != nil {
		return err
	}
	req := googleVoiceSMSOutboundRequest{ID: id, Phone: phone, Thread: thread, ExactThread: exactThread, Body: body, Created: time.Now()}
	raw, _ := json.Marshal(req)
	requestPath := filepath.Join(googleVoiceSMSOutboxDir(dataDir), id+".request.json")
	resultPath := filepath.Join(googleVoiceSMSOutboxDir(dataDir), id+".result.json")
	tmp := requestPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, requestPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	defer os.Remove(requestPath)
	defer os.Remove(resultPath)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if raw, err := os.ReadFile(resultPath); err == nil {
			var result googleVoiceSMSOutboundResult
			if json.Unmarshal(raw, &result) != nil {
				return errors.New("Google Voice returned an invalid SMS result")
			}
			if result.OK {
				return nil
			}
			if result.Error == "" {
				result.Error = "Google Voice refused the SMS"
			}
			return errors.New(result.Error)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}
}

func runGoogleVoiceSMSOutboundLoop(dataDir string, d voiceDevTools, stop <-chan struct{}) {
	if googleVoiceSMSCallProcess() || d == nil {
		return
	}
	_ = os.MkdirAll(googleVoiceSMSOutboxDir(dataDir), 0700)
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			entries, err := os.ReadDir(googleVoiceSMSOutboxDir(dataDir))
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".request.json") {
					continue
				}
				path := filepath.Join(googleVoiceSMSOutboxDir(dataDir), entry.Name())
				raw, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				var req googleVoiceSMSOutboundRequest
				if json.Unmarshal(raw, &req) != nil || req.ID == "" {
					_ = os.Remove(path)
					continue
				}
				result := googleVoiceSMSOutboundResult{OK: true}
				switch {
				case time.Since(req.Created) > 5*time.Minute:
					result.OK = false
					result.Error = "Google Voice SMS request expired before the background connection could send it"
				case req.ExactThread && normalizeGoogleVoiceSMSThread(req.Thread) == "":
					result.OK = false
					result.Error = "Google Voice reply blocked: exact conversation thread is invalid"
				default:
					// The loop guard has to exist before Google does. Both ways
					// FlipAi learns about a text -- its own inbox poll, and the
					// updates the signed-in page receives by itself -- can see
					// this one the instant Google accepts it, which is already
					// too late for bookkeeping that waits for the send to
					// return. Recording it first closes that window.
					//
					// A fingerprint left behind by a failed send only suppresses
					// an identical inbound text for half an hour. Removing it
					// would reopen the window for a text Google may in fact have
					// delivered, and an answered reply is a paid SMS loop, so
					// the harmless failure is the one to prefer.
					rememberGoogleVoiceSMSSent(dataDir, req.Phone, req.Body)
					if err := sendGoogleVoiceTextInPage(dataDir, d, req.Phone, req.Thread, req.Body, req.ExactThread, req.Created.Add(googleVoiceSMSOutboundBudget)); err != nil {
						result.OK = false
						result.Error = err.Error()
					} else {
						mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) { s.LastOutboundAt = time.Now() })
					}
				}
				resultRaw, _ := json.Marshal(result)
				resultPath := filepath.Join(googleVoiceSMSOutboxDir(dataDir), req.ID+".result.json")
				tmp := resultPath + ".tmp"
				_ = os.WriteFile(tmp, resultRaw, 0600)
				_ = os.Rename(tmp, resultPath)
				_ = os.Remove(path)
			}
		}
	}
}

func googleVoiceSMSUIThreadPath(phone, thread string, exactThread bool, accountSlot string) (string, error) {
	phone = normalizeUSPhone(phone)
	if phone == "" {
		return "", errors.New("Google Voice SMS needs a valid recipient")
	}
	if exactThread {
		thread = normalizeGoogleVoiceSMSThread(thread)
		if thread == "" {
			return "", errors.New("Google Voice reply blocked: exact conversation thread is required")
		}
		threadPhone := googleVoiceSMSThreadPhone(thread)
		if threadPhone == "" {
			return "", errors.New("Google Voice reply blocked: exact conversation phone is unavailable")
		}
		if threadPhone != phone {
			return "", errors.New("Google Voice reply blocked: conversation phone does not match recipient")
		}
		return thread, nil
	}
	accountSlot = strings.TrimSpace(accountSlot)
	if !asciiDigitsOnly(accountSlot) {
		accountSlot = "0"
	}
	itemID := "t.+1" + phone
	return "/u/" + accountSlot + "/messages?itemId=" + url.QueryEscape(itemID), nil
}

func googleVoiceSMSUIComposerJS() string {
	return `(()=>{
  const visible=e=>{if(!e)return false;const r=e.getBoundingClientRect();const s=getComputedStyle(e);return r.width>0&&r.height>0&&s.display!=='none'&&s.visibility!=='hidden'};
  const candidates=Array.from(document.querySelectorAll('textarea,[contenteditable="true"][role="textbox"],[contenteditable="true"]')).filter(e=>visible(e)&&!e.disabled&&!e.readOnly);
  let best=null,bestScore=-1;
  for(const e of candidates){
    const label=((e.getAttribute('aria-label')||'')+' '+(e.getAttribute('placeholder')||'')+' '+(e.getAttribute('data-placeholder')||'')).toLowerCase();
    let score=0;
    if(label.includes('type a message'))score+=200;
    if(label.includes('send a message'))score+=180;
    if(label.includes('message'))score+=100;
    if(e.tagName==='TEXTAREA')score+=30;
    if(e.getAttribute('role')==='textbox')score+=10;
    score+=Math.max(0,Math.min(50,e.getBoundingClientRect().top/20));
    if(score>bestScore){best=e;bestScore=score;}
  }
  return best;
})()`
}

func googleVoiceSMSUIStateExpression(phone string) string {
	encodedPhone, _ := json.Marshal(phone)
	composer := googleVoiceSMSUIComposerJS()
	return `(()=>{
  const expected=` + string(encodedPhone) + `;
  let item='';
  try{item=new URL(location.href).searchParams.get('itemId')||''}catch(_){}
  const phone=/^t\.\+1(\d{10})$/.test(item)?item.slice(4):'';
  const composer=` + composer + `;
  return {ready:phone===expected&&!!composer,phone,href:String(location.href||''),detail:composer?'':'message composer not found'};
})()`
}

func googleVoiceSMSUIOpenConversation(d voiceDevTools, phone, target string, deadline time.Time) error {
	if d == nil {
		return errNoVoiceControlChannel
	}
	limit := time.Now().Add(15 * time.Second)
	if !deadline.IsZero() && deadline.Before(limit) {
		limit = deadline
	}
	var state googleVoiceSMSUIPageState
	if err := voiceEval(d, googleVoiceSMSUIStateExpression(phone), false, &state); err != nil || state.Phone != phone {
		encodedTarget, _ := json.Marshal(target)
		var started bool
		_ = voiceEval(d, `(()=>{location.assign(`+string(encodedTarget)+`);return true})()`, false, &started)
	}
	for time.Now().Before(limit) {
		state = googleVoiceSMSUIPageState{}
		if err := voiceEval(d, googleVoiceSMSUIStateExpression(phone), false, &state); err == nil && state.Ready {
			return nil
		}
		time.Sleep(180 * time.Millisecond)
	}
	if state.Phone == phone {
		return errors.New("Google Voice opened the correct conversation but FlipAi could not find its message box")
	}
	return errors.New("Google Voice did not open the requested conversation in time")
}

func googleVoiceSMSUISendExpression(body string) string {
	encodedBody, _ := json.Marshal(body)
	composer := googleVoiceSMSUIComposerJS()
	return `/*` + googleVoiceSMSUITurnMarker + `*/(async()=>{
  const input=` + string(encodedBody) + `;
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const norm=v=>String(v||'').replace(/\s+/g,' ').trim();
  const visible=e=>{if(!e)return false;const r=e.getBoundingClientRect();const s=getComputedStyle(e);return r.width>0&&r.height>0&&s.display!=='none'&&s.visibility!=='hidden'};
  const composer=()=>` + composer + `;
  const value=e=>e?(e.tagName==='TEXTAREA'||e.tagName==='INPUT'?e.value:(e.innerText||e.textContent||'')):'';
  const exactTextCount=()=>{
    const wanted=norm(input);let n=0;
    for(const e of document.querySelectorAll('div,span,p')){
      if(!visible(e)||e.closest('textarea,[contenteditable="true"]'))continue;
      if(norm(e.textContent)===wanted)n++;
    }
    return n;
  };
  const failureText=()=>{
    const parts=[];
    for(const e of document.querySelectorAll('[role="alert"],[aria-live="assertive"],.mat-mdc-snack-bar-label')){
      if(visible(e)){const t=norm(e.innerText||e.textContent);if(t)parts.push(t)}
    }
    return parts.join(' | ');
  };
  let c=composer();
  if(!c)return {ok:false,detail:'Google Voice is loaded but FlipAi could not find the message box.'};
  const beforeCount=exactTextCount();
  const beforeFailure=failureText();
  c.focus();
  try{
    if(c.tagName==='TEXTAREA'||c.tagName==='INPUT'){
      const proto=c.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
      const setter=Object.getOwnPropertyDescriptor(proto,'value').set;
      setter.call(c,input);
      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));
      c.dispatchEvent(new Event('change',{bubbles:true}));
    }else{
      const sel=getSelection(),range=document.createRange();
      range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);
      document.execCommand('delete',false,null);
      document.execCommand('insertText',false,input);
      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));
      c.dispatchEvent(new Event('change',{bubbles:true}));
    }
  }catch(_){
    if(c.tagName==='TEXTAREA'||c.tagName==='INPUT')c.value=input;else c.textContent=input;
    c.dispatchEvent(new Event('input',{bubbles:true}));
  }
  await sleep(150);
  c=composer()||c;
  if(norm(value(c))!==norm(input))return {ok:false,detail:'FlipAi could not place the reply into the Google Voice message box.'};
  const button=()=>{
    const scope=c.closest('form')||c.parentElement?.parentElement||document;
    const all=Array.from(scope.querySelectorAll('button')).concat(scope===document?[]:Array.from(document.querySelectorAll('button')));
    let best=null,bestScore=-1;
    for(const b of all){
      if(!visible(b)||b.disabled)continue;
      const label=norm((b.getAttribute('aria-label')||'')+' '+(b.getAttribute('title')||'')+' '+(b.innerText||'')).toLowerCase();
      let score=0;
      if(label==='send message'||label==='send a message')score+=300;
      else if(label==='send')score+=250;
      else if(/\bsend\b/.test(label))score+=150;
      if((b.textContent||'').trim().toLowerCase()==='send')score+=80;
      const r=b.getBoundingClientRect(),cr=c.getBoundingClientRect();
      const distance=Math.abs(r.top-cr.top)+Math.abs(r.left-cr.right);
      score+=Math.max(0,80-Math.min(80,distance/5));
      if(score>bestScore){best=b;bestScore=score;}
    }
    return bestScore>=120?best:null;
  };
  let b=null;
  for(let i=0;i<30&&!b;i++){b=button();if(!b)await sleep(50)}
  if(!b)return {ok:false,detail:'FlipAi filled the Google Voice message box but could not find its Send button.'};
  b.click();
  let clearedSince=0;
  const end=Date.now()+10000;
  while(Date.now()<end){
    await sleep(180);
    const failure=failureText();
    if(failure&&failure!==beforeFailure&&/(failed|couldn.t|could not|not sent|try again|error|too many|slow down|unable)/i.test(failure)){
      return {ok:false,detail:'Google Voice reported: '+failure.slice(0,300)};
    }
    if(exactTextCount()>beforeCount)return {ok:true};
    c=composer()||c;
    const cleared=!norm(value(c));
    if(cleared){
      if(!clearedSince)clearedSince=Date.now();
      if(Date.now()-clearedSince>=900)return {ok:true};
    }else{
      clearedSince=0;
    }
  }
  return {ok:false,detail:'Google Voice did not confirm the message through its page controls.'};
})()`
}

// Outbound SMS now follows Google Voice's own visible-page workflow inside the
// already-running hidden WebView: open the exact conversation, fill the real
// message composer, and click the page's Send control. FlipAi no longer builds
// or replays the sendsms web-service request for replies. The existing API path
// remains available to the inbox listener only.
func sendGoogleVoiceTextInPage(dataDir string, d voiceDevTools, phone, thread, body string, exactThread bool, deadline time.Time) error {
	phone = normalizeUSPhone(phone)
	body = strings.TrimSpace(body)
	if phone == "" || body == "" {
		return errors.New("Google Voice SMS needs a recipient and text")
	}
	target, err := googleVoiceSMSUIThreadPath(phone, thread, exactThread, googleVoiceSMSAPIAccountSlot(d))
	if err != nil {
		return err
	}

	// The active inbox poll is a separate service request. Keep it out of the
	// way while the page navigates and sends so Google Voice gets one normal UI
	// action at a time; passive capture of inbound updates keeps running.
	googleVoiceSMSOutboundPending.Add(1)
	defer googleVoiceSMSOutboundPending.Add(-1)

	if err := googleVoiceSMSUIOpenConversation(d, phone, target, deadline); err != nil {
		return err
	}
	var result googleVoiceSMSUISendResult
	if err := voiceEval(d, googleVoiceSMSUISendExpression(body), true, &result); err != nil {
		return err
	}
	if !result.OK {
		if strings.TrimSpace(result.Detail) == "" {
			result.Detail = "Google Voice did not accept the SMS through its page controls"
		}
		return errors.New(result.Detail)
	}
	return nil
}
