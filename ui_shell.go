package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Icons
// ---------------------------------------------------------------------------

// uiIconPaths holds the drawing of every icon on a shared 20x20 grid. They are
// inlined rather than fetched so the desktop window never depends on a network
// or an icon font.
var uiIconPaths = map[string]string{
	"home":         `<path d="M3.2 8.6 10 3.3l6.8 5.3V16a1 1 0 0 1-1 1h-3.3v-4.6H7.5V17H4.2a1 1 0 0 1-1-1z"/>`,
	"link":         `<path d="M8.4 11.6a3.2 3.2 0 0 0 4.6 0l2.2-2.2a3.2 3.2 0 0 0-4.6-4.6l-1 1"/><path d="M11.6 8.4a3.2 3.2 0 0 0-4.6 0l-2.2 2.2a3.2 3.2 0 0 0 4.6 4.6l1-1"/>`,
	"agent":        `<rect x="3.4" y="6.8" width="13.2" height="9.4" rx="2.6"/><path d="M10 3.2v3.6M2.6 11.4h.8M16.6 11.4h.8"/><circle cx="10" cy="2.6" r=".9" fill="currentColor" stroke="none"/><circle cx="7.6" cy="11.2" r="1.05" fill="currentColor" stroke="none"/><circle cx="12.4" cy="11.2" r="1.05" fill="currentColor" stroke="none"/>`,
	"phone":        `<path d="M6.2 3.6h2.2l1.1 2.8-1.4 1a8.4 8.4 0 0 0 4.5 4.5l1-1.4 2.8 1.1v2.2a1.6 1.6 0 0 1-1.8 1.6A11.6 11.6 0 0 1 4.6 5.4a1.6 1.6 0 0 1 1.6-1.8z"/>`,
	"clock":        `<circle cx="10" cy="10" r="7"/><path d="M10 6.2V10l2.6 1.6"/>`,
	"gear":         `<path d="M8.5 2.8h3l.3 1.9 1.4.8 1.8-.75 1.5 2.6-1.5 1.25v1.4l1.5 1.25-1.5 2.6-1.8-.75-1.4.8-.3 1.9h-3l-.3-1.9-1.4-.8-1.8.75-1.5-2.6L4.5 10V8.6L3 7.35l1.5-2.6 1.8.75 1.4-.8z"/><circle cx="10" cy="10" r="2.2"/>`,
	"sliders":      `<path d="M4 6.5h7.6M15.6 6.5h.6M4 13.5h1.6M9.6 13.5h6.6"/><circle cx="13.4" cy="6.5" r="1.8"/><circle cx="7.6" cy="13.5" r="1.8"/>`,
	"mail":         `<rect x="3" y="5" width="14" height="10" rx="2"/><path d="m3.6 6.2 5.5 4a1.5 1.5 0 0 0 1.8 0l5.5-4"/>`,
	"send":         `<path d="M17 3.4 9.2 11.2M17 3.4l-5 13.2-2.8-5.4-5.4-2.8z"/>`,
	"bridge":       `<path d="M3 13.6V9.4a7 7 0 0 1 14 0v4.2M3 13.6h14M7 13.6v-2.8M13 13.6v-2.8M10 13.6V8.4"/>`,
	"server":       `<rect x="3" y="4" width="14" height="5" rx="1.6"/><rect x="3" y="11" width="14" height="5" rx="1.6"/><path d="M6 6.5h.01M6 13.5h.01"/>`,
	"power":        `<path d="M10 3.4v6"/><path d="M14.4 6a6 6 0 1 1-8.8 0"/>`,
	"shield":       `<path d="M10 3 4.6 5.2v4.3c0 3.2 2.2 6 5.4 7.1 3.2-1.1 5.4-3.9 5.4-7.1V5.2z"/>`,
	"check":        `<circle cx="10" cy="10" r="7.2"/><path d="m6.8 10.2 2.2 2.2 4.2-4.4"/>`,
	"alert":        `<path d="M10 3.6 2.9 16.2h14.2z"/><path d="M10 8.2v3.2M10 13.8h.01"/>`,
	"refresh":      `<path d="M16.2 8.6A6.4 6.4 0 0 0 5.2 6.2L3.4 7.9"/><path d="M3.8 11.4a6.4 6.4 0 0 0 11 2.4l1.8-1.7"/><path d="M3.4 4.6v3.3h3.3M16.6 15.4v-3.3h-3.3"/>`,
	"download":     `<path d="M10 3.5v8.4M6.6 8.8 10 12.2l3.4-3.4M4 15.4h12"/>`,
	"trash":        `<path d="M4.6 5.8h10.8M8.2 5.8V4.4h3.6v1.4M6.2 5.8l.7 9.4a1.2 1.2 0 0 0 1.2 1.1h3.8a1.2 1.2 0 0 0 1.2-1.1l.7-9.4"/>`,
	"folder":       `<path d="M3.4 6.4a1.4 1.4 0 0 1 1.4-1.4h2.6l1.6 2h5.8a1.4 1.4 0 0 1 1.4 1.4v5.8a1.4 1.4 0 0 1-1.4 1.4H4.8a1.4 1.4 0 0 1-1.4-1.4z"/>`,
	"folder-up":    `<path d="M3.4 6.4a1.4 1.4 0 0 1 1.4-1.4h2.6l1.6 2h5.8a1.4 1.4 0 0 1 1.4 1.4v5.8a1.4 1.4 0 0 1-1.4 1.4H4.8a1.4 1.4 0 0 1-1.4-1.4z"/><path d="M10 14v-4M8.4 11.4 10 9.8l1.6 1.6"/>`,
	"plus":         `<path d="M10 4.6v10.8M4.6 10h10.8"/>`,
	"play":         `<path d="M6.6 4.6 15 10l-8.4 5.4z"/>`,
	"pause":        `<path d="M7.4 4.8v10.4M12.6 4.8v10.4"/>`,
	"search":       `<circle cx="9" cy="9" r="5.2"/><path d="m13 13 3.4 3.4"/>`,
	"more":         `<circle cx="10" cy="4.6" r="1.2" fill="currentColor" stroke="none"/><circle cx="10" cy="10" r="1.2" fill="currentColor" stroke="none"/><circle cx="10" cy="15.4" r="1.2" fill="currentColor" stroke="none"/>`,
	"external":     `<path d="M11.4 4.4h4.2v4.2M15.6 4.4 9.4 10.6M14 11.8v3.4a1.4 1.4 0 0 1-1.4 1.4H5.2a1.4 1.4 0 0 1-1.4-1.4V7.8a1.4 1.4 0 0 1 1.4-1.4h3.4"/>`,
	"copy":         `<rect x="7" y="7" width="9.4" height="9.4" rx="1.6"/><path d="M13 7V5.2a1.6 1.6 0 0 0-1.6-1.6H5.2a1.6 1.6 0 0 0-1.6 1.6v6.2A1.6 1.6 0 0 0 5.2 13H7"/>`,
	"terminal":     `<path d="m5 6.6 3.2 3.4L5 13.4M10.8 13.8h4.4"/>`,
	"key":          `<circle cx="7" cy="12.8" r="3"/><path d="m9.2 10.6 6-6M12.8 7 14.4 8.6M11.2 8.6l1.6 1.6"/>`,
	"wrench":       `<path d="M13.6 3.6a4 4 0 0 0-4.8 5l-4.8 4.8a1.7 1.7 0 0 0 2.4 2.4l4.8-4.8a4 4 0 0 0 5-4.8l-2.5 2.5-2.1-.5-.5-2.1z"/>`,
	"chevron":      `<path d="m8 5.4 4.6 4.6L8 14.6"/>`,
	"chevron-down": `<path d="m5.4 8 4.6 4.6L14.6 8"/>`,
	"cpu":          `<rect x="6" y="6" width="8" height="8" rx="1.6"/><path d="M8.2 3.4V6M11.8 3.4V6M8.2 14v2.6M11.8 14v2.6M3.4 8.2H6M3.4 11.8H6M14 8.2h2.6M14 11.8h2.6"/>`,
	"pause-ring":   `<circle cx="10" cy="10" r="7.2"/><path d="M8.4 7.6v4.8M11.6 7.6v4.8"/>`,
	"x-ring":       `<circle cx="10" cy="10" r="7.2"/><path d="m7.6 7.6 4.8 4.8M12.4 7.6l-4.8 4.8"/>`,
	"tag":          `<path d="M9.4 3.6H4.8a1.2 1.2 0 0 0-1.2 1.2v4.6a1.2 1.2 0 0 0 .35.85l6 6a1.2 1.2 0 0 0 1.7 0l4.6-4.6a1.2 1.2 0 0 0 0-1.7l-6-6a1.2 1.2 0 0 0-.85-.35z"/><path d="M6.9 6.9h.01"/>`,
}

func uiIcon(name string) template.HTML {
	body, ok := uiIconPaths[name]
	if !ok {
		body = uiIconPaths["bridge"]
	}
	return template.HTML(`<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + body + `</svg>`)
}

// uiSpriteNames are the icons the client script looks up by name at runtime.
var uiSpriteNames = []string{"mail", "send", "agent", "bridge", "server", "power", "shield", "alert", "folder", "folder-up", "refresh", "chevron-down"}

// uiBrandSprites are the marks the client script stamps into activity rows.
var uiBrandSprites = []string{"google", "voice", "codex", "claude"}

// ---------------------------------------------------------------------------
// Brand marks
// ---------------------------------------------------------------------------

// uiBrandMarks are the marks of the services FlipAi bridges. They identify the
// real product a row or tile refers to, instead of a generic glyph that could
// mean anything. Each is drawn inline so the window needs no network.
var uiBrandMarks = map[string]template.HTML{
	"google": template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true">` +
		`<path fill="#4285F4" d="M23 12.27c0-.79-.07-1.54-.2-2.27H12v4.51h6.16c-.27 1.43-1.07 2.64-2.29 3.45v2.87h3.7c2.17-2 3.43-4.94 3.43-8.56z"/>` +
		`<path fill="#34A853" d="M12 23.5c3.1 0 5.7-1.03 7.6-2.79l-3.7-2.87c-1.03.69-2.35 1.1-3.9 1.1-3 0-5.54-2.02-6.45-4.74H1.7v2.97A11.5 11.5 0 0 0 12 23.5z"/>` +
		`<path fill="#FBBC05" d="M5.55 14.2a6.93 6.93 0 0 1 0-4.4V6.83H1.7a11.5 11.5 0 0 0 0 10.34l3.85-2.97z"/>` +
		`<path fill="#EA4335" d="M12 5.16c1.69 0 3.2.58 4.4 1.72l3.28-3.28C17.7 1.72 15.1.5 12 .5A11.5 11.5 0 0 0 1.7 6.83L5.55 9.8C6.46 7.08 9 5.16 12 5.16z"/></svg>`),
	"voice": template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true">` +
		`<path fill="#1a73e8" d="M4.6 2.5h14.8c1.16 0 2.1.94 2.1 2.1v10.6c0 1.16-.94 2.1-2.1 2.1H9.9l-4.3 4v-4h-1c-1.16 0-2.1-.94-2.1-2.1V4.6c0-1.16.94-2.1 2.1-2.1z"/>` +
		`<path fill="#fff" d="M8.6 6.2h1.7l.85 2.15-1.1.78a6.4 6.4 0 0 0 3.4 3.4l.78-1.1 2.15.85v1.7c0 .68-.58 1.23-1.26 1.18A9.1 9.1 0 0 1 7.4 7.46 1.2 1.2 0 0 1 8.6 6.2z"/></svg>`),
	"codex":  template.HTML(`<span class="glyph">&gt;_</span>`),
	"claude": template.HTML(`<span class="glyph">A\</span>`),
}

func uiBrand(name string) template.HTML {
	if m, ok := uiBrandMarks[name]; ok {
		return m
	}
	return ""
}

// ---------------------------------------------------------------------------
// Shared view model
// ---------------------------------------------------------------------------

// navEntry is one sidebar link. Group opens a labelled section above the link,
// so the seven pages read as three short lists rather than one undifferentiated
// column: what FlipAi is doing, what it is bridging, and the app itself.
type navEntry struct {
	Key, Href, Label, Icon string
	Group                  string
}

var uiNav = []navEntry{
	{Key: "home", Href: "/", Label: "Home", Icon: "home"},
	{Key: "connections", Href: "/connections", Label: "Connections", Icon: "link", Group: "Bridge"},
	{Key: "agents", Href: "/agents", Label: "Agents", Icon: "agent"},
	{Key: "phone", Href: "/phone", Label: "Phone", Icon: "phone"},
	{Key: "activity", Href: "/activity", Label: "Activity", Icon: "clock"},
	{Key: "settings", Href: "/settings", Label: "Settings", Icon: "gear", Group: "App"},
	{Key: "advanced", Href: "/advanced", Label: "Advanced", Icon: "sliders"},
}

// shellData is everything the frame around a page needs: navigation state,
// window preferences, and the running state shown in the sidebar footer.
type shellData struct {
	Nav        string
	Title      string
	Version    string
	Theme      string
	Compact    bool
	Alerts     bool
	AlertSound bool

	Running     bool
	Paused      bool
	StatusLabel string
	StatusTone  string

	Flash     string
	FlashTone string

	UpdateVersion string
	UpdateNotes   string

	Items []navEntry
}

// ThemeAttr is empty for the system theme so the stylesheet's
// prefers-color-scheme rules decide.
func (s shellData) ThemeAttr() string {
	if s.Theme == ThemeSystem {
		return ""
	}
	return s.Theme
}

func (s shellData) DotClass() string {
	switch {
	case !s.Running:
		return "dot stopped"
	case s.Paused:
		return "dot paused"
	default:
		return "dot"
	}
}

// flashFor turns the ?saved=… / ?notice=… redirect markers into the banner
// shown at the top of a page. Only known keys render, so nothing a URL carries
// can be echoed into the page.
var uiFlashes = map[string][2]string{
	"saved":             {"ok", "Settings saved."},
	"saved-restart":     {"ok", "Settings saved. The background bridge is restarting with the new configuration."},
	"claude-token-only": {"warn", "Saved. The token keeps Claude answering texts, but it cannot control Chrome or open claude.ai/code — press Connect Claude under Authentication & session to sign in properly."},
	"paused":            {"warn", "FlipAi is paused. Incoming texts stay unread in Gmail until you resume."},
	"resumed":           {"ok", "FlipAi resumed. New texts are being processed again."},
	"number-added":      {"ok", "Phone number added to the allowlist."},
	"number-removed":    {"ok", "Phone number removed from the allowlist."},
	"logs-cleared":      {"ok", "Activity log cleared."},
	"restarting":        {"ok", "The background bridge is restarting."},
	"startup-on":        {"ok", "FlipAi will now start when this Windows user signs in."},
	"startup-off":       {"ok", "FlipAi will no longer start automatically at sign-in."},
	"reset":             {"warn", "FlipAi setup was reset. Reconnect Gmail to start again."},
	"boot-on":           {"ok", "FlipAi will now start when this PC powers on, before anyone signs in."},
	"boot-off":          {"ok", "FlipAi will start at sign-in only."},
	"update-current":    {"ok", "FlipAi is up to date."},
	"update-found":      {"warn", "A newer FlipAi release is available."},
}

func (a *App) shell(r *http.Request, nav, title string) shellData {
	a.mu.Lock()
	cfg := a.cfg
	b := a.bridge
	a.mu.Unlock()

	s := shellData{
		Nav: nav, Title: title, Version: version,
		Theme: normalizeTheme(cfg.UI.Theme), Compact: cfg.UI.Compact,
		Alerts: cfg.UI.Alerts, AlertSound: cfg.UI.AlertSound,
		Items: uiNav,
	}
	s.Running = b != nil
	s.Paused = cfg.Paused
	switch {
	case s.Paused:
		s.StatusLabel, s.StatusTone = "Paused", "warn"
	case s.Running:
		s.StatusLabel, s.StatusTone = "Running", "ok"
	default:
		s.StatusLabel, s.StatusTone = "Setup needed", "warn"
	}
	if info := loadUpdateState(a.statePath); info.Newer() {
		s.UpdateVersion = info.Version
		s.UpdateNotes = info.PageURL
	}
	if key := r.URL.Query().Get("ok"); key != "" {
		if f, known := uiFlashes[key]; known {
			s.FlashTone, s.Flash = f[0], f[1]
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

var uiFuncs = template.FuncMap{
	"icon":         uiIcon,
	"brand":        uiBrand,
	"safeHTML":     func(s string) template.HTML { return template.HTML(s) },
	"sprites":      func() []string { return uiSpriteNames },
	"brandSprites": func() []string { return uiBrandSprites },
	"phone":        formatUSPhone,
	"stamp": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Format("1/2/2006 3:04:05 PM")
	},
	"day": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Format("1/2/2006 3:04 PM")
	},
	"ago":    humanSince,
	"uptime": humanDuration,
	"lower":  strings.ToLower,
	"tone":   func(ok bool) string { return map[bool]string{true: "ok", false: "warn"}[ok] },
	"okBad":  func(ok bool) string { return map[bool]string{true: "ok", false: "bad"}[ok] },
	"yesNo":  func(ok bool) string { return map[bool]string{true: "Yes", false: "No"}[ok] },
	"checkFor": func(ok bool) template.HTML {
		return map[bool]template.HTML{true: uiIcon("check"), false: uiIcon("alert")}[ok]
	},
}

func humanSince(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < 45*time.Second:
		return "just now"
	case d < 90*time.Second:
		return "1 minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "1 hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

const shellHTML = `{{define "shell"}}<!doctype html>
<html lang="en"{{with .Shell.ThemeAttr}} data-theme="{{.}}"{{end}}{{if .Shell.Compact}} data-compact="1"{{end}}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Shell.Title}} — FlipAi</title>
<link rel="stylesheet" href="/assets/flipai.css?v={{.Shell.Version}}">
<script src="/assets/flipai.js?v={{.Shell.Version}}" defer></script>
</head>
<body{{if .Shell.Alerts}} data-alerts="1"{{end}}{{if .Shell.AlertSound}} data-alert-sound="1"{{end}}>
<div id="icon-sprites" hidden>{{range sprites}}<span id="icon-{{.}}">{{icon .}}</span>{{end}}{{range brandSprites}}<span id="brand-{{.}}">{{brand .}}</span>{{end}}</div>
<div class="app">
  <aside class="sidebar">
    <div class="brand"><span class="brand-mark">F</span><span>FlipAi</span></div>
    <nav class="nav">
      {{range .Shell.Items}}{{with .Group}}<div class="nav-label">{{.}}</div>{{end}}<a href="{{.Href}}"{{if eq .Key $.Shell.Nav}} aria-current="page"{{end}}>{{icon .Icon}}<span>{{.Label}}</span></a>{{end}}
    </nav>
    <div class="side-status">
      <b><span class="{{.Shell.DotClass}}"></span><span data-status="runningLabel">{{if .Shell.Paused}}FlipAi is paused{{else if .Shell.Running}}FlipAi is running{{else}}FlipAi is idle{{end}}</span></b>
      {{if .Shell.UpdateVersion}}<a class="side-update" href="/settings#updates" title="FlipAi {{.Shell.UpdateVersion}} is available">{{icon "download"}}<span>v{{.Shell.Version}} &rarr; {{.Shell.UpdateVersion}}</span></a>{{else}}<span>v{{.Shell.Version}}</span>{{end}}
    </div>
  </aside>
  <main class="content">
    {{if .Shell.Flash}}<div class="banner {{.Shell.FlashTone}}">{{icon "check"}}<span>{{.Shell.Flash}}</span></div>{{end}}
    {{if .Shell.UpdateVersion}}
    <div class="banner update">
      {{icon "download"}}
      <span><b>FlipAi {{.Shell.UpdateVersion}} is available.</b> Installing keeps your settings, agents, and phone numbers — it is not a fresh setup.</span>
      <form method="post" action="/update/install"><button class="btn primary" type="submit">Install update</button></form>
      <a class="btn" href="/settings">Details</a>
    </div>
    {{end}}
    {{template "content" .}}
  </main>
</div>
<div class="modal" id="folder-picker" hidden>
  <div class="modal-card">
    <div class="modal-head"><h2>Choose a folder</h2><p data-picker-path>—</p></div>
    <div class="modal-list"></div>
    <div class="modal-foot">
      <button type="button" class="btn" data-picker-cancel>Cancel</button>
      <button type="button" class="btn primary" data-picker-choose>Use this folder</button>
    </div>
  </div>
</div>
</body>
</html>{{end}}`

// uiPages maps a page name to the template that defines its content block.
var uiPages = map[string]*template.Template{}

func registerPage(name, body string) {
	uiPages[name] = template.Must(template.New(name).Funcs(uiFuncs).Parse(shellHTML + uiPartials + body))
}

func (a *App) render(w http.ResponseWriter, name string, data any) {
	tpl, ok := uiPages[name]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := tpl.ExecuteTemplate(w, "shell", data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

// redirectTo sends the browser back to a page with an optional flash key. It is
// the standard post/redirect/get finish for every action in the desktop UI.
func redirectTo(w http.ResponseWriter, r *http.Request, path, flash string) {
	if flash != "" {
		path += "?ok=" + flash
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// sortedStages lists the stages present in a slice of events, for the Activity
// page filter.
func sortedStages(events []ActivityEvent) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range events {
		if e.Stage != "" && !seen[e.Stage] {
			seen[e.Stage] = true
			out = append(out, e.Stage)
		}
	}
	sort.Strings(out)
	return out
}
