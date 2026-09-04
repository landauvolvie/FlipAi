//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type googleVoiceSMSOutboundRequest struct {
	ID      string    `json:"id"`
	Phone   string    `json:"phone"`
	Body    string    `json:"body"`
	Created time.Time `json:"created"`
}

type googleVoiceSMSOutboundResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func googleVoiceSMSOutboxDir(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-outbox")
}

func requestGoogleVoiceText(ctx context.Context, dataDir, phone, body string) error {
	phone = normalizeUSPhone(phone)
	body = strings.TrimSpace(body)
	if phone == "" || body == "" {
		return errors.New("Google Voice SMS needs a recipient and text")
	}
	if err := platformEnsureGoogleVoiceSMSWorker(dataDir); err != nil {
		return err
	}
	readyDeadline := time.Now().Add(15 * time.Second)
	for {
		s := loadGoogleVoiceSMSRuntime(dataDir)
		fresh := !s.LastProbeAt.IsZero() && time.Since(s.LastProbeAt) < 8*time.Second
		if s.Running && s.Connected && s.SignedIn && s.Ready && fresh {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(readyDeadline) {
			if s.LastError != "" {
				return errors.New(s.LastError)
			}
			return errors.New("Google Voice SMS browser is not ready; reconnect it under Connections")
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
	req := googleVoiceSMSOutboundRequest{ID: id, Phone: phone, Body: body, Created: time.Now()}
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
	// v0.46.34 also started this loop in the Google Voice calling process. Keep
	// calling code untouched but make that old call-site inert: the dedicated
	// SMS process is now the only process allowed to consume the outbox.
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
				if time.Since(req.Created) > 5*time.Minute {
					result.OK = false
					result.Error = "Google Voice SMS request expired before the browser could send it"
				} else if err := sendGoogleVoiceTextInPage(d, req.Phone, req.Body); err != nil {
					result.OK = false
					result.Error = err.Error()
				} else {
					mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) { s.LastOutboundAt = time.Now() })
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

func sendGoogleVoiceTextInPage(d voiceDevTools, phone, body string) error {
	phone = normalizeUSPhone(phone)
	body = strings.TrimSpace(body)
	if phone == "" || body == "" {
		return errors.New("Google Voice SMS needs a recipient and text")
	}
	phoneJSON, _ := json.Marshal(phone)
	bodyJSON, _ := json.Marshal(body)
	script := strings.ReplaceAll(voiceSendTextJS, "__PHONE__", string(phoneJSON))
	script = strings.ReplaceAll(script, "__BODY__", string(bodyJSON))
	var stage string
	if err := voiceEval(d, script, true, &stage); err != nil {
		return fmt.Errorf("send Google Voice SMS: %w", err)
	}
	if stage != "sent" {
		return fmt.Errorf("send Google Voice SMS: %s", stage)
	}
	return nil
}

const voiceSendTextJS = `(async () => {
  const phone=__PHONE__, body=__BODY__;
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const docs=()=>{const out=[document];for(let i=0;i<out.length;i++){let fs=[];try{fs=out[i].querySelectorAll('iframe,frame')}catch(_){}for(const f of fs){try{const d=f.contentDocument;if(d&&!out.includes(d))out.push(d)}catch(_){}}}return out};
  const all=sel=>{const out=[];for(const d of docs()){try{out.push(...d.querySelectorAll(sel))}catch(_){}}return out};
  const visible=el=>!!el&&!!(el.offsetWidth||el.offsetHeight||el.getClientRects().length);
  const label=el=>(((el.getAttribute&& (el.getAttribute('aria-label')||el.getAttribute('title')||el.getAttribute('placeholder')))||'')+' '+(el.innerText||el.textContent||'')).replace(/\s+/g,' ').trim();
  const buttons=()=>all('button,[role="button"]');
  const clickNamed=re=>{const b=buttons().find(x=>visible(x)&&!x.disabled&&re.test(label(x)));if(!b)return false;b.click();return true};
  const setValue=(el,value)=>{try{const proto=el.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;Object.getOwnPropertyDescriptor(proto,'value').set.call(el,value)}catch(_){el.value=value}el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}))};
  const digits=v=>String(v||'').replace(/\D/g,'').replace(/^1(?=\d{10}$)/,'');
  if(location.hostname.toLowerCase()!=='voice.google.com')return 'not-on-google-voice';
  if(/^\s*sign\s+in\s*$/im.test(String(document.body?.innerText||'').slice(0,1600)))return 'not-signed-in';
  clickNamed(/^(messages|text messages)$/i); await sleep(300);
  if(!clickNamed(/^(send a message|send new message|new message|compose|start a message)$/i))clickNamed(/(send new message|new message|compose|start message)/i);
  await sleep(400);
  let recipient=all('input').find(el=>visible(el)&&/(name|phone|recipient|^to\b)/i.test(label(el)));
  if(!recipient)return 'recipient-input-missing';
  recipient.focus();setValue(recipient,phone);await sleep(650);
  const choices=all('[role="option"],[role="menuitem"],mat-option,gv-contact-list-item').filter(visible);
  const exact=choices.find(x=>digits(label(x)).includes(phone));
  if(exact)exact.click();else if(choices.length===1)choices[0].click();else{recipient.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',code:'Enter',keyCode:13,which:13,bubbles:true}));recipient.dispatchEvent(new KeyboardEvent('keyup',{key:'Enter',code:'Enter',keyCode:13,which:13,bubbles:true}))}
  await sleep(450);
  const composer=all('textarea,input,[contenteditable="true"]').find(el=>visible(el)&&el!==recipient&&/(message|text|sms|type)/i.test(label(el)));
  if(!composer)return 'message-input-missing';
  composer.focus();
  if(composer.isContentEditable){composer.textContent=body;composer.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:body}))}else setValue(composer,body);
  await sleep(150);
  const send=buttons().find(x=>visible(x)&&!x.disabled&&/^(send|send message|send text)$/i.test(label(x))&&!/(image|photo)/i.test(label(x)));
  if(send){send.click();return 'sent'}
  composer.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',code:'Enter',keyCode:13,which:13,bubbles:true}));
  composer.dispatchEvent(new KeyboardEvent('keyup',{key:'Enter',code:'Enter',keyCode:13,which:13,bubbles:true}));
  await sleep(150);return 'sent';
})()`
