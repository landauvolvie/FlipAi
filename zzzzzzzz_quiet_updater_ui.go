package main

import (
	"strings"
)

func updaterUIState(releaseVersion string) string {
	info := currentUpdateSnapshot()
	if releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
		return ""
	}
	if info.Ready() {
		return "ready"
	}
	if info.Downloading {
		return "downloading"
	}
	return "waiting"
}

func updaterUIPercent(releaseVersion string) int {
	info := currentUpdateSnapshot()
	if releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
		return 0
	}
	return info.ProgressPercent()
}

func init() {
	// Updates live only beside the version in the sidebar. Keep Settings focused
	// on startup/calling and remove the old page-wide updater surfaces.
	settings := cleanSettingsHTML
	settings = strings.Replace(settings,
		`<div><h1>Settings</h1><p>Keep FlipAi running and manage app updates and calling.</p></div>`,
		`<div><h1>Settings</h1><p>Keep FlipAi running and manage calling.</p></div>`, 1)
	if start := strings.Index(settings, `<section class="card settings-compact-card">`); start >= 0 {
		if relEnd := strings.Index(settings[start:], `<section class="card settings-startup-card">`); relEnd >= 0 {
			settings = settings[:start] + settings[start+relEnd:]
		}
	}
	settings = strings.Replace(settings, " Check for updates", "", 1)
	registerPage("settings", settings)

}

// Apply the updater shell whenever a page is registered, including provider
// pages registered later during initialization.
func quietUpdaterShell(shell string) string {
	updatedShell := shell
	oldSidebar := `      {{if .Shell.UpdateVersion}}<a class="side-update" href="/settings#updates" title="FlipAi {{.Shell.UpdateVersion}} is available">{{icon "download"}}<span>v{{.Shell.Version}} &rarr; {{.Shell.UpdateVersion}}</span></a>{{else}}<span>v{{.Shell.Version}}</span>{{end}}`
	newSidebar := `      <div class="side-version-row" id="flipai-version-row">
        <span class="side-version">v{{.Shell.Version}}</span>
        <span class="side-update-control" id="flipai-update-control">
        {{if .Shell.UpdateVersion}}
          {{$updateState := updaterState .Shell.UpdateVersion}}
          {{$updatePercent := updaterPercent .Shell.UpdateVersion}}
          <button class="side-update side-update-icon{{if eq $updateState "ready"}} side-update-ready{{end}}" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" {{if ne $updateState "ready"}}disabled{{end}} title="{{if eq $updateState "ready"}}Install FlipAi {{.Shell.UpdateVersion}} and restart{{else}}Downloading FlipAi {{.Shell.UpdateVersion}} ({{$updatePercent}}%){{end}}" aria-label="{{if eq $updateState "ready"}}Install update and restart{{else}}Downloading update{{end}}">{{icon "download"}}</button>
        {{end}}
        </span>
      </div>`
	updatedShell = strings.Replace(updatedShell, oldSidebar, newSidebar, 1)

	bannerStart := `    {{if .Shell.UpdateVersion}}
    <div class="banner update">`
	bannerEnd := `    {{end}}
    {{template "content" .}}`
	if start := strings.Index(updatedShell, bannerStart); start >= 0 {
		if relEnd := strings.Index(updatedShell[start:], bannerEnd); relEnd >= 0 {
			updatedShell = updatedShell[:start] + `    {{template "content" .}}` + updatedShell[start+relEnd+len(bannerEnd):]
		}
	}

	const updaterStyle = `<style>
.side-version-row{display:flex;align-items:center;justify-content:space-between;gap:8px;min-height:30px;width:100%}.side-version{white-space:nowrap}.side-update-control{display:flex;align-items:center;margin-left:auto}.side-update-icon{display:inline-flex;align-items:center;justify-content:center;width:28px;height:28px;padding:5px;border:0;border-radius:50%;background:transparent;color:var(--muted);cursor:default}.side-update-icon .icon{width:18px;height:18px}.side-update-ready{background:var(--accent);color:#fff;cursor:pointer}.side-update-ready:hover{filter:brightness(.94)}.side-update-icon:focus-visible{outline:2px solid var(--accent);outline-offset:3px}.side-update-icon[aria-busy="true"]{opacity:.6}
</style>`
	updatedShell = strings.Replace(updatedShell, `</head>`, updaterStyle+`</head>`, 1)

	// The host owns update checks/downloads. This script only reads the local
	// status endpoint so the icon can show readiness without page
	// reloads or GitHub requests from the UI.
	const updaterScript = `<script>
(() => {
  const control = () => document.getElementById('flipai-update-control');
  let installing = false;
  const install = async (button) => {
    if (installing || !button || button.disabled) return;
    installing = true;
    button.disabled = true;
    button.setAttribute('aria-busy','true');
    button.title = 'Installing update…';
    button.setAttribute('aria-label', button.title);
    try {
      const response = await fetch('/update/install', {method:'POST', credentials:'same-origin', cache:'no-store'});
      if (!response.ok) throw new Error('install failed');
    } catch (_) {
      // A failed request can also mean Setup has stopped the local server.
      // Only restore the control after the host answers a fresh status check.
      try {
        const response=await fetch('/update/status.json',{credentials:'same-origin',cache:'no-store'});
        if (response.ok) {
          const status=await response.json();
          if (!status.installing) { installing=false; render(status); }
        }
      } catch (_) {}
    }
  };
  const render = (s) => {
    const host = control();
    if (!host || installing) return;
    if (!s || !s.available) { host.replaceChildren(); return; }
    let button=host.querySelector('button');
    if (!button) {
      button=document.createElement('button');
      button.type='button';
      button.id='flipai-update-install';
      button.innerHTML='<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3v12m-5-5 5 5 5-5M5 16v5h14v-5"/></svg>';
      button.addEventListener('click',()=>install(button));
      host.append(button);
    }
    const pct=Math.max(0,Math.min(100,Number(s.percent)||0));
    button.className='side-update side-update-icon'+(s.ready ? ' side-update-ready' : '');
    button.dataset.version=s.targetVersion || '';
    button.disabled=!s.ready || !!s.installing;
    button.setAttribute('aria-busy',String(!!s.installing));
    button.title=s.installing ? 'Installing update…' : s.ready ? 'Install FlipAi '+s.targetVersion+' and restart' : (s.downloading ? 'Downloading FlipAi '+s.targetVersion+' ('+pct+'%)' : 'Update download will retry automatically');
    button.setAttribute('aria-label',button.title);
  };
  const refresh = async () => {
    if (installing) return;
    try {
      const response=await fetch('/update/status.json',{credentials:'same-origin',cache:'no-store'});
      if (response.ok) render(await response.json());
    } catch (_) {}
  };
  const initial=document.getElementById('flipai-update-install');
  if(initial) initial.addEventListener('click',()=>install(initial));
  refresh();
  window.setInterval(refresh,1000);
})();
</script>`
	updatedShell = strings.Replace(updatedShell, `</body>`, updaterScript+`</body>`, 1)

	return updatedShell
}
