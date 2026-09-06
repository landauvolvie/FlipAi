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
	if info.Error != "" {
		return "error"
	}
	return "waiting"
}

func updaterUIPercent(releaseVersion string) int {
	info := currentUpdateSnapshot()
	if releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
		return 0
	}
	if info.Ready() {
		return 100
	}
	if info.DownloadPercent < 0 {
		return 0
	}
	if info.DownloadPercent > 99 {
		return 99
	}
	return info.DownloadPercent
}

func init() {
	// Settings no longer owns updates. Keep only startup/calling controls. The
	// old handlers remain for compatibility with an already-open older window.
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

	// Only replace the version row at the bottom of the existing sidebar. No
	// navigation, page layout, branding or unrelated app UI is changed.
	updatedShell := shellHTML
	oldSidebar := `      {{if .Shell.UpdateVersion}}<a class="side-update" href="/settings#updates" title="FlipAi {{.Shell.UpdateVersion}} is available">{{icon "download"}}<span>v{{.Shell.Version}} &rarr; {{.Shell.UpdateVersion}}</span></a>{{else}}<span>v{{.Shell.Version}}</span>{{end}}`
	newSidebar := `      <div class="side-version-row" id="flipai-version-row" data-current-version="{{.Shell.Version}}">
        <span class="side-version">v{{.Shell.Version}}</span>
        {{if .Shell.UpdateVersion}}
          {{$updateState := updaterState .Shell.UpdateVersion}}
          {{$updatePercent := updaterPercent .Shell.UpdateVersion}}
          {{if eq $updateState "ready"}}
            <button class="side-update-control side-update-ready" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" title="Install FlipAi {{.Shell.UpdateVersion}}">Install update</button>
          {{else}}
            <span class="side-update-control side-update-progress" title="Downloading FlipAi {{.Shell.UpdateVersion}}">
              <span class="side-update-ring" style="--flipai-progress:{{$updatePercent}}%" aria-hidden="true"></span>
              <span class="side-update-percent">{{$updatePercent}}%</span>
            </span>
          {{end}}
        {{end}}
      </div>`
	updatedShell = strings.Replace(updatedShell, oldSidebar, newSidebar, 1)

	// The retired page-wide banner is intentionally gone; update status belongs
	// only beside the version.
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
.side-version-row{display:flex;align-items:center;gap:8px;min-height:30px;max-width:100%}.side-version{white-space:nowrap;flex:0 0 auto}.side-update-control{flex:0 0 auto}.side-update-progress{display:inline-flex;align-items:center;gap:5px;height:26px;color:var(--muted);font-size:12px;font-weight:700;white-space:nowrap}.side-update-ring{--flipai-progress:0%;width:20px;height:20px;border-radius:50%;display:inline-block;position:relative;background:conic-gradient(var(--accent) var(--flipai-progress),var(--line) 0)}.side-update-ring:after{content:"";position:absolute;inset:4px;border-radius:50%;background:var(--sidebar-bg,var(--surface,#fff))}.side-update-percent{min-width:29px;text-align:right;font-variant-numeric:tabular-nums}.side-update-ready{min-height:28px;border:0;border-radius:8px;padding:5px 9px;color:#fff;background:var(--accent);font:700 11px/1.1 inherit;cursor:pointer;white-space:nowrap;box-shadow:none}.side-update-ready:before{content:"↓";font-size:13px;margin-right:5px}.side-update-ready:hover{filter:brightness(.97)}.side-update-ready:disabled{cursor:default;opacity:.68}.side-update-ready.is-installing:before{content:"";display:inline-block;width:10px;height:10px;border:2px solid rgba(255,255,255,.45);border-top-color:#fff;border-radius:50%;animation:flipaiUpdateSpin .8s linear infinite;vertical-align:-1px}@keyframes flipaiUpdateSpin{to{transform:rotate(360deg)}}
</style>`
	updatedShell = strings.Replace(updatedShell, `</head>`, updaterStyle+`</head>`, 1)

	const updaterScript = `<script>
(() => {
  const row = () => document.getElementById('flipai-version-row');
  const clamp = (v) => Math.max(0, Math.min(100, Number.isFinite(Number(v)) ? Math.round(Number(v)) : 0));

  function progressControl(r, status) {
    let control = r.querySelector('.side-update-control');
    if (!control || !control.classList.contains('side-update-progress')) {
      if (control) control.remove();
      control = document.createElement('span');
      control.className = 'side-update-control side-update-progress';
      const ring = document.createElement('span');
      ring.className = 'side-update-ring';
      ring.setAttribute('aria-hidden', 'true');
      const pct = document.createElement('span');
      pct.className = 'side-update-percent';
      control.append(ring, pct);
      r.append(control);
    }
    const p = Math.min(99, clamp(status.percent));
    control.title = status.state === 'error'
      ? 'Update download will retry automatically'
      : 'Downloading FlipAi ' + (status.updateVersion || 'update');
    control.querySelector('.side-update-ring').style.setProperty('--flipai-progress', p + '%');
    control.querySelector('.side-update-percent').textContent = p + '%';
  }

  function readyControl(r, status) {
    let button = r.querySelector('#flipai-update-install');
    const existing = r.querySelector('.side-update-control');
    if (!button) {
      if (existing) existing.remove();
      button = document.createElement('button');
      button.type = 'button';
      button.id = 'flipai-update-install';
      button.className = 'side-update-control side-update-ready';
      button.textContent = 'Install update';
      r.append(button);
    }
    button.dataset.version = status.updateVersion || '';
    button.title = 'Install FlipAi ' + (status.updateVersion || 'update');
    bindInstall(button);
  }

  function bindInstall(button) {
    if (!button || button.dataset.bound === '1') return;
    button.dataset.bound = '1';
    button.addEventListener('click', async () => {
      if (button.disabled) return;
      button.disabled = true;
      button.classList.add('is-installing');
      button.textContent = 'Installing…';
      try {
        const response = await fetch('/update/install', {
          method:'POST', credentials:'same-origin', cache:'no-store',
          headers:{'X-FlipAi-Inline':'1'}
        });
        if (!response.ok) throw new Error('install failed');
      } catch (_) {
        button.disabled = false;
        button.classList.remove('is-installing');
        button.textContent = 'Install update';
      }
    });
  }

  function render(status) {
    const r = row();
    if (!r) return;
    if (!status || !status.newer) {
      const control = r.querySelector('.side-update-control');
      if (control) control.remove();
      return;
    }
    if (status.ready || status.state === 'ready') readyControl(r, status);
    else progressControl(r, status);
  }

  async function refresh() {
    try {
      const response = await fetch('/update/status.json', {credentials:'same-origin', cache:'no-store'});
      if (!response.ok) return;
      render(await response.json());
    } catch (_) {}
  }

  bindInstall(document.getElementById('flipai-update-install'));
  refresh();
  window.setInterval(refresh, 1000);
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
