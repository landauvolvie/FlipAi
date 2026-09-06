//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const googleVoiceMediaCaptureMarker = "__FLIPAI_GV_MEDIA_CAPTURE__"

type googleVoiceMediaCaptureRequest struct {
	ID        string    `json:"id"`
	MessageID string    `json:"messageId"`
	Phone     string    `json:"phone"`
	Thread    string    `json:"thread"`
	Marker    string    `json:"marker,omitempty"`
	Created   time.Time `json:"created"`
}

type googleVoiceMediaWireAttachment struct {
	Filename  string `json:"filename,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Data      string `json:"data,omitempty"`
}

type googleVoiceMediaCaptureResult struct {
	OK          bool                             `json:"ok"`
	Error       string                           `json:"error,omitempty"`
	Attachments []googleVoiceMediaWireAttachment `json:"attachments,omitempty"`
}

func googleVoiceMediaQueueDir(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-media-requests")
}

func requestGoogleVoiceInboundMedia(ctx context.Context, dataDir, messageID, phone, thread, marker string) ([]MailAttachment, error) {
	phone = normalizeUSPhone(phone)
	thread = normalizeGoogleVoiceSMSThread(thread)
	if phone == "" || thread == "" {
		return nil, errors.New("Google Voice MMS needs the exact inbound conversation")
	}
	if threadPhone := googleVoiceSMSThreadPhone(thread); threadPhone != "" && threadPhone != phone {
		return nil, errors.New("Google Voice MMS conversation does not match the inbound sender")
	}
	if err := platformEnsureGoogleVoiceSMSWorker(dataDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(googleVoiceMediaQueueDir(dataDir), 0700); err != nil {
		return nil, err
	}
	token, err := secureRandomToken(12)
	if err != nil {
		return nil, err
	}
	req := googleVoiceMediaCaptureRequest{
		ID: token, MessageID: messageID, Phone: phone, Thread: thread,
		Marker: strings.TrimSpace(marker), Created: time.Now(),
	}
	raw, _ := json.Marshal(req)
	requestPath := filepath.Join(googleVoiceMediaQueueDir(dataDir), token+".request.json")
	resultPath := filepath.Join(googleVoiceMediaQueueDir(dataDir), token+".result.json")
	tmp := requestPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, requestPath); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	defer os.Remove(requestPath)
	defer os.Remove(resultPath)

	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(resultPath)
		if err == nil {
			var result googleVoiceMediaCaptureResult
			if json.Unmarshal(raw, &result) != nil {
				return nil, errors.New("Google Voice returned an invalid media result")
			}
			if !result.OK {
				if strings.TrimSpace(result.Error) == "" {
					result.Error = "Google Voice media capture failed"
				}
				return nil, errors.New(result.Error)
			}
			out := make([]MailAttachment, 0, len(result.Attachments))
			for i, item := range result.Attachments {
				mediaType := normalizeInboundMediaType(item.MediaType)
				if !supportedInboundMediaType(mediaType) || item.Data == "" {
					continue
				}
				data, err := base64.StdEncoding.DecodeString(item.Data)
				if err != nil || len(data) == 0 {
					continue
				}
				if len(data) > maxInboundAttachmentBytes {
					return nil, fmt.Errorf("Google Voice attachment exceeds FlipAi's %d MB inbound limit", maxInboundAttachmentBytes>>20)
				}
				out = append(out, MailAttachment{
					Filename:  safeAttachmentFilename(item.Filename, i, mediaType),
					MediaType: mediaType,
					Data:      data,
				})
				if len(out) >= maxInboundAttachmentCount {
					break
				}
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}
	return nil, errors.New("Google Voice media capture timed out")
}

func googleVoiceMediaCaptureExpression(marker string) string {
	markerJSON, _ := json.Marshal(strings.TrimSpace(marker))
	maxBytes := maxInboundAttachmentBytes
	return `/*` + googleVoiceMediaCaptureMarker + `*/(async()=>{
  const marker=` + string(markerJSON) + `;
  const MAX=` + fmt.Sprintf("%d", maxBytes) + `;
  const norm=v=>String(v||'').replace(/\s+/g,' ').trim();
  const visible=e=>{if(!e)return false;const r=e.getBoundingClientRect();const s=getComputedStyle(e);return r.width>0&&r.height>0&&s.display!=='none'&&s.visibility!=='hidden'};
  const composer=Array.from(document.querySelectorAll('textarea,[contenteditable="true"][role="textbox"],[contenteditable="true"]')).filter(visible).sort((a,b)=>b.getBoundingClientRect().top-a.getBoundingClientRect().top)[0]||null;
  const composerTop=composer?composer.getBoundingClientRect().top:innerHeight;
  let markerY=-1;
  if(marker){
    for(const e of document.querySelectorAll('span,div,p')){
      if(!visible(e))continue;
      const t=norm(e.innerText||e.textContent);
      if(!t||t.length>marker.length+80)continue;
      if(t===marker||t.includes(marker)) markerY=Math.max(markerY,e.getBoundingClientRect().top);
    }
  }
  const mediaURL=e=>{
    if(!e)return '';
    if(e.tagName==='AUDIO'||e.tagName==='VIDEO'||e.tagName==='IMG')return e.currentSrc||e.src||'';
    if(e.tagName==='SOURCE')return e.src||'';
    if(e.tagName==='A')return e.href||'';
    return '';
  };
  const candidates=[];
  for(const e of document.querySelectorAll('img[src],video[src],video source[src],audio[src],audio source[src],a[href]')){
    if(!visible(e)&&e.tagName!=='SOURCE')continue;
    const u=mediaURL(e);if(!u||!(/^(https?:|blob:|data:)/i.test(u)))continue;
    const low=u.toLowerCase();
    if(/favicon|logo|avatar|profile|emoji|googlelogo|product-logo/.test(low))continue;
    let owner=e.tagName==='SOURCE'?e.parentElement:e;
    const r=owner.getBoundingClientRect();
    if(e.tagName==='IMG'&&Math.max(r.width,r.height)<72)continue;
    if(r.top>composerTop+20)continue;
    if(e.tagName==='A'&&!e.hasAttribute('download')&&!/\.(?:png|jpe?g|gif|webp|mp3|m4a|wav|ogg|mp4)(?:[?#]|$)/i.test(u))continue;
    const y=r.top+r.height/2;
    const distance=markerY>=0?Math.abs(y-markerY):Math.max(0,composerTop-y);
    if(markerY>=0&&distance>700)continue;
    candidates.push({e:owner,u,y,distance});
  }
  candidates.sort((a,b)=>markerY>=0?(a.distance-b.distance):(b.y-a.y));
  const seen=new Set(), chosen=[];
  for(const c of candidates){if(seen.has(c.u))continue;seen.add(c.u);chosen.push(c);if(chosen.length>=6)break;}
  const b64=bytes=>{let s='';const chunk=0x8000;for(let i=0;i<bytes.length;i+=chunk)s+=String.fromCharCode(...bytes.subarray(i,Math.min(bytes.length,i+chunk)));return btoa(s)};
  const extName=(u,i,type)=>{
    try{const p=new URL(u,location.href).pathname;const n=decodeURIComponent(p.split('/').pop()||'');if(n&&n.includes('.'))return n.slice(-160)}catch(_){}
    const ext=type.includes('jpeg')?'.jpg':type.includes('png')?'.png':type.includes('gif')?'.gif':type.includes('webp')?'.webp':type.includes('mpeg')?'.mp3':type.includes('wav')?'.wav':type.includes('ogg')?'.ogg':type.includes('audio/mp4')?'.m4a':type.includes('video/mp4')?'.mp4':'.bin';
    return 'google-voice-'+(i+1)+ext;
  };
  const out=[];const problems=[];
  for(let i=0;i<chosen.length;i++){
    const c=chosen[i];
    try{
      let type='',bytes=null,name='';
      if(/^data:/i.test(c.u)){
        const m=c.u.match(/^data:([^;,]+)?(;base64)?,(.*)$/s);if(!m)throw new Error('invalid data URL');
        type=(m[1]||'application/octet-stream').toLowerCase();
        if(m[2]){const bin=atob(m[3]);bytes=new Uint8Array(bin.length);for(let j=0;j<bin.length;j++)bytes[j]=bin.charCodeAt(j)}
        else bytes=new TextEncoder().encode(decodeURIComponent(m[3]));
      }else{
        const r=await fetch(c.u,{credentials:'include',cache:'force-cache'});if(!r.ok)throw new Error('HTTP '+r.status);
        type=String(r.headers.get('content-type')||'').split(';')[0].toLowerCase();
        const len=Number(r.headers.get('content-length')||0);if(len>MAX)throw new Error('file too large');
        bytes=new Uint8Array(await r.arrayBuffer());
      }
      if(!bytes||!bytes.length||bytes.length>MAX)throw new Error('file is empty or too large');
      if(!type||type==='application/octet-stream'){
        if(c.e.tagName==='IMG')type='image/jpeg';else if(c.e.tagName==='AUDIO')type='audio/mpeg';else if(c.e.tagName==='VIDEO')type='video/mp4';
      }
      if(!(/^(image|audio)\//.test(type)||type==='video/mp4'))continue;
      if(c.e.tagName==='A')name=c.e.getAttribute('download')||'';
      out.push({filename:name||extName(c.u,i,type),mediaType:type,data:b64(bytes)});
    }catch(e){problems.push(String(e&&e.message||e));}
  }
  if(out.length)return {ok:true,attachments:out};
  return {ok:false,error:chosen.length?('The MMS was visible, but its file bytes could not be read: '+problems.slice(0,2).join('; ')):'No MMS media element was found next to the received message.'};
})()`
}

func captureGoogleVoiceMediaInPage(dataDir string, d voiceDevTools, req googleVoiceMediaCaptureRequest) googleVoiceMediaCaptureResult {
	if d == nil {
		return googleVoiceMediaCaptureResult{Error: "Google Voice media browser is unavailable"}
	}
	target, err := googleVoiceSMSUIThreadPath(req.Phone, req.Thread, true, googleVoiceSMSAPIAccountSlot(d))
	if err != nil {
		return googleVoiceMediaCaptureResult{Error: err.Error()}
	}
	googleVoiceSMSOutboundPending.Add(1)
	defer googleVoiceSMSOutboundPending.Add(-1)
	deadline := time.Now().Add(20 * time.Second)
	if err := googleVoiceSMSUIOpenConversation(d, req.Phone, target, deadline); err != nil {
		return googleVoiceMediaCaptureResult{Error: err.Error()}
	}
	var result googleVoiceMediaCaptureResult
	if err := voiceEval(d, googleVoiceMediaCaptureExpression(req.Marker), true, &result); err != nil {
		return googleVoiceMediaCaptureResult{Error: err.Error()}
	}
	if !result.OK && strings.TrimSpace(result.Error) == "" {
		result.Error = "Google Voice did not expose the MMS attachment"
	}
	if result.OK {
		mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
			s.LastEvent = "media-received"
			s.LastNote = fmt.Sprintf("Captured %d Google Voice media attachment(s)", len(result.Attachments))
		})
	}
	return result
}

func runGoogleVoiceSMSMediaCaptureLoop(dataDir string, d voiceDevTools, stop <-chan struct{}) {
	if googleVoiceSMSCallProcess() || d == nil {
		return
	}
	_ = os.MkdirAll(googleVoiceMediaQueueDir(dataDir), 0700)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		entries, err := os.ReadDir(googleVoiceMediaQueueDir(dataDir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".request.json") {
				continue
			}
			path := filepath.Join(googleVoiceMediaQueueDir(dataDir), entry.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var req googleVoiceMediaCaptureRequest
			if json.Unmarshal(raw, &req) != nil || req.ID == "" {
				_ = os.Remove(path)
				continue
			}
			result := googleVoiceMediaCaptureResult{}
			if time.Since(req.Created) > 2*time.Minute {
				result.Error = "Google Voice media request expired"
			} else {
				result = captureGoogleVoiceMediaInPage(dataDir, d, req)
			}
			resultRaw, _ := json.Marshal(result)
			resultPath := filepath.Join(googleVoiceMediaQueueDir(dataDir), req.ID+".result.json")
			tmp := resultPath + ".tmp"
			_ = os.WriteFile(tmp, resultRaw, 0600)
			_ = os.Rename(tmp, resultPath)
			_ = os.Remove(path)
		}
	}
}
