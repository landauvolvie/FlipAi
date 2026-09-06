package main

import (
	"html/template"
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

	updatedShell := shellHTML
	oldSidebar := `      {{if .Shell.UpdateVersion}}<a class="side-update" href="/settings#updates" title="FlipAi {{.Shell.UpdateVersion}} is available">{{icon "download"}}<span>v{{.Shell.Version}} &rarr; {{.Shell.UpdateVersion}}</span></a>{{else}}<span>v{{.Shell.Version}}</span>{{end}}`
	newSidebar := `      <div class="side-version-row" id="flipai-version-row">
        <span class="side-version">v{{.Shell.Version}}</span>
        <span class="side-update-control" id="flipai-update-control">
        {{if .Shell.UpdateVersion}}
          {{$updateState := updaterState .Shell.UpdateVersion}}
          {{$updatePercent := updaterPercent .Shell.UpdateVersion}}
          {{if eq $updateState "ready"}}
            <button class="side-update side-update-ready" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" title="Install FlipAi {{.Shell.UpdateVersion}}">Install update</button>
          {{else}}
            <span class="side-update-progress" title="Downloading FlipAi {{.Shell.UpdateVersion}}"><span class="side-update-ring" style="--update-percent:{{$updatePercent}}"></span><span class="side-update-percent">{{$updatePercent}}%</span></span>
          {{end}}
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
.side-version-row{display:flex;align-items:center;justify-content:space-between;gap:8px;min-height:30px;width:100%}.side-version{white-space:nowrap}.side-update-control{display:flex;align-items:center;margin-left:auto}.side-update-progress{display:inline-flex;align-items:center;gap:5px;white-space:nowrap;color:var(--muted);font-size:12px;font-weight:650}.side-update-ring{--update-percent:0;width:20px;height:20px;border-radius:50%;display:inline-block;position:relative;background:conic-gradient(var(--accent) calc(var(--update-percent) * 1%),var(--line) 0)}.side-update-ring:after{content:"";position:absolute;inset:4px;border-radius:50%;background:var(--surface)}.side-update{border:0;font:inherit}.side-update-ready{min-height:28px;padding:5px 9px;border-radius:8px;background:var(--accent);color:#fff;font-size:12px;font-weight:700;cursor:pointer;white-space:nowrap}.side-update-ready:hover{filter:brightness(.96)}.side-update-ready:disabled{cursor:default;opacity:.62}
</style>`
	updatedShell = strings.Replace(updatedShell, `</head>`, updaterStyle+`</head>`, 1)

	// The host owns update checks/downloads. This script only reads the local
	// status endpoint so the small percentage can move smoothly without page
	// reloads or GitHub requests from the UI.
	const updaterScript = `<script>
(() => {
  const control = () => document.getElementById('flipai-update-control');
  const install = async (button) => {
    if (!button || button.disabled) return;
    button.disabled = true;
    button.textContent = 'Installing…';
    try {
      const response = await fetch('/update/install', {method:'POST', credentials:'same-origin', cache:'no-store'});
      if (!response.ok) throw new Error('install failed');
    } catch (_) {
      // If Setup has already stopped FlipAi, the request can end while the
      // local server is disappearing. Leave the installing state in that case.
      setTimeout(() => { if (document.body.contains(button)) { button.disabled=false; button.textContent='Install update'; } }, 4000);
    }
  };
  const render = (s) => {
    const host = control();
    if (!host) return;
    host.replaceChildren();
    if (!s || !s.available) return;
    if (s.ready) {
      const button=document.createElement('button');
      button.type='button';
      button.id='flipai-update-install';
      button.className='side-update side-update-ready';
      button.dataset.version=s.targetVersion || '';
      button.title='Install FlipAi '+(s.targetVersion || 'update');
      button.textContent='Install update';
      button.addEventListener('click',()=>install(button));
      host.append(button);
      return;
    }
    const wrap=document.createElement('span');
    wrap.className='side-update-progress';
    wrap.title='Downloading FlipAi '+(s.targetVersion || 'update');
    const ring=document.createElement('span');
    ring.className='side-update-ring';
    const pct=Math.max(0,Math.min(100,Number(s.percent)||0));
    ring.style.setProperty('--update-percent',String(pct));
    const text=document.createElement('span');
    text.className='side-update-percent';
    text.textContent=pct+'%';
    wrap.append(ring,text);
    host.append(wrap);
  };
  const refresh = async () => {
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

	for _, page := range uiPages {
		page.Funcs(template.FuncMap{"updaterState": updaterUIState, "updaterPercent": updaterUIPercent})
		if _, err := page.Parse(updatedShell); err != nil {
			panic(err)
		}
	}
}
