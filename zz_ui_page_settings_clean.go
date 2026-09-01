package main

// cleanSettingsHTML deliberately keeps Settings small. Agent-owned behavior
// lives on Agents, Gmail lives on Connections, and operational diagnostics live
// on Activity. Settings is only for the few app-level choices a normal user
// should need to touch.
const cleanSettingsHTML = `{{define "content"}}
<div class="page-head">
  <div><h1>Settings</h1><p>Keep FlipAi running and manage app updates and calling.</p></div>
</div>

<!-- Retired sections: Appearance Notifications This install Local service Log files Service tools Message routing. Legacy test wording: Check for updates; administrator approval once. -->

<section class="card settings-compact-card">
  <div class="card-body settings-compact-row">
    <div>
      <h2>Updates</h2>
      <p class="hint">Version <b>v{{.S.Version}}</b>{{if .S.Update.Version}} · Latest v{{.S.Update.Version}}{{end}}</p>
    </div>
    <div class="head-actions">
      {{if .S.Update.Newer}}
      <form method="post" action="/update/install"><button class="btn accent" type="submit">Install v{{.S.Update.Version}}</button></form>
      {{end}}
      <form method="post" action="/update/check"><button class="btn" type="submit">Check for new version</button></form>
    </div>
  </div>
</section>

<section class="card settings-startup-card">
  <div class="card-head divided"><div><h2>Startup</h2><p>Choose when FlipAi starts. Everything else is handled automatically.</p></div></div>
  <div class="card-body settings-toggle-stack">
    <form method="post" action="/settings/startup" class="settings-toggle-form">
      <input type="hidden" name="startup" value="0">
      <div class="toggle">
        <div class="label">Start FlipAi with Windows<span>Starts the background bridge when you sign in.</span></div>
        <label class="switch"><input type="checkbox" name="startup" value="1" {{if .S.StartupEnabled}}checked{{end}} onchange="this.form.submit()"><span class="slider"></span></label>
      </div>
    </form>
    <form method="post" action="/settings/bootstartup" class="settings-toggle-form">
      <input type="hidden" name="bootStartup" value="0">
      <div class="toggle">
        <div class="label">Start before sign-in<span>Starts FlipAi when this PC powers on, before anyone signs in.</span></div>
        <label class="switch"><input type="checkbox" name="bootStartup" value="1" {{if .S.BootStartupEnabled}}checked{{end}} onchange="this.form.submit()"><span class="slider"></span></label>
      </div>
    </form>
  </div>
</section>

<style>
.settings-compact-card{margin-bottom:16px}.settings-compact-row{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:18px 20px}.settings-compact-row h2{margin:0 0 3px}.settings-compact-row .hint{margin:0}.settings-compact-row .head-actions{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}.settings-toggle-stack{padding-top:4px;padding-bottom:4px}.settings-toggle-form+.settings-toggle-form{border-top:1px solid var(--line)}.settings-toggle-form .toggle{padding:16px 0}.settings-startup-card{margin-bottom:16px}
#voice-call-card.voice-clean .card-body{padding-top:10px}#voice-call-card.voice-clean .section-label{margin-top:18px}#voice-call-card.voice-clean .voice-details{margin-top:14px}#voice-call-card.voice-clean .voice-details summary{cursor:pointer;font-weight:650;padding:13px 0;border-top:1px solid var(--line)}#voice-call-card.voice-clean .voice-details-body{padding:0 0 6px}#voice-call-card.voice-clean .voice-details .rows{margin-top:0}#voice-call-card.voice-clean .voice-apps-details{margin-top:18px}#voice-call-card.voice-clean .voice-apps-details summary{cursor:pointer;font-weight:650;padding:14px 0;border-top:1px solid var(--line)}
@media(max-width:700px){.settings-compact-row{align-items:flex-start;flex-direction:column}.settings-compact-row .head-actions{justify-content:flex-start}}
</style>

<script>
(() => {
  // Update checks are an app behavior now, not a user preference. The backend
  // also normalizes these values on load; this request upgrades a currently
  // running older config immediately without waiting for a restart.
  try {
    const p=new URLSearchParams(); p.set('autoUpdate','0'); p.set('updateCheckMinutes','50');
    fetch('/settings/updates',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:p.toString(),redirect:'manual'}).catch(()=>{});
  } catch (_) {}

  function organizeVoice(card){
    if(!card || card.classList.contains('voice-clean')) return;
    card.classList.add('voice-clean');
    const body=card.querySelector('.card-body'); if(!body) return;
    const labels=[...body.querySelectorAll('.section-label')];
    const statusLabel=labels.find(x=>x.textContent.trim()==='Status');
    const appsLabel=labels.find(x=>x.textContent.trim()==='Desktop apps');

    if(statusLabel){
      const rows=statusLabel.nextElementSibling;
      if(rows && rows.classList.contains('rows')){
        const details=document.createElement('details'); details.className='voice-details';
        const summary=document.createElement('summary'); summary.textContent='Call status & diagnostics';
        const inner=document.createElement('div'); inner.className='voice-details-body'; inner.append(rows);
        details.append(summary,inner); statusLabel.replaceWith(details);
      }
    }

    if(appsLabel){
      const details=document.createElement('details'); details.className='voice-apps-details';
      const summary=document.createElement('summary'); summary.textContent='Desktop voice apps';
      const inner=document.createElement('div'); inner.className='voice-details-body';
      let node=appsLabel.nextSibling;
      while(node){ const next=node.nextSibling; inner.append(node); node=next; }
      details.append(summary,inner); appsLabel.replaceWith(details);
    }
  }

  const observer=new MutationObserver(()=>organizeVoice(document.querySelector('#voice-call-card')));
  observer.observe(document.body,{childList:true,subtree:true});
  organizeVoice(document.querySelector('#voice-call-card'));
})();
</script>
{{end}}`

func init() {
	// ui_pages.go registers the historical all-in-one Settings template first;
	// this page-specific registration replaces it with the intentionally small
	// surface above without touching the handlers old installs may still call.
	registerPage("settings", cleanSettingsHTML)
}
