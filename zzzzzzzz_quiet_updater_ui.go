package main

import (
	"html/template"
	"strings"
)

// updaterUIState is deliberately tiny: the sidebar only needs to know whether
// a newer release is still being staged or is verified and ready to install.
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

func updaterUIProgress(releaseVersion string) int {
	info := currentUpdateSnapshot()
	if releaseVersion == "" || info.Version != releaseVersion || !info.Newer() {
		return 0
	}
	return info.ProgressPercent()
}

func init() {
	// Settings no longer owns updates. Keep only startup/calling controls and
	// remove even the compatibility text that mentioned checking for updates.
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

	// Replace the old Settings link + page-wide update banner with a compact
	// version-row control. Every page shares this shell, so the indicator is
	// always in the same place.
	updatedShell := shellHTML
	oldSidebar := `      {{if .Shell.UpdateVersion}}<a class="side-update" href="/settings#updates" title="FlipAi {{.Shell.UpdateVersion}} is available">{{icon "download"}}<span>v{{.Shell.Version}} &rarr; {{.Shell.UpdateVersion}}</span></a>{{else}}<span>v{{.Shell.Version}}</span>{{end}}`
	newSidebar := `      <div class="side-version-row" id="flipai-version-row">
        <span class="side-version">v{{.Shell.Version}}</span>
        {{if .Shell.UpdateVersion}}
          {{$updateState := updaterState .Shell.UpdateVersion}}
          {{$updateProgress := updaterProgress .Shell.UpdateVersion}}
          {{if eq $updateState "ready"}}
            <button class="side-update side-update-ready" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" title="Install FlipAi {{.Shell.UpdateVersion}} and restart">{{icon "download"}}<span>Install update</span></button>
          {{else}}
            <span class="side-update-progress" title="Downloading FlipAi {{.Shell.UpdateVersion}}">
              <span class="side-update-ring" style="--flipai-update-progress:{{$updateProgress}}"></span><span class="side-update-percent">{{$updateProgress}}%</span>
            </span>
          {{end}}
        {{end}}
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
.side-version-row{display:flex;align-items:center;gap:8px;min-height:30px;flex-wrap:wrap}.side-version{white-space:nowrap}.side-update-progress{display:inline-flex;align-items:center;gap:5px;color:var(--accent);font-size:12px;font-weight:650;white-space:nowrap}.side-update-ring{--flipai-update-progress:0;width:18px;height:18px;border-radius:50%;background:conic-gradient(var(--accent) calc(var(--flipai-update-progress)*1%),var(--line) 0);position:relative;display:inline-block}.side-update-ring:after{content:"";position:absolute;inset:3px;border-radius:50%;background:var(--surface)}.side-update-percent{min-width:29px}.side-update{border:0;border-radius:8px;color:var(--accent);background:transparent}.side-update-ready{min-height:29px;padding:4px 7px;display:inline-flex;align-items:center;gap:5px;cursor:pointer;font-size:11px;font-weight:700;white-space:nowrap}.side-update-ready svg{width:14px;height:14px}.side-update-ready:hover{background:var(--accent-soft)}.side-update-ready:disabled{cursor:default;opacity:.6}
</style>`
	updatedShell = strings.Replace(updatedShell, `</head>`, updaterStyle+`</head>`, 1)

	// Poll only FlipAi's local Home page. The real GitHub check stays on the
	// background host timer; this tiny local refresh merely changes the icon
	// from downloading to install-ready without requiring page navigation.
	const updaterScript = `<script>
(() => {
  const bindInstall = () => {
    const button = document.getElementById('flipai-update-install');
    if (!button || button.dataset.bound === '1') return;
    button.dataset.bound = '1';
    button.addEventListener('click', async () => {
      if (button.disabled) return;
      button.disabled = true;
      const oldHTML = button.innerHTML;
      button.textContent = 'Installing…';
      try {
        const response = await fetch('/update/install', {method:'POST', credentials:'same-origin', cache:'no-store'});
        if (!response.ok) throw new Error('install failed');
      } catch (_) {
        button.disabled = false;
        button.innerHTML = oldHTML;
      }
    });
  };
  const refreshUpdateControl = async () => {
    try {
      const response = await fetch('/', {credentials:'same-origin', cache:'no-store'});
      if (!response.ok) return;
      const html = await response.text();
      const doc = new DOMParser().parseFromString(html, 'text/html');
      const fresh = doc.getElementById('flipai-version-row');
      const current = document.getElementById('flipai-version-row');
      if (fresh && current && fresh.outerHTML !== current.outerHTML) {
        current.replaceWith(fresh);
        bindInstall();
      }
    } catch (_) {}
  };
  bindInstall();
  window.setInterval(refreshUpdateControl, 1000);
})();
</script>`
	updatedShell = strings.Replace(updatedShell, `</body>`, updaterScript+`</body>`, 1)

	for _, page := range uiPages {
		page.Funcs(template.FuncMap{"updaterState": updaterUIState, "updaterProgress": updaterUIProgress})
		if _, err := page.Parse(updatedShell); err != nil {
			panic(err)
		}
	}
}
