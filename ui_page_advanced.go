package main

import (
	"net/http"
	"strings"
)

// Advanced is about FlipAi itself: the loopback service, the log files, and the
// three actions that restart or stop local processes. It deliberately owns no
// agent settings — those live with their agent on the Agents page — and no log
// export or clear, because the Activity page is where a log is being read when
// someone wants to export it.
func init() {
	registerPage("advanced", advancedPageHTML)
}

type advancedView struct {
	pageView
	Tiles     []tileView
	LastError ActivityEvent
	HasError  bool
	Health    []healthRow
	Healthy   bool
}

type healthRow struct {
	Label, Value, Tone string
}

const advancedPageHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Advanced</h1>
    <p>The local service behind this window, its log files, and the tools that restart or stop it.</p>
  </div>
  <div class="page-actions">
    <form method="post" action="/health/check"><button class="btn accent" type="submit">{{icon "check"}}Run health check</button></form>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<div class="cards-2">
  <section class="card">
    <div class="card-head divided"><div class="card-title-row"><span class="mark shield">{{icon "server"}}</span><div><h2>Local service</h2><p>The loopback control server this window is talking to.</p></div></div></div>
    <div class="card-body">
      <div class="rows">
        <div class="row"><div class="label">Loopback address</div><div class="value"><b class="mono">http://{{.S.Listen}}</b><span class="pill ok">Listening</span></div></div>
        <div class="row"><div class="label">Session token<span>Pages are only served to windows FlipAi opened itself.</span></div><div class="value"><b>Active</b><span class="pill ok">Valid</span></div></div>
        {{range .Health}}
        <div class="row"><div class="label">{{.Label}}</div><div class="value"><b class="{{.Tone}}">{{.Value}}</b></div></div>
        {{end}}
      </div>
    </div>
  </section>

  <section class="card">
    <div class="card-head divided"><div class="card-title-row"><span class="mark shield">{{icon "clock"}}</span><div><h2>Log files</h2><p>Written locally for this Windows user. Export and clear live on the Activity page.</p></div></div></div>
    <div class="card-body">
      <div class="rows">
        <div class="row"><div class="label">Open logs folder<span>{{.S.DataDir}}</span></div><div class="value"><a class="btn small" href="/open/folder?which=logs">{{icon "folder"}}Open</a></div></div>
        <div class="row"><div class="label">Read the full event log<span>Filter, search, export, or clear it there.</span></div><div class="value"><a class="btn small" href="/activity">{{icon "clock"}}Open Activity</a></div></div>
      </div>
      {{if .HasError}}
      <div class="field">
        <label>Most recent error <span class="unit">{{stamp .LastError.Time}}</span></label>
        <div class="input-suffix">
          <div class="codebox bad" id="last-error">[{{.LastError.Stage}}] {{.LastError.Message}}</div>
          <button class="btn icon" type="button" data-copy="#last-error" title="Copy">{{icon "copy"}}</button>
        </div>
      </div>
      {{else}}
      <p class="hint">No errors have been recorded.</p>
      {{end}}
    </div>
  </section>
</div>

<section class="card">
  <div class="card-head divided">
    <div class="card-title-row"><span class="mark shield">{{icon "wrench"}}</span><div><h2>Service tools</h2><p>Use with care. These actions restart or stop local FlipAi processes.</p></div></div>
  </div>
  <div class="card-body">
    <div class="rows">
      <div class="row">
        <div class="label">Restart bridge<span>Reloads settings and reconnects Gmail and the agents. Texting pauses for a moment.</span></div>
        <div class="value"><form method="post" action="/bridge/restart"><button class="btn small" type="submit">{{icon "refresh"}}Restart</button></form></div>
      </div>
      <div class="row">
        <div class="label">Quit FlipAi completely<span>Stops the window, the tray icon, the background host, and the watchdog. Texts are not processed until you start it again.</span></div>
        <div class="value"><form method="post" action="/quit" data-confirm="Stop FlipAi completely? Texts will not be processed until you start it again."><button class="btn small danger" type="submit">{{icon "power"}}Quit</button></form></div>
      </div>
    </div>
  </div>
</section>

<p class="callout">Looking for executable paths, permission modes, SMS shortcuts, or a conversation reset? Each of those belongs to one agent, so they live with Codex or Claude on the <a href="/agents">Agents</a> page. Windows startup and the startup repair are on <a href="/settings">Settings</a>.</p>
{{end}}`

func (a *App) advancedPage(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	view := advancedView{pageView: pageView{Shell: a.shell(r, "advanced", "Advanced"), S: s}}
	view.LastError, view.HasError = lastError(a.recentEvents(200))

	hostValue, hostTone := "Running", "ok"
	if s.Paused {
		hostValue, hostTone = "Paused", "warn"
	}
	bridgeValue, bridgeTone := "Not started", "warn"
	switch {
	case s.Busy:
		bridgeValue, bridgeTone = "Running a turn", "brand"
	case s.Running && s.Paused:
		bridgeValue, bridgeTone = "Paused", "warn"
	case s.Running:
		bridgeValue, bridgeTone = "Idle and watching", "ok"
	}
	view.Tiles = []tileView{
		{Icon: "server", Title: "Host status", Value: hostValue, Tone: hostTone, Sub: "Uptime " + humanDuration(s.Uptime)},
		{Icon: "link", Title: "Local endpoint", Value: "http://" + s.Listen, Tone: "", Sub: "Loopback only", Check: "ok"},
		{Icon: "folder", Title: "Data folder", Value: shortPath(s.DataDir), Tone: "", Sub: "Config, state, and logs"},
		{Icon: "bridge", Title: "Bridge", Value: bridgeValue, Tone: bridgeTone, Sub: agentSessionsSub(s)},
	}

	view.Health = []healthRow{
		{Label: "Gmail backend", Value: readyText(s.GmailReady, "Connected", "Not connected"), Tone: toneClass(s.GmailReady)},
		{Label: "SMS processing", Value: readyText(s.Running && !s.Paused, "Active", pausedOrStopped(s)), Tone: toneClass(s.Running && !s.Paused)},
		{Label: "Codex executable", Value: readyText(s.CodexFound, "Found", "Missing"), Tone: toneClass(s.CodexFound)},
		{Label: "Claude executable", Value: readyText(s.ClaudeFound, "Found", "Missing"), Tone: toneClass(s.ClaudeFound)},
	}
	view.Healthy = s.GmailReady && s.Running && !s.Paused
	a.render(w, "advanced", view)
}

func agentSessionsSub(s uiStatus) string {
	var parts []string
	if s.CodexThreadActive {
		parts = append(parts, "Codex thread")
	}
	if s.ClaudeSessionActive {
		parts = append(parts, "Claude session")
	}
	if len(parts) == 0 {
		return "No agent conversation open"
	}
	return strings.Join(parts, " · ") + " open"
}

func readyText(ok bool, good, bad string) string {
	if ok {
		return good
	}
	return bad
}

func toneClass(ok bool) string {
	if ok {
		return "ok"
	}
	return "warn"
}

func pausedOrStopped(s uiStatus) string {
	if s.Paused {
		return "Paused"
	}
	return "Waiting for setup"
}
