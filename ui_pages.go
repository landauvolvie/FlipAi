package main

import (
	"net/http"
	"strings"
)

// Every page is the same shell around one content block. The blocks live here
// as templates so the markup that produces a screen sits next to the handler
// that fills it in.

type pageView struct {
	Shell shellData
	S     uiStatus
}

// tileView is one status tile. Value/Tone report state that the snapshot
// actually verified; Check draws the small confirmation mark in the corner.
type tileView struct {
	Icon      string
	Brand     string
	Brandish  bool
	Title     string
	Value     string
	Tone      string
	Big       bool
	Sub       string
	Check     string
	FootLabel string
	FootValue string
	FootTone  string
}

const uiPartials = `
{{define "tile"}}<div class="tile">
  <div class="tile-top">
    {{if .Brand}}<span class="bmark {{.Brand}}">{{brand .Brand}}</span>{{else}}<div class="tile-icon{{if .Brandish}} brandish{{end}}">{{icon .Icon}}</div>{{end}}
    {{if .Check}}<span class="check {{.Check}}">{{if eq .Check "ok"}}{{icon "check"}}{{else}}{{icon "alert"}}{{end}}</span>{{end}}
  </div>
  <h3>{{.Title}}</h3>
  <div class="val {{.Tone}}{{if .Big}} big{{end}}">{{.Value}}</div>
  {{if .Sub}}<div class="sub">{{.Sub}}</div>{{end}}
  {{if .FootLabel}}<div class="tile-foot"><span>{{.FootLabel}}</span><span class="{{.FootTone}}">{{.FootValue}}</span></div>{{end}}
</div>{{end}}

{{define "eventhead"}}<thead><tr>
  <th>Time</th><th>Stage</th><th>Status</th><th>Message</th><th>Sender</th><th>Agent</th><th class="num">Duration</th>
</tr></thead>{{end}}

{{define "promptEditor"}}<div class="prompt-editor" data-prompt-editor>
  <div class="prompt-editor-head">
    <div class="label">{{icon "send"}}<span>{{.Title}}</span>{{if .Custom}}<span class="pill brand">Custom</span>{{else}}<span class="pill">Default</span>{{end}}</div>
    <div class="tools"><button class="btn small" type="button" data-prompt-reset>{{icon "refresh"}}Reset</button></div>
  </div>
  <textarea name="{{.Name}}" rows="3" maxlength="{{.Max}}" spellcheck="true" data-prompt-input data-prompt-fallback="{{.Fallback}}" placeholder="{{.Fallback}}">{{.Value}}</textarea>
  <div class="prompt-editor-foot">
    <span>{{.Hint}}</span>
    <span class="prompt-count" data-prompt-count>0</span>
  </div>
  <p class="prompt-preview" data-prompt-preview></p>
</div>{{end}}
`

func registerUIPages() {
	registerPage("home", homeHTML)
	registerPage("connections", connectionsHTML)
	registerPage("activity", activityPageHTML)
	registerPage("settings", settingsHTML)
}

func init() { registerUIPages() }

// ---------------------------------------------------------------------------
// Home
// ---------------------------------------------------------------------------

type homeView struct {
	pageView
	Tiles []tileView
}

const homeHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>FlipAi <span class="pill {{.Shell.StatusTone}}">{{.Shell.StatusLabel}}</span></h1>
    <p>Bridge Google Voice SMS commands to the Codex and Claude agents running on this PC.</p>
  </div>
  <div class="page-actions">
    {{if .S.Paused}}
      <form method="post" action="/bridge/resume"><button class="btn accent" type="submit">{{icon "play"}}Resume FlipAi</button></form>
    {{else}}
      <form method="post" action="/bridge/pause"><button class="btn" type="submit">{{icon "pause"}}Pause FlipAi</button></form>
    {{end}}
  </div>
</div>

{{if .S.SetupPending}}
<section class="card">
  <div class="card-head">
    <div><h2>Finish setup</h2><p>{{.S.SetupPending}} step(s) left before a text can reach an agent.</p></div>
  </div>
  <div class="card-body">
    <div class="rows">
      {{range .S.SetupSteps}}
      <div class="row">
        <div class="label">{{.Label}}<span>{{.Detail}}</span></div>
        <div class="value">
          {{if .Done}}<span class="pill ok">Done</span>{{else}}<a class="btn small accent" href="{{.Href}}">Open{{icon "chevron"}}</a>{{end}}
        </div>
      </div>
      {{end}}
    </div>
  </div>
</section>
{{end}}

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<section class="card" data-feed data-per-page="5">
  <div class="card-head">
    <div><h2>Recent activity</h2><p>The newest steps FlipAi took, oldest details stay in the Activity page.</p></div>
    <div class="head-actions"><a class="linky" href="/activity">View all Activity ›</a></div>
  </div>
  <div class="table-wrap">
    <table>
      {{template "eventhead"}}
      <tbody data-events><tr><td colspan="7"><div class="empty">Loading activity…</div></td></tr></tbody>
    </table>
  </div>
  <div class="table-foot">
    <span data-count>—</span>
    <a class="linky" href="/activity">Open the full log{{icon "chevron"}}</a>
  </div>
</section>
{{end}}`

func (a *App) homePage(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	gmailValue, gmailTone := "Not connected", "warn"
	gmailSub := "Choose a connection method"
	if s.GmailReady {
		gmailValue, gmailTone = "Connected", "ok"
		gmailSub = "Ready to send and receive"
		if s.GmailEmail != "" {
			gmailSub = s.GmailEmail
		}
	}
	codexValue, codexTone := checkLabel(s.CodexCheck, "Ready")
	claudeValue, claudeTone := checkLabel(s.ClaudeCheck, "Ready")
	phoneValue, phoneTone := "No numbers yet", "warn"
	if s.AllowedCount == 1 {
		phoneValue, phoneTone = "1 allowed number", "ok"
	} else if s.AllowedCount > 1 {
		phoneValue, phoneTone = plural(s.AllowedCount, "allowed number"), "ok"
	}
	securityValue, securityTone, securitySub := "Allowlist only", "ok", "Only allowed numbers can reach agents"
	if s.RequireCode {
		securityValue, securityTone, securitySub = "Code required", "ok", "Every text must start with your code"
	}
	view := homeView{pageView: pageView{Shell: a.shell(r, "home", "FlipAi"), S: s}}
	view.Tiles = []tileView{
		{Brand: "google", Title: "Google Voice / Gmail", Value: gmailValue, Tone: gmailTone, Sub: gmailSub, Check: checkTone(s.GmailReady)},
		{Brand: "codex", Title: "Codex", Value: codexValue, Tone: codexTone, Sub: agentRole(s, "C"), Check: checkTone(s.CodexCheck.OK)},
		{Brand: "claude", Title: "Claude", Value: claudeValue, Tone: claudeTone, Sub: agentRole(s, "A"), Check: checkTone(s.ClaudeCheck.OK)},
		{Icon: "phone", Title: "Phone", Value: phoneValue, Tone: phoneTone, Sub: pausedSub(s), Check: checkTone(s.AllowedCount > 0)},
		{Icon: "shield", Title: "Security", Value: securityValue, Tone: securityTone, Sub: securitySub, Check: "ok"},
	}
	a.render(w, "home", view)
}

func checkTone(ok bool) string {
	if ok {
		return "ok"
	}
	return "warn"
}

func agentRole(s uiStatus, agent string) string {
	if s.DefaultAgent == agent {
		return "Default agent"
	}
	if agent == "C" {
		return "Handles " + s.CodexPrefix + ": messages"
	}
	return "Handles " + s.ClaudePrefix + ": messages"
}

func pausedSub(s uiStatus) string {
	switch {
	case s.Paused:
		return "Paused — texts stay unread"
	case s.AllowedCount == 0:
		return "Add a number to start"
	case !s.Running:
		return "Bridge not started yet"
	default:
		return "Receiving messages"
	}
}

// shortPath trims a long Windows path to something that fits a tile, keeping
// the tail because the last folder is the part that identifies it.
func shortPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "Not set"
	}
	if len(p) <= 34 {
		return p
	}
	return "…" + p[len(p)-33:]
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}

// ---------------------------------------------------------------------------
// Connections
// ---------------------------------------------------------------------------

type connectionsView struct {
	pageView
	Tiles          []tileView
	HasCredentials bool
	HasAppPassword bool
	LastSync       string
	ReplyReady     bool
}

const connectionsHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Connections</h1>
    <p>Configure how FlipAi reads Google Voice texts from Gmail and sends replies back.</p>
  </div>
  <div class="page-actions">
    <a class="btn" href="/connections">{{icon "refresh"}}Refresh</a>
    <button class="btn accent" type="button" data-test="/gmail/test" data-test-busy="Checking Gmail">{{icon "send"}}Test Gmail</button>
  </div>
</div>


<section class="card">
  <div class="card-head divided">
    <div class="card-title-row">
      <span class="bmark lg google">{{brand "google"}}</span>
      <div>
        <h2>Gmail / Google Voice <span class="pill {{if .S.GmailReady}}ok{{else}}warn{{end}}">{{if .S.GmailReady}}Connected{{else}}Not connected{{end}}</span></h2>
        <p>Send and receive SMS through Gmail and Google Voice.</p>
      </div>
    </div>
    <div class="head-actions"><button class="btn" type="button" data-reveal="#gmail-form">{{icon "gear"}}Manage</button></div>
  </div>
  <div class="card-body">
    <div class="rows">
      <div class="row"><div class="label">Authentication method</div><div class="value"><b>{{.S.GmailMethodLabel}}</b>{{if .S.GmailReady}}<span class="pill ok">Valid</span>{{else if .S.GmailMethod}}<span class="pill warn">Incomplete</span>{{end}}</div></div>
      <div class="row"><div class="label">Google account</div><div class="value"><b>{{if .S.GmailEmail}}{{.S.GmailEmail}}{{else if eq .S.GmailMethod "oauth"}}Signed in with OAuth{{else}}Not set yet{{end}}</b></div></div>
      <div class="row"><div class="label">Reply address<span>FlipAi always answers the authenticated Google Voice thread the text arrived on.</span></div><div class="value"><b>Authenticated Voice thread</b>{{if .ReplyReady}}<span class="pill ok">Ready</span>{{else}}<span class="pill warn">Waiting</span>{{end}}</div></div>
      <div class="row"><div class="label">Last mailbox check</div><div class="value"><b>{{.LastSync}}</b>{{if .S.LastPollErr}}<span class="pill bad">Error</span>{{end}}</div></div>
    </div>
    {{if .S.LastPollErr}}<p class="hint">Last error: {{.S.LastPollErr}}</p>{{end}}

    <div id="gmail-form" class="hidden">
      <form method="post" action="/connections/save" enctype="multipart/form-data">
        <details class="disclosure" open>
          <summary>Gmail connection</summary>
          <div class="disclosure-body">
            <div class="grid-2">
              <div class="field">
                <label for="gmailMethod">Connection method</label>
                <select id="gmailMethod" name="gmailMethod">
                  <option value="app_password"{{if eq .S.GmailMethod "app_password"}} selected{{end}}>Gmail App Password</option>
                  <option value="oauth"{{if eq .S.GmailMethod "oauth"}} selected{{end}}>Your own Google API project (OAuth)</option>
                </select>
                <p class="hint">App Password uses IMAP IDLE, so new texts wake FlipAi almost immediately.</p>
              </div>
              <div class="field">
                <label for="gmailEmail">Gmail address</label>
                <input id="gmailEmail" type="email" name="gmailEmail" value="{{.S.GmailEmail}}" placeholder="you@gmail.com">
                <p class="hint">Used with the App Password method.</p>
              </div>
            </div>
            <div class="grid-2">
              <div class="field">
                <label for="appPassword">Google App Password{{if .HasAppPassword}} — saved{{end}}</label>
                <input id="appPassword" type="password" name="appPassword" autocomplete="new-password" placeholder="{{if .HasAppPassword}}leave blank to keep the saved password{{else}}xxxx xxxx xxxx xxxx{{end}}">
                <p class="hint">Requires Google 2-Step Verification. Stored encrypted for this Windows user.</p>
              </div>
              <div class="field">
                <label for="credentials">OAuth Desktop credentials JSON{{if .HasCredentials}} — saved{{end}}</label>
                <input id="credentials" type="file" name="credentials" accept="application/json,.json">
                <p class="hint">Only for the "your own Google API project" method.</p>
              </div>
            </div>
            <div class="field">
              <label for="subjectPhrase">Subject phrase match</label>
              <input id="subjectPhrase" type="text" name="subjectPhrase" value="{{.S.SubjectPhrase}}">
              <p class="hint">Only Gmail messages whose subject contains this phrase are treated as Google Voice texts.</p>
            </div>
            <div class="form-actions">
              <button class="btn primary" type="submit">Save connection</button>
              {{if eq .S.GmailMethod "oauth"}}{{if .HasCredentials}}<a class="btn" href="/oauth/google/start">Connect Google account</a>{{end}}{{end}}
              <button class="btn" type="button" data-test="/gmail/test" data-test-busy="Checking Gmail">Test Gmail</button>
            </div>
          </div>
        </details>
      </form>
    </div>
  </div>
</section>

<section class="card">
  <div class="card-head divided">
    <div class="card-title-row">
      <span class="mark shield">{{icon "send"}}</span>
      <div><h2>End-to-end check</h2><p>Walks the whole path — read the mailbox, route the text, answer the Voice thread — and reports where it stops.</p></div>
    </div>
    <div class="head-actions"><button class="btn accent" type="button" data-test="/connections/flowtest" data-test-method="POST" data-test-busy="Running the check">{{icon "send"}}Test message flow</button></div>
  </div>
  <div class="card-body">
    <div class="rows">
      <div class="row"><div class="label">Mailbox<span>FlipAi reads Google Voice notifications from this account.</span></div><div class="value"><b class="{{tone .S.GmailReady}}">{{if .S.GmailReady}}Reachable{{else}}Not connected{{end}}</b></div></div>
      <div class="row"><div class="label">Senders<span>Who may reach an agent is configured on the Phone page.</span></div><div class="value"><b>{{if .S.AllowedCount}}{{.S.AllowedCount}} allowed{{else}}None yet{{end}}</b><a class="linky" href="/phone">Open Phone{{icon "chevron"}}</a></div></div>
      <div class="row"><div class="label">Agents<span>Which agent answers is configured on the Agents page.</span></div><div class="value"><b>{{.S.DefaultAgentName}} by default</b><a class="linky" href="/agents">Open Agents{{icon "chevron"}}</a></div></div>
    </div>
  </div>
</section>
{{end}}`

func (a *App) connectionsPage(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	view := connectionsView{
		pageView:       pageView{Shell: a.shell(r, "connections", "Connections"), S: s},
		HasCredentials: fileExists(a.cfg.Gmail.CredentialsFile),
		HasAppPassword: hasAppPasswordSecret(appPasswordPath(a.dataDir)),
		ReplyReady:     s.GmailReady,
	}
	view.LastSync = "Not checked yet"
	if !s.LastPollAt.IsZero() {
		view.LastSync = humanSince(s.LastPollAt)
	}
	gmailValue, gmailTone := "Not connected", "warn"
	if s.GmailReady {
		gmailValue, gmailTone = "Connected", "ok"
	}
	replyValue, replyTone := "Waiting for Gmail", "warn"
	if s.GmailReady {
		replyValue, replyTone = "Reply ready", "ok"
	}
	filterValue, filterTone := "Every message", ""
	if s.SubjectPhrase != "" {
		filterValue, filterTone = "Subject match", "ok"
	}
	view.Tiles = []tileView{
		{Brand: "google", Title: "Gmail", Value: gmailValue, Tone: gmailTone, Sub: s.GmailMethodLabel, Check: checkTone(s.GmailReady)},
		{Brand: "voice", Title: "Voice reply", Value: replyValue, Tone: replyTone, Sub: "Replies go to the sender's Voice thread", Check: checkTone(s.GmailReady)},
		{Icon: "mail", Title: "Inbox filter", Value: filterValue, Tone: filterTone, Sub: subjectPhraseSub(s)},
		{Icon: "clock", Title: "Last sync", Value: view.LastSync, Tone: "", Sub: mailboxSub(s), Check: checkTone(s.LastPollErr == "" && !s.LastPollAt.IsZero())},
	}
	a.render(w, "connections", view)
}

// subjectPhraseSub names the phrase a Gmail message must carry to count as a
// Google Voice text, so the tile explains the filter rather than only its state.
func subjectPhraseSub(s uiStatus) string {
	if s.SubjectPhrase == "" {
		return "No subject phrase required"
	}
	return "Subject contains “" + s.SubjectPhrase + "”"
}

func mailboxSub(s uiStatus) string {
	switch {
	case s.LastPollErr != "":
		return "Last check failed"
	case s.Paused:
		return "Paused"
	case s.LastPollAt.IsZero():
		return "Waiting for the bridge to start"
	default:
		return "Mailbox reachable"
	}
}

// ---------------------------------------------------------------------------
// Phone
// ---------------------------------------------------------------------------

type activityView struct {
	pageView
	Stages []string
}

const activityPageHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Activity</h1>
    <p>Every step FlipAi took, from the Gmail check through agent execution to the reply.</p>
  </div>
  <div class="page-actions">
    <a class="btn" href="/activity">{{icon "refresh"}}Refresh</a>
    <a class="btn" href="/logs/export">{{icon "download"}}Export logs</a>
    <form method="post" action="/activity/clear" data-confirm="Clear the FlipAi activity history?"><button class="btn danger" type="submit">{{icon "trash"}}Clear logs</button></form>
  </div>
</div>

<div data-feed data-filterable data-per-page="10">
  <div class="tiles">
    <div class="tile">
      <div class="tile-top"><span class="bmark google">{{brand "google"}}</span></div>
      <h3>Gmail / Voice</h3><div class="val" data-summary="gmail">Waiting</div><div class="sub">Mailbox checks and message reads</div>
    </div>
    <div class="tile">
      <div class="tile-top"><div class="tile-icon">{{icon "agent"}}</div></div>
      <h3>Agents</h3><div class="val" data-summary="agent">Waiting</div><div class="sub">Codex and Claude turns</div>
    </div>
    <div class="tile">
      <div class="tile-top"><span class="bmark voice">{{brand "voice"}}</span></div>
      <h3>Reply</h3><div class="val" data-summary="reply">Waiting</div><div class="sub">Last reply <span data-summary="reply" data-summary-time>—</span></div>
    </div>
    <div class="tile">
      <div class="tile-top"><div class="tile-icon">{{icon "clock"}}</div></div>
      <h3>Latest event</h3><div class="val brand" data-latest>—</div><div class="sub" data-latest-time>—</div>
    </div>
  </div>

  <div class="filters">
    <select data-filter="stage" aria-label="Filter by stage">
      <option value="">All stages</option>
      {{range .Stages}}<option value="{{.}}">{{.}}</option>{{end}}
    </select>
    <select data-filter="agent" aria-label="Filter by agent">
      <option value="">All agents</option>
      <option value="C">Codex</option>
      <option value="A">Claude</option>
    </select>
    <label class="search">{{icon "search"}}<input type="search" data-filter="q" placeholder="Search activity…" aria-label="Search activity"></label>
    <select data-filter="range" aria-label="Filter by time">
      <option value="">Any time</option>
      <option value="1">Last hour</option>
      <option value="24">Last 24 hours</option>
      <option value="168">Last 7 days</option>
    </select>
  </div>

  <section class="card">
    <div class="table-wrap">
      <table>
        {{template "eventhead"}}
        <tbody data-events><tr><td colspan="7"><div class="empty">Loading activity…</div></td></tr></tbody>
      </table>
    </div>
    <div class="table-foot">
      <span data-count>—</span>
      <div class="pager" data-pager></div>
    </div>
  </section>
</div>

<p class="callout">Privacy: FlipAi logs statuses, stages, and errors only. Message text, agent prompts and results, security codes, Gmail passwords, and OAuth tokens are never written to this log.</p>
{{end}}`

func (a *App) activityPage(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	view := activityView{
		pageView: pageView{Shell: a.shell(r, "activity", "Activity"), S: s},
		Stages:   sortedStages(a.recentEvents(500)),
	}
	a.render(w, "activity", view)
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

type settingsView struct {
	pageView
	Tiles []tileView

	// Absorbed from the retired Advanced page: one settings screen, not two.
	LastError ActivityEvent
	HasError  bool
	Health    []healthRow
	Healthy   bool
}

const settingsHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Settings</h1>
    <p>General app preferences and desktop behavior.</p>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<section class="card" id="updates">
  <div class="card-head divided">
    <div class="card-title-row">
      <span class="mark shield">{{icon "download"}}</span>
      <div><h2>Updates</h2><p>FlipAi installs over itself, keeping your settings and connections.</p></div>
    </div>
    <div class="head-actions">
      <form method="post" action="/update/check"><button class="btn" type="submit">{{icon "refresh"}}Check for updates</button></form>
      {{if .S.Update.Newer}}<form method="post" action="/update/install"><button class="btn primary" type="submit">{{icon "download"}}Install {{.S.Update.Version}}</button></form>{{end}}
    </div>
  </div>
  <div class="card-body">
    <div class="rows">
      <div class="row"><div class="label">Installed version</div><div class="value"><b>v{{.S.Version}}</b></div></div>
      <div class="row">
        <div class="label">Latest release</div>
        <div class="value">
          {{if .S.Update.Version}}<b>v{{.S.Update.Version}}</b>{{else}}<b>Not checked yet</b>{{end}}
          {{if .S.Update.Newer}}<span class="pill warn">Update available</span>{{else if .S.Update.Version}}<span class="pill ok">Up to date</span>{{end}}
        </div>
      </div>
      <div class="row"><div class="label">Last checked</div><div class="value"><b>{{if .S.Update.CheckedAt.IsZero}}Never{{else}}{{ago .S.Update.CheckedAt}}{{end}}</b>{{if .S.Update.Error}}<span class="pill bad">Check failed</span>{{end}}</div></div>
      {{if .S.Update.Error}}<div class="row"><div class="label">Last error</div><div class="value">{{.S.Update.Error}}</div></div>{{end}}
    </div>
    {{if .S.Update.Newer}}
    <p class="hint">Installing runs the signed-in-user installer for v{{.S.Update.Version}} in place. It keeps this install's folder, settings, allowed numbers, and Windows startup choice, and asks no setup questions. FlipAi reopens when it finishes.</p>
    {{end}}
    <form method="post" action="/settings/updates">
      <div class="toggle">
        <div class="label">Install updates automatically<span>FlipAi downloads the release, checks it against the checksum published with it, installs it, and comes back on the new version. It never interrupts an SMS turn that is already running.</span></div>
        <label class="switch"><input type="hidden" name="autoUpdate" value="0"><input type="checkbox" name="autoUpdate" value="1" data-autosubmit{{if .S.AutoUpdate}} checked{{end}}><span class="slider"></span></label>
      </div>
      <div class="field">
        <label for="updateCheckMinutes">Check for updates every</label>
        <select id="updateCheckMinutes" name="updateCheckMinutes" data-autosubmit>
          <option value="5"{{if eq .S.UpdateCheckMinutes 5}} selected{{end}}>Every 5 minutes</option>
          <option value="10"{{if eq .S.UpdateCheckMinutes 10}} selected{{end}}>Every 10 minutes</option>
          <option value="30"{{if eq .S.UpdateCheckMinutes 30}} selected{{end}}>Every 30 minutes</option>
          <option value="60"{{if eq .S.UpdateCheckMinutes 60}} selected{{end}}>Hourly</option>
          <option value="360"{{if eq .S.UpdateCheckMinutes 360}} selected{{end}}>Every 6 hours</option>
          <option value="1440"{{if eq .S.UpdateCheckMinutes 1440}} selected{{end}}>Daily</option>
          <option value="10080"{{if eq .S.UpdateCheckMinutes 10080}} selected{{end}}>Weekly</option>
        </select>
        <p class="hint">FlipAi checks on this schedule in the background, so you never have to open this page to find out. A new release also shows next to the version in the sidebar.</p>
      </div>
    </form>
  </div>
</section>

<div class="cards-2">
  <section class="card">
    <div class="card-head"><div class="card-title-row"><span class="mark shield">{{icon "power"}}</span><div><h2>Startup</h2><p>How FlipAi behaves when Windows starts and when you close the window.</p></div></div></div>
    <div class="card-body">
      <form method="post" action="/settings/startup">
        <div class="toggle">
          <div class="label">Start FlipAi with Windows<span>Registers FlipAi for this user only. No administrator rights are used.</span></div>
          <label class="switch"><input type="hidden" name="startup" value="0"><input type="checkbox" name="startup" value="1" data-autosubmit{{if .S.StartupEnabled}} checked{{end}}><span class="slider"></span></label>
        </div>
      </form>
      <form method="post" action="/settings/bootstartup" data-confirm="Windows will ask for administrator approval to create the startup task. Continue?">
        <div class="toggle">
          <div class="label">Start before sign-in
            <span>Runs FlipAi when this PC powers on, without waiting for anyone to sign in. Windows asks for administrator approval once, here — never during installation. Saved credentials are re-protected for this PC so they can be read at boot.</span>
          </div>
          <label class="switch"><input type="hidden" name="bootStartup" value="0"><input type="checkbox" name="bootStartup" value="1" data-autosubmit{{if .S.BootStartupEnabled}} checked{{end}}><span class="slider"></span></label>
        </div>
      </form>
      <form method="post" action="/settings/save">
        <div class="toggle">
          <div class="label">Close to tray<span>Closing the window leaves the bridge running in the notification area. Turn this off to quit FlipAi when the window closes.</span></div>
          <label class="switch"><input type="hidden" name="closeToTray" value="0"><input type="checkbox" name="closeToTray" value="1" data-autosubmit{{if .S.CloseToTray}} checked{{end}}><span class="slider"></span></label>
        </div>
      </form>
      <div class="rows" style="margin-top:14px;border-top:1px solid var(--line-soft);padding-top:14px">
        <div class="row">
          <div class="label">Repair the startup entry<span>Rewrites this user's Run registry value to point at the current FlipAi executable. Use it after moving or reinstalling FlipAi.</span></div>
          <div class="value"><form method="post" action="/settings/startup"><input type="hidden" name="startup" value="1"><button class="btn small" type="submit">{{icon "wrench"}}Repair</button></form></div>
        </div>
      </div>
    </div>
  </section>

  <section class="card">
    <div class="card-head"><div class="card-title-row"><span class="mark shield">{{icon "sliders"}}</span><div><h2>Appearance</h2><p>Choose how FlipAi looks.</p></div></div></div>
    <div class="card-body">
      <form method="post" action="/settings/save">
        <div class="field">
          <label for="theme">Theme</label>
          <select id="theme" name="theme" data-autosubmit>
            <option value="light"{{if eq .S.Theme "light"}} selected{{end}}>Light</option>
            <option value="dark"{{if eq .S.Theme "dark"}} selected{{end}}>Dark</option>
            <option value="system"{{if eq .S.Theme "system"}} selected{{end}}>Match Windows</option>
          </select>
        </div>
        <div class="toggle">
          <div class="label">Compact mode<span>Reduce spacing and use a tighter layout.</span></div>
          <label class="switch"><input type="hidden" name="compact" value="0"><input type="checkbox" name="compact" value="1" data-autosubmit{{if .S.Compact}} checked{{end}}><span class="slider"></span></label>
        </div>
      </form>
    </div>
  </section>

  <section class="card">
    <div class="card-head"><div class="card-title-row"><span class="mark shield">{{icon "alert"}}</span><div><h2>Notifications</h2><p>What FlipAi shows while this window is open.</p></div></div></div>
    <div class="card-body">
      <form method="post" action="/settings/save">
        <div class="toggle">
          <div class="label">Error alerts<span>Show a banner in this window when a new error reaches the activity log.</span></div>
          <label class="switch"><input type="hidden" name="alerts" value="0"><input type="checkbox" name="alerts" value="1" data-autosubmit{{if .S.Alerts}} checked{{end}}><span class="slider"></span></label>
        </div>
        <div class="toggle">
          <div class="label">Alert sound<span>Play a short tone with each alert banner.</span></div>
          <label class="switch"><input type="hidden" name="alertSound" value="0"><input type="checkbox" name="alertSound" value="1" data-autosubmit{{if .S.AlertSound}} checked{{end}}><span class="slider"></span></label>
        </div>
        <p class="hint">Texts to your phone are configured on the Phone page.</p>
      </form>
    </div>
  </section>

  <section class="card">
    <div class="card-head divided"><div class="card-title-row"><span class="mark shield">{{icon "folder"}}</span><div><h2>This install</h2><p>Where FlipAi keeps its files on this PC, and how to start over.</p></div></div></div>
    <div class="card-body">
      <div class="rows">
        <div class="row"><div class="label">Data folder<span>{{.S.DataDir}}</span></div><div class="value"><a class="btn small" href="/open/folder?which=data">{{icon "folder"}}Open</a></div></div>
        <div class="row">
          <div class="label">Reset setup<span>Clears Gmail credentials, the allowlist, the security code, and agent settings on this PC.</span></div>
          <div class="value">
            <form method="post" action="/settings/reset" data-confirm="Reset FlipAi to its default settings? Gmail credentials, allowed numbers, and the security code will be removed from this PC.">
              <button class="btn small danger" type="submit">{{icon "refresh"}}Reset</button>
            </form>
          </div>
        </div>
      </div>
      <p class="hint">Exporting the activity log is on <a href="/activity">Activity</a>.</p>
    </div>
  </section>
</div>

<div class="section-label">Message routing</div>
<section class="card">
  <div class="card-head divided"><div><h2>Shared routing</h2><p>The few things that are not one agent's own. Allowed numbers, security codes and instructions live with their agent on <a href="/agents">Agents</a>.</p></div></div>
  <div class="card-body">
    <form method="post" action="/agents/save">
      <input type="hidden" name="back" value="/settings">
      <div class="grid-3">
        <div class="field">
          <label for="newSessionCommand">New-conversation keyword</label>
          <input id="newSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
          <p class="hint">Works as <b>{{.S.CodexPrefix}} {{.S.NewSessionCommand}}</b>, <b>{{.S.ClaudePrefix}} {{.S.NewSessionCommand}}</b>, or on its own.</p>
        </div>
        <div class="field">
          <label for="turnTimeout">Turn timeout</label>
          <div class="input-suffix"><input id="turnTimeout" type="number" name="turnTimeout" min="1" max="600" value="{{.S.TurnTimeout}}"><span class="unit">min</span></div>
          <p class="hint">Maximum time any one agent turn may run.</p>
        </div>
        <div class="field">
          <label for="replyMaxChars">Reply size</label>
          <div class="input-suffix"><input id="replyMaxChars" type="number" name="replyMaxChars" min="120" max="1600" value="{{.S.ReplyMaxChars}}"><span class="unit">chars</span></div>
          <p class="hint">Longer answers are split into numbered texts.</p>
        </div>
      </div>
      <div class="field">
        <label for="cwd">Fallback working folder</label>
        <div class="input-group">
          <input id="cwd" type="text" name="cwd" value="{{.S.Cwd}}">
          <button class="btn" type="button" data-browse="#cwd">Browse</button>
        </div>
        <p class="hint">Used only by an agent that has no folder of its own.</p>
      </div>
      <div class="form-actions"><button class="btn primary" type="submit">Save routing</button></div>
    </form>
  </div>
</section>

<div class="section-label">The local service</div>
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

<p class="callout">Executable paths, permission modes, SMS shortcuts, allowed phone numbers, security codes and the instruction sent with every text all belong to one agent, so they live with Codex or Claude on the <a href="/agents">Agents</a> page. Gmail and Google Voice live on <a href="/connections">Connections</a>.</p>
{{end}}`

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	view := settingsView{pageView: pageView{Shell: a.shell(r, "settings", "Settings"), S: s}}
	startupValue, startupTone := "Disabled", ""
	startupSub := "Starts with this Windows user"
	if s.StartupEnabled {
		startupValue, startupTone = "At sign-in", "ok"
	}
	if s.BootStartupEnabled {
		startupValue, startupTone = "At power-on", "ok"
		startupSub = "Runs before anyone signs in"
	}
	view.LastError, view.HasError = lastError(a.recentEvents(200))
	view.Health = []healthRow{
		{Label: "Gmail backend", Value: readyText(s.GmailReady, "Connected", "Not connected"), Tone: toneClass(s.GmailReady)},
		{Label: "SMS processing", Value: readyText(s.Running && !s.Paused, "Active", pausedOrStopped(s)), Tone: toneClass(s.Running && !s.Paused)},
		{Label: "Codex executable", Value: readyText(s.CodexFound, "Found", "Missing"), Tone: toneClass(s.CodexFound)},
		{Label: "Claude executable", Value: readyText(s.ClaudeFound, "Found", "Missing"), Tone: toneClass(s.ClaudeFound)},
	}
	view.Healthy = s.GmailReady && s.Running && !s.Paused
	view.Tiles = []tileView{
		{Icon: "cpu", Title: "App version", Value: "v" + s.Version, Tone: "brand", Sub: updateSub(s)},
		{Icon: "power", Title: "Startup", Value: startupValue, Tone: startupTone, Sub: startupSub},
		{Icon: "sliders", Title: "Theme", Value: themeLabel(s.Theme), Tone: "", Sub: compactLabel(s.Compact)},
		{Icon: "folder", Title: "Data location", Value: shortPath(s.DataDir), Tone: "", Sub: "Settings, logs, and tokens"},
	}
	a.render(w, "settings", view)
}

func updateSub(s uiStatus) string {
	switch {
	case s.Update.Newer():
		return "v" + s.Update.Version + " available"
	case s.Update.Version != "":
		return "Up to date"
	default:
		return "Local Windows build"
	}
}

func themeLabel(theme string) string {
	switch theme {
	case ThemeDark:
		return "Dark"
	case ThemeSystem:
		return "Match Windows"
	default:
		return "Light"
	}
}

func compactLabel(compact bool) string {
	if compact {
		return "Compact layout"
	}
	return "Comfortable layout"
}
