package main

import (
	"net/http"
	"strings"
)

// The Agents page is intentionally registered again after ui_pages.go. Go's
// build system presents package files in lexical filename order, so this file's
// init runs after the original page registration and replaces only the two
// templates below. Keeping the handlers and view models unchanged makes this a
// presentation-only reorganization except for the explicit conversation-reset
// action.
func init() {
	registerPage("agents", organizedAgentsHTML)
	registerPage("advanced", organizedAdvancedHTML)
}

// organizedAgentsHTML keeps everything that belongs to one agent inside that
// agent's card. The final Shared Behavior card contains only settings that truly
// apply to both agents.
const organizedAgentsHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Agents</h1>
    <p>Each agent now keeps its shortcuts, workspace, conversation, access, tools, and installation settings together.</p>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<section class="card" id="codex">
  <form method="post" action="/agents/save">
    <div class="card-head divided">
      <div class="card-title-row">
        <span class="bmark lg codex">{{brand "codex"}}</span>
        <div>
          <h2>Codex
            <span class="pill {{if .S.CodexCheck.OK}}ok{{else}}warn{{end}}">{{if .S.CodexCheck.OK}}Ready{{else if .S.CodexCheck.Known}}Needs attention{{else}}Not tested{{end}}</span>
            {{if eq .S.DefaultAgent "C"}}<span class="pill brand">Default</span>{{end}}
          </h2>
          <p>Everything FlipAi uses to route, resume, and launch Codex is managed here.</p>
        </div>
      </div>
      <div class="head-actions">
        <a class="btn accent" href="/codex/test">{{icon "play"}}Test Codex</a>
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
      <details class="disclosure" open>
        <summary>Routing &amp; workspace</summary>
        <div class="disclosure-body">
          <div class="grid-2">
            <div class="field">
              <label for="codexPrefix">SMS shortcut</label>
              <input id="codexPrefix" type="text" name="codexPrefix" value="{{.S.CodexPrefix}}" maxlength="24" required>
              <p class="hint">Start a text with <b>{{.S.CodexPrefix}}:</b> to send it to Codex. Letters or numbers are fine.</p>
            </div>
            <div class="field">
              <label for="codexCwd">Working folder</label>
              <div class="input-group">
                <input id="codexCwd" type="text" name="codexCwd" value="{{.S.CodexCwd}}">
                <button class="btn" type="button" data-browse="#codexCwd">Browse</button>
              </div>
              <p class="hint">{{if .S.CodexCwdOK}}Folder found on this PC.{{else}}This folder does not exist yet.{{end}}</p>
            </div>
          </div>
        </div>
      </details>

      <details class="disclosure" open>
        <summary>Conversation &amp; new chat</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row">
              <div class="label">Current conversation<span>The SMS conversation FlipAi resumes on the next Codex request.</span></div>
              <div class="value"><b>{{if .S.CodexThreadActive}}Thread active{{else}}No thread yet{{end}}</b>{{if .S.CodexThreadActive}}<span class="pill ok">Continuing</span>{{end}}</div>
            </div>
            <div class="row">
              <div class="label">Start a new chat<span>Send this by SMS when you want a clean Codex conversation.</span></div>
              <div class="value"><b class="mono">{{.S.CodexPrefix}} {{.S.NewSessionCommand}}</b><span>Also accepts {{.S.CodexPrefix}}: {{.S.NewSessionCommand}}</span></div>
            </div>
            <div class="row">
              <div class="label">Reset current chat<span>Immediately forget the saved Codex thread. FlipAi restarts the bridge and creates a clean conversation.</span></div>
              <div class="value">
                {{if .S.CodexThreadActive}}
                <button class="btn small danger" type="submit" formaction="/agents/reset" name="agent" value="C" data-confirm="Reset the current Codex conversation? The next Codex work will use a clean chat.">{{icon "refresh"}}Reset chat</button>
                {{else}}<span class="pill">Nothing to reset</span>{{end}}
              </div>
            </div>
          </div>
        </div>
      </details>

      <details class="disclosure">
        <summary>Access &amp; installation</summary>
        <div class="disclosure-body">
          <div class="field">
            <label for="codexPath">Executable path</label>
            <input id="codexPath" type="text" name="codexPath" value="{{.S.CodexPath}}" placeholder="codex">
            <p class="hint">{{if .S.CodexFound}}Resolves to {{.S.CodexResolved}}{{else}}Not found — leave blank to search the usual install locations.{{end}}</p>
          </div>
          <div class="rows">
            <div class="row"><div class="label">Access level<span>SMS turns run with this Windows user's normal permissions, with no Codex sandbox and no elevation.</span></div><div class="value"><b>Full user access</b></div></div>
            <div class="row"><div class="label">Desktop history<span>FlipAi releases the thread after each turn so Codex Desktop can open the same history.</span></div><div class="value"><b>{{if .S.CodexThreadActive}}Available{{else}}After the first turn{{end}}</b></div></div>
            <div class="row"><div class="label">Last test</div><div class="value"><b>{{if .S.CodexCheck.Known}}{{ago .S.CodexCheck.At}}{{else}}Never{{end}}</b>{{if .S.CodexCheck.Detail}}<span>{{.S.CodexCheck.Detail}}</span>{{end}}</div></div>
          </div>
        </div>
      </details>

      <div class="form-actions"><button class="btn primary" type="submit">Save Codex settings</button></div>
    </div>
  </form>
</section>

<section class="card" id="claude">
  <form method="post" action="/agents/save">
    <div class="card-head divided">
      <div class="card-title-row">
        <span class="bmark lg claude">{{brand "claude"}}</span>
        <div>
          <h2>Claude
            <span class="pill {{if .S.ClaudeCheck.OK}}ok{{else}}warn{{end}}">{{if .S.ClaudeCheck.OK}}Ready{{else if .S.ClaudeCheck.Known}}Needs attention{{else}}Not tested{{end}}</span>
            {{if eq .S.DefaultAgent "A"}}<span class="pill brand">Default</span>{{end}}
          </h2>
          <p>Everything FlipAi uses to route, resume, authorize, and launch Claude is managed here.</p>
        </div>
      </div>
      <div class="head-actions">
        <a class="btn accent" href="/claude/test">{{icon "play"}}Test Claude</a>
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
      <details class="disclosure" open>
        <summary>Routing &amp; workspace</summary>
        <div class="disclosure-body">
          <div class="grid-2">
            <div class="field">
              <label for="claudePrefix">SMS shortcut</label>
              <input id="claudePrefix" type="text" name="claudePrefix" value="{{.S.ClaudePrefix}}" maxlength="24" required>
              <p class="hint">Start a text with <b>{{.S.ClaudePrefix}}:</b> to send it to Claude. It must differ from the Codex shortcut.</p>
            </div>
            <div class="field">
              <label for="claudeCwd">Working folder</label>
              <div class="input-group">
                <input id="claudeCwd" type="text" name="claudeCwd" value="{{.S.ClaudeCwd}}">
                <button class="btn" type="button" data-browse="#claudeCwd">Browse</button>
              </div>
              <p class="hint">{{if .S.ClaudeCwdOK}}Folder found on this PC.{{else}}This folder does not exist yet.{{end}}</p>
            </div>
          </div>
        </div>
      </details>

      <details class="disclosure" open>
        <summary>Conversation &amp; new chat</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row">
              <div class="label">Current conversation<span>The Claude Code session FlipAi resumes on the next request.</span></div>
              <div class="value"><b>{{if .S.ClaudeSessionActive}}Session active{{else}}No session yet{{end}}</b>{{if .S.ClaudeSessionName}}<span>Named “{{.S.ClaudeSessionName}}”.</span>{{end}}</div>
            </div>
            <div class="row">
              <div class="label">Start a new chat<span>Send this by SMS when you want a clean Claude conversation.</span></div>
              <div class="value"><b class="mono">{{.S.ClaudePrefix}} {{.S.NewSessionCommand}}</b><span>Also accepts {{.S.ClaudePrefix}}: {{.S.NewSessionCommand}}</span></div>
            </div>
            {{if .S.ClaudeSessionActive}}
            <div class="row"><div class="label">Resume from Claude Code<span>Open this exact SMS session from any folder on a supported Claude Code version.</span></div><div class="value"><b class="mono">claude --resume {{.S.ClaudeSessionID}}</b></div></div>
            <div class="row"><div class="label">Move to Claude Desktop<span>Resume it in Claude Code, then type this command to move the conversation into Desktop.</span></div><div class="value"><b class="mono">/desktop</b></div></div>
            {{end}}
            <div class="row">
              <div class="label">Reset current chat<span>Forget the saved Claude session and prepare a clean named session for the next Claude message.</span></div>
              <div class="value">
                {{if or .S.ClaudeSessionActive .S.ClaudeSessionName}}
                <button class="btn small danger" type="submit" formaction="/agents/reset" name="agent" value="A" data-confirm="Reset the current Claude conversation? The next Claude work will use a clean chat.">{{icon "refresh"}}Reset chat</button>
                {{else}}<span class="pill">Nothing to reset</span>{{end}}
              </div>
            </div>
          </div>
        </div>
      </details>

      <details class="disclosure" open>
        <summary>Access &amp; tools</summary>
        <div class="disclosure-body">
          <div class="field">
            <label for="permissionMode">Permission mode</label>
            <select id="permissionMode" name="permissionMode">
              <option value="bypassPermissions"{{if eq .S.PermissionMode "bypassPermissions"}} selected{{end}}>Full user access (matches Codex)</option>
              <option value="dontAsk"{{if eq .S.PermissionMode "dontAsk"}} selected{{end}}>Never prompt (your Claude rules decide)</option>
              <option value="acceptEdits"{{if eq .S.PermissionMode "acceptEdits"}} selected{{end}}>Accept edits only (blocks Chrome)</option>
              <option value="plan"{{if eq .S.PermissionMode "plan"}} selected{{end}}>Plan only</option>
              <option value="default"{{if eq .S.PermissionMode "default"}} selected{{end}}>Ask (blocks unattended turns)</option>
            </select>
            <p class="hint">SMS turns are unattended. Any mode that waits for approval can block work; Full user access is the closest match to Codex.</p>
          </div>
          <div class="toggle">
            <div class="label">Let Claude control Chrome<span>Passes --chrome so SMS requests can use the browser when Claude's permission mode allows it.</span></div>
            <label class="switch"><input type="hidden" name="claudeUseChrome" value="0"><input type="checkbox" name="claudeUseChrome" value="1"{{if .S.ClaudeUseChrome}} checked{{end}}><span class="slider"></span></label>
          </div>
          <div class="rows">
            <div class="row"><div class="label">Effective access level<span>What unattended Claude turns currently receive.</span></div><div class="value"><b>{{.S.PermissionModeLabel}}</b></div></div>
          </div>
        </div>
      </details>

      <details class="disclosure">
        <summary>Authentication &amp; installation</summary>
        <div class="disclosure-body">
          <div class="grid-2">
            <div class="field">
              <label for="claudePath">Executable path</label>
              <input id="claudePath" type="text" name="claudePath" value="{{.S.ClaudePath}}" placeholder="claude">
              <p class="hint">{{if .S.ClaudeFound}}Resolves to {{.S.ClaudeResolved}}{{else}}Not found — leave blank to search the usual install locations.{{end}}</p>
            </div>
            <div class="field">
              <label for="claudeToken">Long-lived token{{if .S.HasClaudeToken}} — saved{{end}}</label>
              <input id="claudeToken" type="password" name="claudeToken" autocomplete="off" placeholder="{{if .S.HasClaudeToken}}leave blank to keep the saved token{{else}}paste the value from claude setup-token{{end}}">
              <p class="hint">Optional. Stored with Windows DPAPI and used when the normal Claude Code browser login is not long-lived enough.</p>
            </div>
          </div>
          {{if .S.HasClaudeToken}}
          <div class="toggle">
            <div class="label">Remove saved token<span>Return to the normal Claude Code CLI login.</span></div>
            <label class="switch"><input type="hidden" name="clearClaudeToken" value="0"><input type="checkbox" name="clearClaudeToken" value="1"><span class="slider"></span></label>
          </div>
          {{end}}
          <div class="rows">
            <div class="row"><div class="label">Last test</div><div class="value"><b>{{if .S.ClaudeCheck.Known}}{{ago .S.ClaudeCheck.At}}{{else}}Never{{end}}</b>{{if .S.ClaudeCheck.Detail}}<span>{{.S.ClaudeCheck.Detail}}</span>{{end}}</div></div>
          </div>
        </div>
      </details>

      <div class="form-actions"><button class="btn primary" type="submit">Save Claude settings</button></div>
    </div>
  </form>
</section>

<section class="card" id="shared-agent-behavior">
  <form method="post" action="/agents/save">
    <div class="card-head">
      <div class="card-title-row">
        <span class="mark shield">{{icon "sliders"}}</span>
        <div><h2>Shared Behavior defaults</h2><p>Only settings that genuinely apply to both agents live here. All agent-specific controls are above.</p></div>
      </div>
    </div>
    <div class="card-body">
      <div class="grid-3">
        <div class="field">
          <label for="defaultAgent">Default agent</label>
          <select id="defaultAgent" name="defaultAgent">
            <option value="C"{{if eq .S.DefaultAgent "C"}} selected{{end}}>Codex</option>
            <option value="A"{{if eq .S.DefaultAgent "A"}} selected{{end}}>Claude</option>
          </select>
          <p class="hint">Used when a text has no agent shortcut in front of it.</p>
        </div>
        <div class="field">
          <label for="newSessionCommand">New-chat keyword</label>
          <input id="newSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
          <p class="hint">Changes the keyword in both <b>{{.S.CodexPrefix}} {{.S.NewSessionCommand}}</b> and <b>{{.S.ClaudePrefix}} {{.S.NewSessionCommand}}</b>.</p>
        </div>
        <div class="field">
          <label for="turnTimeout">Turn timeout</label>
          <div class="input-suffix"><input id="turnTimeout" type="number" name="turnTimeout" min="1" max="600" value="{{.S.TurnTimeout}}"><span class="unit">min</span></div>
          <p class="hint">Maximum time any one agent turn may run.</p>
        </div>
      </div>
      <div class="field">
        <label for="cwd">Shared fallback folder</label>
        <div class="input-group">
          <input id="cwd" type="text" name="cwd" value="{{.S.Cwd}}">
          <button class="btn" type="button" data-browse="#cwd">Browse</button>
        </div>
        <p class="hint">Used only when an agent does not have its own working folder above.</p>
      </div>
      <div class="form-actions"><button class="btn primary" type="submit">Save shared defaults</button></div>
    </div>
  </form>
</section>
{{end}}`

// organizedAdvancedHTML deliberately contains no editable agent paths, access,
// session, or shortcut controls. Advanced is now reserved for FlipAi itself.
const organizedAdvancedHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Advanced</h1>
    <p>Local service diagnostics and troubleshooting tools for FlipAi itself.</p>
  </div>
</div>

<section class="card">
  <div class="card-head divided">
    <div class="card-title-row">
      <span class="mark shield">{{icon "agent"}}</span>
      <div><h2>Agent settings moved</h2><p>Executable paths, access, sessions, new-chat controls, and SMS shortcuts now live with Codex or Claude on the Agents page.</p></div>
    </div>
    <div class="head-actions"><a class="btn accent" href="/agents">{{icon "agent"}}Open Agents</a></div>
  </div>
</section>

<div class="cards-2">
  <section class="card">
    <div class="card-head divided"><div class="card-title-row"><span class="mark shield">{{icon "server"}}</span><div><h2>Local service</h2><p>The loopback control server this window is talking to.</p></div></div></div>
    <div class="card-body">
      <div class="rows">
        <div class="row"><div class="label">Loopback address</div><div class="value"><b class="mono">http://{{.S.Listen}}</b><span class="pill ok">Listening</span></div></div>
        <div class="row"><div class="label">Session token<span>Pages are only served to windows FlipAi opened itself.</span></div><div class="value"><b>Active</b><span class="pill ok">Valid</span></div></div>
        <div class="row"><div class="label">Gmail backend</div><div class="value"><b class="{{if .S.GmailReady}}ok{{else}}warn{{end}}">{{if .S.GmailReady}}Connected{{else}}Not connected{{end}}</b></div></div>
        <div class="row"><div class="label">SMS processing</div><div class="value"><b class="{{if and .S.Running (not .S.Paused)}}ok{{else}}warn{{end}}">{{if .S.Paused}}Paused{{else if .S.Running}}Active{{else}}Waiting for setup{{end}}</b></div></div>
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

// resetAgentConversation provides the Agents-page reset button. It refuses to
// interrupt a turn that is currently running, clears only the selected agent's
// saved conversation, and restarts the bridge so the in-memory state cannot
// continue using the old id.
func (a *App) resetAgentConversation(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, http.StatusBadRequest, false, "Could not read the reset request", err.Error())
		return
	}

	a.mu.Lock()
	b := a.bridge
	a.mu.Unlock()
	if b != nil && b.Busy() {
		renderResult(w, r, http.StatusConflict, false, "Agent is busy", "Wait for the current agent turn to finish before resetting its conversation.")
		return
	}

	agent := strings.ToUpper(strings.TrimSpace(r.FormValue("agent")))
	s := loadState(a.statePath)
	var label string
	switch agent {
	case "C":
		label = "Codex"
		s.CodexThreadID = ""
	case "A":
		label = "Claude"
		s.ClaudeSessionID = ""
		s.ClaudeSessionName = ""
	default:
		renderResult(w, r, http.StatusBadRequest, false, "Unknown agent", "Choose Codex or Claude from the Agents page.")
		return
	}

	if err := saveState(a.statePath, s); err != nil {
		renderResult(w, r, http.StatusInternalServerError, false, "Could not reset the conversation", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "agent", label+" conversation reset from the Agents page", "", agent, "")
	redirectTo(w, r, "/agents", "saved-restart")
	go a.restartSoon()
}
