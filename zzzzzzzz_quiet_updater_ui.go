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
          {{if eq $updateState "ready"}}
            <button class="side-update side-update-ready" id="flipai-update-install" type="button" data-version="{{.Shell.UpdateVersion}}" title="Install FlipAi {{.Shell.UpdateVersion}} and restart">{{icon "download"}}</button>
          {{else}}
            <span class="side-update side-update-downloading" title="Downloading FlipAi {{.Shell.UpdateVersion}}">{{icon "download"}}</span>
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
.side-version-row{display:flex;align-items:center;gap:7px;min-height:28px}.side-version{white-space:nowrap}.side-update{width:26px;height:26px;display:inline-grid;place-items:center;border:0;border-radius:8px;padding:0;color:var(--accent);background:transparent}.side-update svg{width:17px;height:17px}.side-update-ready{cursor:pointer}.side-update-ready:hover{background:var(--accent-soft)}.side-update-ready:disabled{cursor:default;opacity:.55}.side-update-downloading{opacity:.72;pointer-events:none}.side-update-downloading svg{animation:flipaiUpdatePulse 1.15s ease-in-out infinite}@keyframes flipaiUpdatePulse{0%,100%{transform:translateY(0);opacity:.55}50%{transform:translateY(2px);opacity:1}}
</style>`
	updatedShell = strings.Replace(updatedShell, `</head>`, updaterStyle+`</head>`, 1)

	// Poll only FlipAi's local Home page. The real GitHub check stays on the
	// five-minute host timer; this tiny local refresh merely changes the icon
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
      try {
        await fetch('/update/install', {method:'POST', credentials:'same-origin', cache:'no-store'});
      } catch (_) {
        button.disabled = false;
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
  window.setInterval(refreshUpdateControl, 5000);
})();
</script>`
	updatedShell = strings.Replace(updatedShell, `</body>`, updaterScript+`</body>`, 1)

	for _, page := range uiPages {
		page.Funcs(template.FuncMap{"updaterState": updaterUIState})
		if _, err := page.Parse(updatedShell); err != nil {
			panic(err)
		}
	}
}
