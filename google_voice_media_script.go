package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Shared source lets the MMS capture script run in browser regression tests.
const googleVoiceMediaCaptureMarker = "__FLIPAI_GV_MEDIA_CAPTURE__"

func googleVoiceMediaCaptureExpression(marker string) string {
	markerJSON, _ := json.Marshal(strings.TrimSpace(marker))
	mediaTypesJSON, _ := json.Marshal(inboundMediaExtensions)
	extensions := map[string]string{}
	for _, mediaType := range inboundMediaExtensions {
		extensions[mediaType] = attachmentExtension(mediaType)
	}
	extensionsJSON, _ := json.Marshal(extensions)
	maxBytes := maxInboundAttachmentBytes
	return `/*` + googleVoiceMediaCaptureMarker + `*/(async()=>{
  const marker=` + string(markerJSON) + `;
 const mediaTypes=` + string(mediaTypesJSON) + `;
 const extensions=` + string(extensionsJSON) + `;
 const byName=name=>mediaTypes[(String(name||'').match(/\.[a-z0-9]+(?:[?#]|$)/i)||[''])[0].replace(/[?#]$/,'').toLowerCase()]||'';
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
  for(const e of document.querySelectorAll('img[src],video[src],video source[src],audio,audio source[src],a[href]')){
    let owner=e.tagName==='SOURCE'?e.parentElement:e;
    // Voice notes may use a hidden audio element with visible custom controls.
    // Stay inside its nearby card; never play it or open an unrelated link.
    if((owner.tagName==='AUDIO'||owner.tagName==='VIDEO')&&!visible(owner)){
      for(let i=0;i<3&&owner.parentElement&&owner.parentElement!==document.body&&!visible(owner);i++)owner=owner.parentElement;
    }
    if(!visible(owner))continue;
    const u=mediaURL(e);if(!u||!(/^(https?:|blob:|data:)/i.test(u)))continue;
    const low=u.toLowerCase();
    if(/favicon|logo|avatar|profile|emoji|googlelogo|product-logo/.test(low))continue;
    const r=owner.getBoundingClientRect();
    if(e.tagName==='IMG'&&Math.max(r.width,r.height)<72)continue;
    if(r.top>composerTop+20)continue;
    if(e.tagName==='A'&&!e.hasAttribute('download')&&!byName(u)&&!byName(e.textContent)&&!byName(e.getAttribute('aria-label')))continue;
    const y=r.top+r.height/2;
    const distance=markerY>=0?Math.abs(y-markerY):Math.max(0,composerTop-y);
    if(markerY>=0&&distance>700)continue;
    candidates.push({e,u,y,distance});
  }
  candidates.sort((a,b)=>markerY>=0?(a.distance-b.distance):(b.y-a.y));
  const seen=new Set(), chosen=[];
  for(const c of candidates){if(seen.has(c.u))continue;seen.add(c.u);chosen.push(c);if(chosen.length>=6)break;}
  const b64=bytes=>{let s='';const chunk=0x8000;for(let i=0;i<bytes.length;i+=chunk)s+=String.fromCharCode(...bytes.subarray(i,Math.min(bytes.length,i+chunk)));return btoa(s)};
  const extName=(u,i,type)=>{
    try{const p=new URL(u,location.href).pathname;const n=decodeURIComponent(p.split('/').pop()||'');if(n&&n.includes('.'))return n.slice(-160)}catch(_){}
    const ext=extensions[type]||'.bin';
    return 'google-voice-'+(i+1)+ext;
  };
  const out=[];const problems=[];
  for(let i=0;i<chosen.length;i++){
    const c=chosen[i];
    try{
      let type='',bytes=null,name=c.e.tagName==='A'?(c.e.getAttribute('download')||norm(c.e.textContent)||''):'';
      if(/^data:/i.test(c.u)){
        const m=c.u.match(/^data:([^;,]+)?(;base64)?,(.*)$/s);if(!m)throw new Error('invalid data URL');
        type=(m[1]||'application/octet-stream').toLowerCase();
        if(m[2]){const bin=atob(m[3]);bytes=new Uint8Array(bin.length);for(let j=0;j<bin.length;j++)bytes[j]=bin.charCodeAt(j)}
        else bytes=new TextEncoder().encode(decodeURIComponent(m[3]));
      }else{
        const r=await fetch(c.u,{credentials:'include',cache:'force-cache'});if(!r.ok)throw new Error('HTTP '+r.status);
        type=String(r.headers.get('content-type')||'').split(';')[0].toLowerCase();
        const disposition=r.headers.get('content-disposition')||'';
        const filename=disposition.match(/filename=["']?([^"';]+)/i);
        if(filename)name=filename[1].trim();
        const len=Number(r.headers.get('content-length')||0);if(len>MAX)throw new Error('file too large');
        bytes=new Uint8Array(await r.arrayBuffer());
      }
      if(!bytes||!bytes.length||bytes.length>MAX)throw new Error('file is empty or too large');
      if(!type||/^(application|binary)\/(octet-stream|x-download|download)$/.test(type)){
        type=byName(name)||byName(c.u)||String(c.e.getAttribute('type')||'').split(';')[0].toLowerCase();
        if(!type&&c.e.tagName==='IMG')type='image/jpeg';
        // Sniff common voice-note containers instead of mislabelling every
        // generic audio blob as MP3.
        const magic=String.fromCharCode(...bytes.subarray(0,16));
        if(!type&&magic.startsWith('RIFF')&&magic.slice(8,12)==='WAVE')type='audio/wav';
        if(!type&&magic.startsWith('OggS'))type='audio/ogg';
        if(!type&&magic.startsWith('#!AMR'))type='audio/amr';
        if(!type&&magic.startsWith('fLaC'))type='audio/flac';
        if(!type&&(magic.startsWith('ID3')||(bytes[0]===255&&(bytes[1]&224)===224)))type='audio/mpeg';
        if(!type&&magic.slice(4,8)==='ftyp')type=/M4A|M4B/.test(magic)?'audio/mp4':'video/mp4';
      }
      if(type==='application/ogg')type='audio/ogg';
      if(!(/^(image|audio)\//.test(type)||type==='video/mp4'))continue;
      if(!byName(name))name='';
      out.push({filename:name||extName(c.u,i,type),mediaType:type,data:b64(bytes)});
    }catch(e){problems.push(String(e&&e.message||e));}
  }
  if(out.length)return {ok:true,attachments:out};
  return {ok:false,error:chosen.length?('The MMS was visible, but its file bytes could not be read: '+problems.slice(0,2).join('; ')):'No MMS media element was found next to the received message.'};
})()`
}
