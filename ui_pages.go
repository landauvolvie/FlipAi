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
`

func registerUIPages() {
	registerPage("home", homeHTML)
	registerPage("connections", connectionsHTML)
	registerPage("agents", agentsHTML)
	registerPage("phone", phoneHTML)
	registerPage("activity", activityPageHTML)
	registerPage("settings", settingsHTML)
	registerPage("advanced", advancedHTML)
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
    <a class="btn small" href="/logs/export">{{icon "download"}}Export logs</a>
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
    <a class="btn accent" href="/gmail/test">{{icon "send"}}Test Gmail</a>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

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
      <div class="row"><div class="label">Authentication method</div><div class="value"><b>{{.S.GmailMethodLabel}}</b>{{if .S.GmailReady}}<span class="pill ok">Valid</span>{{else if .S.GmailMethod}}<span class="pill warn">Incomplete</span>{{else}}<span class="pill">Not selected</span>{{end}}</div></div>
      <div class="row"><div class="label">Google account</div><div class="value"><b>{{if .S.GmailEmail}}{{.S.GmailEmail}}{{else}}Signed in with OAuth{{end}}</b></div></div>
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
              <a class="btn" href="/gmail/test">Test Gmail</a>
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
      <span class="mark shield">{{icon "shield"}}</span>
      <div><h2>Inbound sender settings</h2><p>Control who can message your agents and how replies are sent.</p></div>
    </div>
    <div class="head-actions"><form method="post" action="/connections/flowtest"><button class="btn accent" type="submit">{{icon "send"}}Test message flow</button></form></div>
  </div>
  <div class="card-body">
    <div class="rows">
      <div class="row"><div class="label">Allowed phone numbers</div><div class="value"><b>{{if .S.AllowedCount}}{{.S.AllowedCount}} allowed{{else}}None yet{{end}}</b><a class="linky" href="/phone">Manage</a></div></div>
      <div class="row"><div class="label">SMS security code</div><div class="value"><b>{{if .S.RequireCode}}Required{{else}}Off{{end}}</b><a class="linky" href="/phone">Manage</a></div></div>
      <div class="row"><div class="label">Subject phrase matching</div><div class="value"><b>{{if .S.SubjectPhrase}}{{.S.SubjectPhrase}}{{else}}Off{{end}}</b>{{if .S.SubjectPhrase}}<span class="pill ok">Enabled</span>{{end}}</div></div>
      <div class="row"><div class="label">Status</div><div class="value"><b>{{if .S.AllowedCount}}Filtering active{{else}}No senders allowed{{end}}</b><span>{{if .S.AllowedCount}}Only allowed numbers can reach your agents{{else}}Add a number before texting{{end}}</span></div></div>
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
	filterValue, filterTone := "No senders allowed", "warn"
	if s.AllowedCount > 0 {
		filterValue, filterTone = "Active", "ok"
	}
	view.Tiles = []tileView{
		{Brand: "google", Title: "Gmail", Value: gmailValue, Tone: gmailTone, Sub: s.GmailMethodLabel, Check: checkTone(s.GmailReady)},
		{Brand: "voice", Title: "Voice reply", Value: replyValue, Tone: replyTone, Sub: "Replies go to the sender's Voice thread", Check: checkTone(s.GmailReady)},
		{Icon: "shield", Title: "Sender filter", Value: filterValue, Tone: filterTone, Sub: plural(s.AllowedCount, "number") + " allowed", Check: checkTone(s.AllowedCount > 0)},
		{Icon: "clock", Title: "Last sync", Value: view.LastSync, Tone: "", Sub: mailboxSub(s), Check: checkTone(s.LastPollErr == "" && !s.LastPollAt.IsZero())},
	}
	a.render(w, "connections", view)
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
// Agents
// ---------------------------------------------------------------------------

type agentsView struct {
	pageView
	Tiles []tileView
}

const agentsHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Agents</h1>
    <p>Configure your local AI agents and how a text turns into a turn of work.</p>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<section class="card">
  <form method="post" action="/agents/save">
    <div class="card-head">
      <div class="card-title-row">
        <span class="bmark lg codex">{{brand "codex"}}</span>
        <div>
          <h2>Codex
            <span class="pill {{if .S.CodexCheck.OK}}ok{{else}}warn{{end}}">{{if .S.CodexCheck.OK}}Ready{{else if .S.CodexCheck.Known}}Needs attention{{else}}Not tested{{end}}</span>
            {{if eq .S.DefaultAgent "C"}}<span class="pill brand">Default</span>{{end}}
          </h2>
          <p>Local Codex CLI signed in with ChatGPT. Handles <b>C:</b> messages.</p>
        </div>
      </div>
      <div class="head-actions">
        <a class="btn accent" href="/codex/test">{{icon "play"}}Test</a>
        <div class="menu">
          <button class="btn icon" type="button" data-menu-trigger aria-label="More Codex actions">{{icon "more"}}</button>
          <div class="menu-panel">
            <button type="submit" name="defaultAgent" value="C">{{icon "check"}}Set as default agent</button>
            <a href="/open/folder?which=codex">{{icon "folder"}}Open working folder</a>
            <a href="/activity?stage=agent">{{icon "clock"}}View agent activity</a>
          </div>
        </div>
      </div>
    </div>
    <div class="card-body">
      <div class="grid-2">
        <div class="field">
          <label for="codexCwd">Working folder</label>
          <div class="input-group">
            <input id="codexCwd" type="text" name="codexCwd" value="{{.S.CodexCwd}}">
            <button class="btn" type="button" data-browse="#codexCwd">Browse</button>
          </div>
          <p class="hint">{{if .S.CodexCwdOK}}Folder found on this PC.{{else}}This folder does not exist yet.{{end}}</p>
        </div>
        <div class="field">
          <label for="codexPath">Executable path</label>
          <input id="codexPath" type="text" name="codexPath" value="{{.S.CodexPath}}" placeholder="codex">
          <p class="hint">{{if .S.CodexFound}}Resolves to {{.S.CodexResolved}}{{else}}Not found — leave blank to search the usual install locations.{{end}}</p>
        </div>
      </div>
      <details class="disclosure">
        <summary>Advanced</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row"><div class="label">Access level<span>SMS turns run with this Windows user's normal permissions, with no Codex sandbox and no elevation.</span></div><div class="value"><b>Full user access</b></div></div>
            <div class="row"><div class="label">Conversation<span>FlipAi releases the thread after each turn so Codex Desktop can open the same history.</span></div><div class="value"><b>{{if .S.CodexThreadActive}}Thread active{{else}}No thread yet{{end}}</b></div></div>
            <div class="row"><div class="label">Last test</div><div class="value"><b>{{if .S.CodexCheck.Known}}{{ago .S.CodexCheck.At}}{{else}}Never{{end}}</b>{{if .S.CodexCheck.Detail}}<span>{{.S.CodexCheck.Detail}}</span>{{end}}</div></div>
          </div>
        </div>
      </details>
      <div class="form-actions"><button class="btn primary" type="submit">Save Codex settings</button></div>
    </div>
  </form>
</section>

<section class="card">
  <form method="post" action="/agents/save">
    <div class="card-head">
      <div class="card-title-row">
        <span class="bmark lg claude">{{brand "claude"}}</span>
        <div>
          <h2>Claude
            <span class="pill {{if .S.ClaudeCheck.OK}}ok{{else}}warn{{end}}">{{if .S.ClaudeCheck.OK}}Ready{{else if .S.ClaudeCheck.Known}}Needs attention{{else}}Not tested{{end}}</span>
            {{if eq .S.DefaultAgent "A"}}<span class="pill brand">Default</span>{{end}}
          </h2>
          <p>Claude Code CLI under your Claude subscription. Handles <b>A:</b> messages.</p>
        </div>
      </div>
      <div class="head-actions">
        <a class="btn accent" href="/claude/test">{{icon "play"}}Test</a>
        <div class="menu">
          <button class="btn icon" type="button" data-menu-trigger aria-label="More Claude actions">{{icon "more"}}</button>
          <div class="menu-panel">
            <button type="submit" name="defaultAgent" value="A">{{icon "check"}}Set as default agent</button>
            <a href="/open/folder?which=claude">{{icon "folder"}}Open working folder</a>
            <a href="/activity?stage=agent">{{icon "clock"}}View agent activity</a>
          </div>
        </div>
      </div>
    </div>
    <div class="card-body">
      <div class="grid-2">
        <div class="field">
          <label for="claudeCwd">Working folder</label>
          <div class="input-group">
            <input id="claudeCwd" type="text" name="claudeCwd" value="{{.S.ClaudeCwd}}">
            <button class="btn" type="button" data-browse="#claudeCwd">Browse</button>
          </div>
          <p class="hint">{{if .S.ClaudeCwdOK}}Folder found on this PC.{{else}}This folder does not exist yet.{{end}}</p>
        </div>
        <div class="field">
          <label for="claudePath">Executable path</label>
          <input id="claudePath" type="text" name="claudePath" value="{{.S.ClaudePath}}" placeholder="claude">
          <p class="hint">{{if .S.ClaudeFound}}Resolves to {{.S.ClaudeResolved}}{{else}}Not found — leave blank to search the usual install locations.{{end}}</p>
        </div>
      </div>
      <div class="toggle">
        <div class="label">Let Claude control Chrome<span>Passes --chrome, so a text can use the browser exactly as Claude does at the desktop. Needs Claude permission mode set to full user access; a narrower mode refuses the browser tools.</span></div>
        <label class="switch"><input type="hidden" name="claudeUseChrome" value="0"><input type="checkbox" name="claudeUseChrome" value="1"{{if .S.ClaudeUseChrome}} checked{{end}}><span class="slider"></span></label>
      </div>
      <details class="disclosure">
        <summary>Advanced</summary>
        <div class="disclosure-body">
          <div class="field">
            <label for="claudeToken">Long-lived token{{if .S.HasClaudeToken}} — saved{{end}}</label>
            <input id="claudeToken" type="password" name="claudeToken" autocomplete="off" placeholder="{{if .S.HasClaudeToken}}leave blank to keep the saved token{{else}}paste the value from claude setup-token{{end}}">
            <p class="hint">Optional. The Claude Code browser session expires; running <b>claude setup-token</b> once and pasting the result keeps the bridge signed in for longer. Stored with Windows DPAPI.</p>
          </div>
          {{if .S.HasClaudeToken}}
          <div class="toggle">
            <div class="label">Remove the saved token<span>Go back to the normal Claude Code CLI login.</span></div>
            <label class="switch"><input type="hidden" name="clearClaudeToken" value="0"><input type="checkbox" name="clearClaudeToken" value="1"><span class="slider"></span></label>
          </div>
          {{end}}
          <div class="rows">
            <div class="row"><div class="label">Conversation</div><div class="value"><b>{{if .S.ClaudeSessionActive}}Session active{{else}}No session yet{{end}}</b>{{if .S.ClaudeSessionName}}<span>Named "{{.S.ClaudeSessionName}}".</span>{{end}}</div></div>
            {{if .S.ClaudeSessionActive}}
            <div class="row"><div class="label">Resume this conversation<span>Claude Code keeps SMS (<code>-p</code>) sessions out of the interactive /resume picker, so open it by id. This works from any folder on Claude Code 2.1.223 or newer.</span></div><div class="value"><b>claude --resume {{.S.ClaudeSessionID}}</b></div></div>
            <div class="row"><div class="label">Move it to Claude Desktop<span>Resume it as above, then type <b>/desktop</b>. Claude saves the session and opens it in the desktop app. Supported on Windows x64 and macOS with a Claude subscription. Claude Desktop keeps its own history, so it cannot list this conversation until you move it across.</span></div><div class="value"><b>/desktop</b></div></div>
            {{end}}
            <div class="row"><div class="label">Access level<span>SMS turns run with this Windows user's normal permissions and no elevation, the same as Codex.</span></div><div class="value"><b>{{.S.PermissionModeLabel}}</b></div></div>
            <div class="row"><div class="label">Last test</div><div class="value"><b>{{if .S.ClaudeCheck.Known}}{{ago .S.ClaudeCheck.At}}{{else}}Never{{end}}</b>{{if .S.ClaudeCheck.Detail}}<span>{{.S.ClaudeCheck.Detail}}</span>{{end}}</div></div>
          </div>
        </div>
      </details>
      <div class="form-actions"><button class="btn primary" type="submit">Save Claude settings</button></div>
    </div>
  </form>
</section>

<section class="card">
  <form method="post" action="/agents/save">
    <div class="card-head">
      <div><h2>Behavior</h2><p>Global routing and execution settings for both agents.</p></div>
    </div>
    <div class="card-body">
      <div class="grid-3">
        <div class="field">
          <label for="defaultAgent">Default agent</label>
          <select id="defaultAgent" name="defaultAgent">
            <option value="C"{{if eq .S.DefaultAgent "C"}} selected{{end}}>Codex</option>
            <option value="A"{{if eq .S.DefaultAgent "A"}} selected{{end}}>Claude</option>
          </select>
          <p class="hint">Used when a text has no {{.S.CodexPrefix}}: or {{.S.ClaudePrefix}}: prefix.</p>
        </div>
        <div class="field">
          <label for="turnTimeout">Turn timeout</label>
          <div class="input-suffix"><input id="turnTimeout" type="number" name="turnTimeout" min="1" max="600" value="{{.S.TurnTimeout}}"><span class="unit">min</span></div>
          <p class="hint">Maximum time one agent turn may run.</p>
        </div>
        <div class="field">
          <label for="permissionMode">Claude permission mode</label>
          <select id="permissionMode" name="permissionMode">
            <option value="bypassPermissions"{{if eq .S.PermissionMode "bypassPermissions"}} selected{{end}}>Full user access (matches Codex)</option>
            <option value="dontAsk"{{if eq .S.PermissionMode "dontAsk"}} selected{{end}}>Never prompt (your Claude rules decide)</option>
            <option value="acceptEdits"{{if eq .S.PermissionMode "acceptEdits"}} selected{{end}}>Accept edits only (blocks Chrome)</option>
            <option value="plan"{{if eq .S.PermissionMode "plan"}} selected{{end}}>Plan only</option>
            <option value="default"{{if eq .S.PermissionMode "default"}} selected{{end}}>Ask (blocks unattended turns)</option>
          </select>
          <p class="hint">Texting is unattended, so anything that asks for approval is refused. "Accept edits only" approves file edits and nothing else, so Chrome and other tools are blocked. "Full user access" is what Codex SMS turns already use.</p>
        </div>
      </div>
      <div class="grid-3">
        <div class="field">
          <label for="codexPrefix">Codex SMS prefix</label>
          <input id="codexPrefix" type="text" name="codexPrefix" value="{{.S.CodexPrefix}}" maxlength="24" required>
          <p class="hint">Example: <b>{{.S.CodexPrefix}}: check the latest build</b>. Letters or numbers are fine.</p>
        </div>
        <div class="field">
          <label for="claudePrefix">Claude SMS prefix</label>
          <input id="claudePrefix" type="text" name="claudePrefix" value="{{.S.ClaudePrefix}}" maxlength="24" required>
          <p class="hint">Example: <b>{{.S.ClaudePrefix}}: review this issue</b>. It must differ from the Codex prefix.</p>
        </div>
        <div class="field">
          <label for="newSessionCommand">New-session command</label>
          <input id="newSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
          <p class="hint">Use <b>{{.S.CodexPrefix}} {{.S.NewSessionCommand}}</b>, <b>{{.S.ClaudePrefix}} {{.S.NewSessionCommand}}</b>, or send it alone for the default agent.</p>
        </div>
      </div>
      <div class="field">
        <label for="cwd">Shared working folder</label>
        <div class="input-group">
          <input id="cwd" type="text" name="cwd" value="{{.S.Cwd}}">
          <button class="btn" type="button" data-browse="#cwd">Browse</button>
        </div>
        <p class="hint">Used by any agent that has no folder of its own.</p>
      </div>
      <div class="form-actions"><button class="btn primary" type="submit">Save behavior</button></div>
    </div>
  </form>
</section>
{{end}}`

func (a *App) agentsPage(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	codexValue, codexTone := checkLabel(s.CodexCheck, "Ready")
	claudeValue, claudeTone := checkLabel(s.ClaudeCheck, "Ready")
	view := agentsView{pageView: pageView{Shell: a.shell(r, "agents", "Agents"), S: s}}
	view.Tiles = []tileView{
		{Brand: "codex", Title: "Codex", Value: codexValue, Tone: codexTone, Sub: "Local Codex CLI",
			FootLabel: "Executable", FootValue: foundLabel(s.CodexFound), FootTone: codexTone},
		{Brand: "claude", Title: "Claude", Value: claudeValue, Tone: claudeTone, Sub: "Claude Code CLI",
			FootLabel: "Executable", FootValue: foundLabel(s.ClaudeFound), FootTone: claudeTone},
		{Icon: "cpu", Title: "Default agent", Value: s.DefaultAgentName, Tone: "brand", Sub: "Used without a " + s.CodexPrefix + ": or " + s.ClaudePrefix + ": prefix",
			FootLabel: "Turn timeout", FootValue: itoa(s.TurnTimeout) + " min"},
		{Icon: "folder", Title: "Working folders", Value: shortPath(s.CodexCwd), Tone: "", Sub: "Claude: " + shortPath(s.ClaudeCwd),
			FootLabel: "Accessible", FootValue: yesNo(s.CodexCwdOK && s.ClaudeCwdOK)},
	}
	a.render(w, "agents", view)
}

func foundLabel(ok bool) string {
	if ok {
		return "Found"
	}
	return "Not found"
}

func yesNo(ok bool) string {
	if ok {
		return "Yes"
	}
	return "No"
}

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

// ---------------------------------------------------------------------------
// Phone
// ---------------------------------------------------------------------------

type phoneView struct {
	pageView
	Tiles []tileView
}

const phoneHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Phone</h1>
    <p>Control incoming SMS access, reply behavior, and progress texts.</p>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<section class="card">
  <div class="card-head">
    <div><h2>Allowed numbers</h2><p>Only messages from these numbers are accepted. Every other sender is ignored.</p></div>
    <div class="head-actions"><button class="btn accent" type="button" data-reveal="#add-number">{{icon "plus"}}Add number</button></div>
  </div>
  <div id="add-number" class="card-body hidden">
    <form method="post" action="/phone/numbers/add">
      <div class="grid-2">
        <div class="field">
          <label for="number">Phone number</label>
          <input id="number" type="text" name="number" placeholder="(845) 555-1234" required>
          <p class="hint">10-digit US/Canada mobile number. A leading +1 is fine.</p>
        </div>
        <div class="field">
          <label for="label">Label</label>
          <input id="label" type="text" name="label" placeholder="My mobile" maxlength="40">
          <p class="hint">Optional, only shown in this list.</p>
        </div>
      </div>
      <div class="form-actions"><button class="btn primary" type="submit">Add to allowlist</button></div>
    </form>
  </div>
  <div class="table-wrap">
    <table>
      <thead><tr><th>Phone number</th><th>Label</th><th>Added</th><th>Status</th><th class="num">Actions</th></tr></thead>
      <tbody>
        {{range .S.AllowedNumbers}}
        <tr>
          <td><b>{{.Display}}</b></td>
          <td>{{if .Label}}{{.Label}}{{else}}—{{end}}</td>
          <td class="when">{{day .Added}}</td>
          <td><span class="pill ok">Allowed</span></td>
          <td class="num">
            <form method="post" action="/phone/numbers/remove" data-confirm="Remove {{.Display}} from the allowlist?">
              <input type="hidden" name="number" value="{{.Number}}">
              <button class="btn small danger" type="submit">{{icon "trash"}}Remove</button>
            </form>
          </td>
        </tr>
        {{else}}
        <tr><td colspan="5"><div class="empty"><b>No numbers yet</b>Add the mobile number you text from so FlipAi can accept it.</div></td></tr>
        {{end}}
      </tbody>
    </table>
  </div>
  <div class="table-foot"><span>Showing {{.S.AllowedCount}} of {{.S.AllowedCount}} number(s)</span></div>
</section>

<div class="cards-2">
  <section class="card">
    <form method="post" action="/phone/save">
      <div class="card-head"><div><h2>Reply behavior</h2><p>How FlipAi composes and sends replies.</p></div></div>
      <div class="card-body">
        <div class="field">
          <label for="replyMaxChars">Reply max characters</label>
          <input id="replyMaxChars" type="number" name="replyMaxChars" min="80" max="1000" value="{{.S.ReplyMaxChars}}">
          <p class="hint">Maximum characters per message part.</p>
        </div>
        <div class="field">
          <label for="maxReplyParts">Max reply parts</label>
          <select id="maxReplyParts" name="maxReplyParts">
            {{range $i := .Parts}}<option value="{{$i}}"{{if eq $i $.S.MaxReplyParts}} selected{{end}}>{{$i}}</option>{{end}}
          </select>
          <p class="hint">A longer answer is split into numbered texts instead of being cut off.</p>
        </div>
        <div class="toggle">
          <div class="label">Reply acknowledgement<span>Text a one-line confirmation as soon as a command is accepted.</span></div>
          <label class="switch"><input type="hidden" name="replyAck" value="0"><input type="checkbox" name="replyAck" value="1" data-autosubmit{{if .S.ReplyAck}} checked{{end}}><span class="slider"></span></label>
        </div>
        <div class="toggle">
          <div class="label">Progress updates<span>Text periodic updates while a long turn is still running.</span></div>
          <label class="switch"><input type="hidden" name="progressUpdates" value="0"><input type="checkbox" name="progressUpdates" value="1" data-autosubmit{{if .S.ProgressUpdates}} checked{{end}}><span class="slider"></span></label>
        </div>
        <div class="field">
          <label for="progressInterval">Progress interval</label>
          <div class="input-suffix"><input id="progressInterval" type="number" name="progressInterval" min="30" max="3600" value="{{.S.ProgressInterval}}"><span class="unit">sec</span></div>
          <p class="hint">Time between progress updates.</p>
        </div>
        <div class="form-actions"><button class="btn primary" type="submit">Save reply behavior</button></div>
      </div>
    </form>
  </section>

  <section class="card">
    <form method="post" action="/phone/security">
      <div class="card-head"><div><h2>Security code</h2><p>Require a private code at the start of every text.</p></div></div>
      <div class="card-body">
        <div class="toggle">
          <div class="label">Require code<span>Messages must start with the correct code. The number allowlist is enforced either way.</span></div>
          <label class="switch"><input type="hidden" name="requireCode" value="0"><input type="checkbox" name="requireCode" value="1" data-autosubmit{{if .S.RequireCode}} checked{{end}}><span class="slider"></span></label>
        </div>
        <div class="rows">
          <div class="row"><div class="label">Status</div><div class="value">{{if .S.RequireCode}}<span class="pill ok">Enabled</span>{{else}}<span class="pill">Off</span>{{end}}</div></div>
          <div class="row">
            <div class="label">Security code</div>
            <div class="value"><b class="mono">{{if .S.HasCode}}••••••{{else}}Not set{{end}}</b><button class="btn small" type="button" data-reveal="#change-code">Change code</button></div>
          </div>
        </div>
        <div id="change-code" class="hidden">
          <div class="field">
            <label for="securityCode">New security code</label>
            <input id="securityCode" type="password" name="securityCode" autocomplete="new-password" placeholder="At least 6 characters, no spaces">
            <p class="hint">Stored only as a salted, iterated hash. Example text: <b>482913 C: check GitHub</b>.</p>
          </div>
          <div class="form-actions"><button class="btn primary" type="submit">Save security code</button></div>
        </div>
      </div>
    </form>
  </section>
</div>
{{end}}`

func (a *App) phonePage(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	view := struct {
		phoneView
		Parts []int
	}{phoneView: phoneView{pageView: pageView{Shell: a.shell(r, "phone", "Phone"), S: s}}}
	for i := 1; i <= 10; i++ {
		view.Parts = append(view.Parts, i)
	}
	codeValue, codeTone := "Off", ""
	if s.RequireCode {
		codeValue, codeTone = "Enabled", "ok"
	}
	progressValue, progressTone := "Off", ""
	progressSub := "No updates during long turns"
	if s.ProgressUpdates {
		progressValue, progressTone = "Enabled", "ok"
		progressSub = "Every " + itoa(s.ProgressInterval) + " sec"
	}
	view.Tiles = []tileView{
		{Icon: "phone", Title: "Allowed numbers", Value: itoa(s.AllowedCount), Tone: "brand", Big: true, Sub: plural(s.AllowedCount, "number") + " allowed"},
		{Icon: "shield", Title: "Security code", Value: codeValue, Tone: codeTone, Big: true, Sub: securitySub(s)},
		{Icon: "send", Title: "Reply split limit", Value: itoa(s.MaxReplyParts) + " parts", Tone: "brand", Big: true, Sub: itoa(s.ReplyMaxChars) + " characters per part"},
		{Icon: "refresh", Title: "Progress updates", Value: progressValue, Tone: progressTone, Big: true, Sub: progressSub},
	}
	a.render(w, "phone", view)
}

func securitySub(s uiStatus) string {
	if !s.RequireCode {
		return "Allowlist only"
	}
	if s.HasCode {
		return "Active"
	}
	return "No code set yet"
}

// ---------------------------------------------------------------------------
// Activity
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
        <label for="updateCheckHours">Check for updates every</label>
        <select id="updateCheckHours" name="updateCheckHours" data-autosubmit>
          <option value="1"{{if eq .S.UpdateCheckHours 1}} selected{{end}}>Hour</option>
          <option value="6"{{if eq .S.UpdateCheckHours 6}} selected{{end}}>6 hours</option>
          <option value="12"{{if eq .S.UpdateCheckHours 12}} selected{{end}}>12 hours</option>
          <option value="24"{{if eq .S.UpdateCheckHours 24}} selected{{end}}>Day</option>
          <option value="168"{{if eq .S.UpdateCheckHours 168}} selected{{end}}>Week</option>
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
    <div class="card-head divided"><div class="card-title-row"><span class="mark shield">{{icon "wrench"}}</span><div><h2>Diagnostics</h2><p>Copies of local data for troubleshooting.</p></div></div></div>
    <div class="card-body">
      <div class="rows">
        <div class="row"><div class="label">Export logs<span>Save a zip of the activity log and bridge log.</span></div><div class="value"><a class="btn small" href="/logs/export">{{icon "download"}}Export</a></div></div>
        <div class="row"><div class="label">Open data folder<span>{{.S.DataDir}}</span></div><div class="value"><a class="btn small" href="/open/folder?which=data">{{icon "folder"}}Open</a></div></div>
        <div class="row">
          <div class="label">Reset setup<span>Clears Gmail credentials, the allowlist, the security code, and agent settings on this PC.</span></div>
          <div class="value">
            <form method="post" action="/settings/reset" data-confirm="Reset FlipAi to its default settings? Gmail credentials, allowed numbers, and the security code will be removed from this PC.">
              <button class="btn small danger" type="submit">{{icon "refresh"}}Reset</button>
            </form>
          </div>
        </div>
      </div>
    </div>
  </section>
</div>
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

// ---------------------------------------------------------------------------
// Advanced
// ---------------------------------------------------------------------------

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

const advancedHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Advanced</h1>
    <p>Technical paths, diagnostics, and troubleshooting tools.</p>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<section class="card">
  <form method="post" action="/agents/save">
    <div class="card-head"><div><h2>Executable paths</h2><p>Where FlipAi launches the local agent CLIs from.</p></div></div>
    <div class="card-body">
      <div class="field">
        <label for="advCodexPath">Codex path</label>
        <div class="input-suffix">
          <div class="input-group">
            <input id="advCodexPath" type="text" name="codexPath" value="{{.S.CodexPath}}" placeholder="codex">
            <button class="btn" type="button" data-browse="#advCodexPath">Browse</button>
          </div>
          <span class="check {{if .S.CodexFound}}ok{{else}}bad{{end}}">{{if .S.CodexFound}}{{icon "check"}}{{else}}{{icon "x-ring"}}{{end}}</span>
        </div>
        <p class="hint">{{if .S.CodexFound}}{{.S.CodexResolved}}{{else}}No Codex executable found at this path.{{end}}</p>
      </div>
      <div class="field">
        <label for="advClaudePath">Claude path</label>
        <div class="input-suffix">
          <div class="input-group">
            <input id="advClaudePath" type="text" name="claudePath" value="{{.S.ClaudePath}}" placeholder="claude">
            <button class="btn" type="button" data-browse="#advClaudePath">Browse</button>
          </div>
          <span class="check {{if .S.ClaudeFound}}ok{{else}}bad{{end}}">{{if .S.ClaudeFound}}{{icon "check"}}{{else}}{{icon "x-ring"}}{{end}}</span>
        </div>
        <p class="hint">{{if .S.ClaudeFound}}{{.S.ClaudeResolved}}{{else}}No Claude executable found at this path.{{end}}</p>
      </div>
      <div class="form-actions"><button class="btn primary" type="submit">Save paths</button></div>
    </div>
  </form>
</section>

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
      <div class="form-actions"><form method="post" action="/health/check"><button class="btn accent" type="submit">{{icon "check"}}Run health check</button></form></div>
    </div>
  </section>

  <section class="card">
    <div class="card-head divided"><div class="card-title-row"><span class="mark shield">{{icon "clock"}}</span><div><h2>Logs and troubleshooting</h2><p>Local log files for this Windows user.</p></div></div></div>
    <div class="card-body">
      <div class="rows">
        <div class="row"><div class="label">Export logs<span>Zip of activity.jsonl and bridge.log.</span></div><div class="value"><a class="btn small" href="/logs/export">{{icon "download"}}Export</a></div></div>
        <div class="row"><div class="label">Clear activity log</div><div class="value"><form method="post" action="/activity/clear" data-confirm="Clear the FlipAi activity history?"><button class="btn small danger" type="submit">{{icon "trash"}}Clear</button></form></div></div>
        <div class="row"><div class="label">Open logs folder<span>{{.S.DataDir}}</span></div><div class="value"><a class="btn small" href="/open/folder?which=logs">{{icon "folder"}}Open</a></div></div>
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
  <div class="card-head">
    <div class="card-title-row"><span class="mark shield">{{icon "wrench"}}</span><div><h2>Advanced tools</h2><p>Use with care. These actions restart local FlipAi processes.</p></div></div>
  </div>
  <div class="card-body">
    <div class="rows">
      <div class="row">
        <div class="label">Restart bridge<span>Reloads settings and reconnects Gmail and the agents. Texting pauses for a moment.</span></div>
        <div class="value"><form method="post" action="/bridge/restart"><button class="btn small" type="submit">{{icon "refresh"}}Restart</button></form></div>
      </div>
      <div class="row">
        <div class="label">Repair Windows startup entry<span>Rewrites this user's Run registry value to point at the current FlipAi executable.</span></div>
        <div class="value"><form method="post" action="/settings/startup"><input type="hidden" name="startup" value="1"><button class="btn small" type="submit">{{icon "wrench"}}Repair</button></form></div>
      </div>
      <div class="row">
        <div class="label">Quit FlipAi completely<span>Stops the window, the tray icon, the background host, and the watchdog.</span></div>
        <div class="value"><form method="post" action="/quit" data-confirm="Stop FlipAi completely? Texts will not be processed until you start it again."><button class="btn small danger" type="submit">{{icon "power"}}Quit</button></form></div>
      </div>
    </div>
  </div>
</section>
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
