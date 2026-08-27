package main

// voiceAudioDesktopScript adds the one-click free virtual-audio installer to
// Settings. It is deliberately separate from the main calling UI: the driver
// operation needs UAC and can fail independently without hiding or breaking
// Google Voice controls.
const voiceAudioDesktopScript = `
(() => {
  if (!globalThis.__flipaiDesktop || location.pathname !== '/settings') return;
  const AUDIO='http://127.0.0.1:8772';
  const show=(message,bad=false)=>{
    document.querySelector('#voice-audio-install-note')?.remove();
    const b=document.createElement('div'); b.id='voice-audio-install-note'; b.className='banner '+(bad?'bad':'ok');
    const s=document.createElement('span'); s.textContent=message; b.append(s);
    document.querySelector('.content')?.prepend(b);
    if(!bad) setTimeout(()=>b.remove(),7000);
  };
  const installUI=()=>{
    if(document.querySelector('#voice-audio-install')) return true;
    const problems=document.querySelector('#vc-problems');
    if(!problems) return false;
    const wrap=document.createElement('div'); wrap.id='voice-audio-install'; wrap.className='callout';
    const strong=document.createElement('b'); strong.textContent='Free built-in audio bridge: ';
    const text=document.createTextNode('FlipAi can install two free signed virtual speaker/microphone pairs automatically. No VB-CABLE purchase and no manual device selection. Windows will ask for administrator approval once.');
    const button=document.createElement('button'); button.type='button'; button.className='btn accent'; button.style.marginLeft='10px'; button.textContent='Install free audio bridge';
    button.addEventListener('click',async()=>{
      const original=button.textContent; button.disabled=true; button.textContent='Installing...';
      show('Downloading, verifying, and installing the free audio bridge. Approve the Windows administrator prompt.');
      try{
        const r=await fetch(AUDIO+'/install',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});
        let body={}; try{body=await r.json()}catch(_){}
        if(!r.ok||!body.ok) throw new Error(body.message||('Audio installer returned '+r.status));
        show(body.message+(body.reboot?' Restart Windows once to finish the driver install.':''));
        setTimeout(()=>location.reload(),3500);
      }catch(e){ show(e.message||String(e),true); }
      finally{button.disabled=false;button.textContent=original;}
    });
    wrap.append(strong,text,button); problems.append(wrap); return true;
  };
  const boot=()=>{
    if(installUI()) return;
    let tries=0; const t=setInterval(()=>{tries++; if(installUI()||tries>40)clearInterval(t)},250);
  };
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',boot,{once:true}); else boot();
})();
`
