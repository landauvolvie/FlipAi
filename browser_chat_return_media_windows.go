//go:build windows

package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const browserChatReturnedMediaMarker = "__FLIPAI_BROWSER_RETURN_MEDIA__"

func isBrowserChatTurnExpression(expression string) bool {
	// Every browser-chat provider uses the same bounded 90-second awaited page
	// turn. Login probes and ordinary page checks do not contain this deadline.
	return strings.Contains(expression, "const deadline=Date.now()+90000;") ||
		strings.Contains(expression, "const deadline = Date.now() + 90000;")
}

type browserChatPageMedia struct {
	Kind            string `json:"kind"`
	Filename        string `json:"filename"`
	MediaType       string `json:"mediaType"`
	Base64          string `json:"base64"`
	ConversationURL string `json:"conversationUrl"`
}

// The scan is deliberately limited to the newest assistant response. That
// avoids mistaking the user's uploaded MMS, an avatar, or an older generated
// image for media returned by this turn.
const browserChatReturnedMediaJS = `/*` + browserChatReturnedMediaMarker + `*/(async()=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const e of out[i].querySelectorAll('*')){if(e.shadowRoot&&!seen.has(e.shadowRoot)){seen.add(e.shadowRoot);out.push(e.shadowRoot)}}}return out};
  const visible=e=>{if(!e)return false;const r=e.getBoundingClientRect?.();if(!r)return true;const s=getComputedStyle(e);return r.width>0&&r.height>0&&s.display!=='none'&&s.visibility!=='hidden'};
  const assistantSelectors=[
    '[data-message-author-role="assistant"]','[data-content="ai-message"]','[data-author="assistant"]','[data-author="bot"]',
    '[data-testid*="assistant"]','[data-testid*="bot"]','[data-testid="assistant-message"]','.font-claude-response-body','.font-claude-message',
    'model-response','[data-test-id="model-response"]','[data-testid="model-response"]','.model-response-text','[class*="model-response"]',
    '[class*="response-message"]','[class*="assistant-message"]'
  ];
  const assistants=()=>{const all=[];const seen=new Set();for(const root of roots()){for(const sel of assistantSelectors){for(const e of root.querySelectorAll(sel)){if(!seen.has(e)&&visible(e)){seen.add(e);all.push(e)}}}}return all};
  const fileLike=/\.(?:pdf|docx?|xlsx?|pptx?|csv|txt|rtf|zip|7z|tar|gz|json|xml|md)(?:$|[?#])/i;
  const mediaIn=box=>{
    const out=[];
    const add=(kind,e,src)=>{if(!e||!src||!visible(e))return;out.push({kind,e,src:String(src)})};
    for(const e of box.querySelectorAll('img')){
      const src=e.currentSrc||e.src||'';const alt=String(e.alt||'').toLowerCase();
      if(!src||/\.svg(?:$|[?#])/i.test(src)||/(avatar|logo|icon|emoji)/.test(alt))continue;
      const w=e.naturalWidth||e.getBoundingClientRect().width,h=e.naturalHeight||e.getBoundingClientRect().height;
      if(w<96||h<96)continue;
      add('image',e,src);
    }
    for(const e of box.querySelectorAll('video')) add('video',e,e.currentSrc||e.src||e.querySelector('source')?.src||'');
    for(const e of box.querySelectorAll('audio')) add('audio',e,e.currentSrc||e.src||e.querySelector('source')?.src||'');
    for(const e of box.querySelectorAll('a[href]')){
      const href=e.href||'';const label=String((e.getAttribute('download')||'')+' '+(e.innerText||'')+' '+(e.getAttribute('aria-label')||'')).toLowerCase();
      if(e.hasAttribute('download')||fileLike.test(href)||/\b(download|file|attachment)\b/.test(label)) add('file',e,href);
    }
    return out;
  };
  let choice=null;
  const until=Date.now()+1600;
  while(Date.now()<until&&!choice){
    const a=assistants();
    for(let i=a.length-1;i>=0&&!choice;i--){const media=mediaIn(a[i]);if(media.length)choice=media[media.length-1]}
    if(!choice)await sleep(160);
  }
  if(!choice)return null;
  const href=String(location.href||'');
  let filename='';
  try{
    if(choice.e.getAttribute('download'))filename=choice.e.getAttribute('download')||'';
    if(!filename){const u=new URL(choice.src,location.href);filename=decodeURIComponent(u.pathname.split('/').pop()||'')}
  }catch(_){}
  if(!filename){filename=choice.kind==='image'?'flipai-generated.png':'flipai-media'}
  let mediaType='',base64='';
  if(choice.kind==='image'){
    try{
      const r=await fetch(choice.src,{credentials:'include'});if(r.ok){const blob=await r.blob();mediaType=blob.type||'';
        if(blob.size>0&&blob.size<=8*1024*1024){const bytes=new Uint8Array(await blob.arrayBuffer());let binary='';for(let i=0;i<bytes.length;i+=32768){binary+=String.fromCharCode(...bytes.subarray(i,i+32768))}base64=btoa(binary)}
      }
    }catch(_){}
  }
  return {kind:choice.kind,filename,mediaType,base64,conversationUrl:href};
})()`

func captureBrowserChatReturnedMediaAfterTurn(d voiceDevTools) {
	if d == nil {
		return
	}
	var page *browserChatPageMedia
	if err := voiceEval(d, browserChatReturnedMediaJS, true, &page); err != nil || page == nil {
		return
	}
	var data []byte
	if strings.TrimSpace(page.Base64) != "" {
		decoded, err := base64.StdEncoding.DecodeString(page.Base64)
		if err == nil {
			data = decoded
		}
	}
	// Round-trip through JSON once so malformed page values cannot sneak in
	// through an unexpected custom type or alias.
	raw, _ := json.Marshal(page)
	var clean browserChatPageMedia
	if json.Unmarshal(raw, &clean) != nil {
		return
	}
	storeCapturedBrowserChatReturnedMedia(&browserChatReturnedMedia{
		Kind:            clean.Kind,
		Filename:        clean.Filename,
		MediaType:       clean.MediaType,
		Data:            data,
		ConversationURL: clean.ConversationURL,
	})
}
