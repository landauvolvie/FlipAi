package main

// This file deliberately registers the Agents and Advanced pages after
// ui_pages.go. Keeping the redesign here makes the ownership boundary obvious:
// agent-specific controls live on Agents, while Advanced remains app-level
// diagnostics only.
func init() {
	registerPage("agents", organizedAgentsHTML)
	registerPage("advanced", organizedAdvancedHTML)
}

const organizedAgentsHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Agents</h1>
    <p>Everything specific to Codex or Claude lives here: SMS shortcuts, conversations, access, runtime, and sign-in.</p>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<section class="card">
  <form method="post" action="/agents/save">
    <div class="card-head divided">
      <div class="card-title-row">
        <span class="bmark lg codex">{{brand "codex"}}</span>
        <div>
          <h2>Codex
            <span class="pill {{if .S.CodexCheck.OK}}ok{{else}}warn{{end}}">{{if .S.CodexCheck.OK}}Ready{{else if .S.CodexCheck.Known}}Needs attention{{else}}Not tested{{end}}</span>
            {{if eq .S.DefaultAgent "C"}}<span class="pill brand">Default</span>{{end}}
          </h2>
          <p>Codex CLI through your ChatGPT login. All Codex-specific controls are grouped below.</p>
        </div>
      </div>
      <div class="head-actions">
        <a class="btn accent" href="/codex/test">{{icon "play"}}Test Codex</a>
        {{if ne .S.DefaultAgent "C"}}<button class="btn" type="submit" name="defaultAgent" value="C">{{icon "check"}}Make default</button>{{end}}
      </div>
    </div>
    <div class="card-body">
      <details class="disclosure" open>
        <summary>SMS shortcut</summary>
        <div class="disclosure-body">
          <div class="grid-2">
            <div class="field">
              <label for="codexPrefix">Codex shortcut</label>
              <input id="codexPrefix" type="text" name="codexPrefix" value="{{.S.CodexPrefix}}" maxlength="24" required>
              <p class="hint">Start a text with <b>{{.S.CodexPrefix}}:</b> to send it specifically to Codex. Letters or numbers are fine.</p>
            </div>
            <div class="field">
              <label for="codexNewSessionCommand">New conversation word</label>
              <input id="codexNewSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
              <p class="hint">Start fresh with <b>{{.S.CodexPrefix}} {{.S.NewSessionCommand}}</b>. This word is shared by both agents; each agent uses its own shortcut.</p>
            </div>
          </div>
        </div>
      </details>

      <details class="disclosure" open>
        <summary>Conversation &amp; session</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row">
              <div class="label">Current conversation<span>FlipAi keeps the current Codex thread between SMS turns and releases it after each turn so Codex Desktop can open the same history.</span></div>
              <div class="value"><b>{{if .S.CodexThreadActive}}Thread active{{else}}No conversation yet{{end}}</b></div>
            </div>
            <div class="row">
              <div class="label">Reset / start new chat<span>Send this exact shortcut from your allowed phone. The next Codex request starts with a clean conversation.</span></div>
              <div class="value"><b class="mono">{{.S.CodexPrefix}} {{.S.NewSessionCommand}}</b></div>
            </div>
          </div>
        </div>
      </details>

      <details class="disclosure" open>
        <summary>Access &amp; tools</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row">
              <div class="label">Access level<span>SMS turns run with this Windows user's normal permissions. FlipAi does not elevate the process.</span></div>
              <div class="value"><b>Full user access</b><span class="pill ok">Unattended</span></div>
            </div>
            <div class="row">
              <div class="label">Approval behavior<span>Codex runs unattended so it cannot stop and wait for an approval dialog.</span></div>
              <div class="value"><b>No interactive approvals</b></div>
            </div>
          </div>
        </div>
      </details>

      <details class="disclosure">
        <summary>Runtime</summary>
        <div class="disclosure-body">
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
          <div class="rows">
            <div class="row"><div class="label">Last test</div><div class="value"><b>{{if .S.CodexCheck.Known}}{{ago .S.CodexCheck.At}}{{else}}Never{{end}}</b>{{if .S.CodexCheck.Detail}}<span>{{.S.CodexCheck.Detail}}</span>{{end}}</div></div>
          </div>
          <div class="form-actions">
            <button class="btn primary" type="submit">Save Codex settings</button>
            <a class="btn" href="/open/folder?which=codex">{{icon "folder"}}Open folder</a>
            <a class="btn" href="/activity?stage=agent">{{icon "clock"}}Agent activity</a>
          </div>
        </div>
      </details>
    </div>
  </form>
</section>

<section class="card">
  <form method="post" action="/agents/save">
    <div class="card-head divided">
      <div class="card-title-row">
        <span class="bmark lg claude">{{brand "claude"}}</span>
        <div>
          <h2>Claude
            <span class="pill {{if .S.ClaudeCheck.OK}}ok{{else}}warn{{end}}">{{if .S.ClaudeCheck.OK}}Ready{{else if .S.ClaudeCheck.Known}}Needs attention{{else}}Not tested{{end}}</span>
            {{if eq .S.DefaultAgent "A"}}<span class="pill brand">Default</span>{{end}}
          </h2>
          <p>Claude Code CLI through your Claude subscription. All Claude-specific controls are grouped below.</p>
        </div>
      </div>
      <div class="head-actions">
        <a class="btn accent" href="/claude/test">{{icon "play"}}Test Claude</a>
        {{if ne .S.DefaultAgent "A"}}<button class="btn" type="submit" name="defaultAgent" value="A">{{icon "check"}}Make default</button>{{end}}
      </div>
    </div>
    <div class="card-body">
      <details class="disclosure" open>
        <summary>SMS shortcut</summary>
        <div class="disclosure-body">
          <div class="grid-2">
            <div class="field">
              <label for="claudePrefix">Claude shortcut</label>
              <input id="claudePrefix" type="text" name="claudePrefix" value="{{.S.ClaudePrefix}}" maxlength="24" required>
              <p class="hint">Start a text with <b>{{.S.ClaudePrefix}}:</b> to send it specifically to Claude. It must differ from the Codex shortcut.</p>
            </div>
            <div class="field">
              <label for="claudeNewSessionCommand">New conversation word</label>
              <input id="claudeNewSessionCommand" type="text" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required>
              <p class="hint">Start fresh with <b>{{.S.ClaudePrefix}} {{.S.NewSessionCommand}}</b>. This word is shared by both agents; each agent uses its own shortcut.</p>
            </div>
          </div>
        </div>
      </details>

      <details class="disclosure" open>
        <summary>Conversation &amp; session</summary>
        <div class="disclosure-body">
          <div class="rows">
            <div class="row">
              <div class="label">Current session</div>
              <div class="value"><b>{{if .S.ClaudeSessionActive}}Session active{{else}}No conversation yet{{end}}</b>{{if .S.ClaudeSessionName}}<span>Named "{{.S.ClaudeSessionName}}".</span>{{end}}</div>
            </div>
            <div class="row">
              <div class="label">Reset / start new chat<span>Send this exact shortcut from your allowed phone. FlipAi clears the current Claude session and the next request starts fresh.</span></div>
              <div class="value"><b class="mono">{{.S.ClaudePrefix}} {{.S.NewSessionCommand}}</b></div>
            </div>
            {{if .S.ClaudeSessionActive}}
            <div class="row">
              <div class="label">Resume in Claude Code<span>Open the current SMS conversation directly by session id.</span></div>
              <div class="value"><b class="mono">claude --resume {{.S.ClaudeSessionID}}</b></div>
            </div>
            <div class="row">
              <div class="label">Move to Claude Desktop<span>Resume the session above, then enter this command in Claude Code.</span></div>
              <div class="value"><b class="mono">/desktop</b></div>
            </div>
            {{end}}
          </div>
        </div>
      </details>

      <details class="disclosure" open>
        <summary>Access &amp; browser</summary>
        <div class="disclosure-body">
          <div class="field">
            <label for="permissionMode">Claude access level</label>
            <select id="permissionMode" name="permissionMode">
              <option value="bypassPermissions"{{if eq .S.PermissionMode "bypassPermissions"}} selected{{end}}>Full user access (matches Codex)</option>
              <option value="dontAsk"{{if eq .S.PermissionMode "dontAsk"}} selected{{end}}>Never prompt (Claude rules decide)</option>
              <option value="acceptEdits"{{if eq .S.PermissionMode "acceptEdits"}} selected{{end}}>Accept edits only (blocks Chrome)</option>
              <option value="plan"{{if eq .S.PermissionMode "plan"}} selected{{end}}>Plan only</option>
              <option value="default"{{if eq .S.PermissionMode "default"}} selected{{end}}>Ask (blocks unattended turns)</option>
            </select>
            <p class="hint">SMS is unattended. Modes that require approval can block tools. Full user access gives Claude the same signed-in-user reach as Codex, without elevation.</p>
          </div>
          <div class="toggle">
            <div class="label">Let Claude control Chrome<span>Passes <b>--chrome</b>. Browser tools require an access mode that permits them.</span></div>
            <label class="switch"><input type="hidden" name="claudeUseChrome" value="0"><input type="checkbox" name="claudeUseChrome" value="1"{{if .S.ClaudeUseChrome}} checked{{end}}><span class="slider"></span></label>
          </div>
        </div>
      </details>

      <details class="disclosure">
        <summary>Runtime &amp; sign-in</summary>
        <div class="disclosure-body">
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
          <div class="field">
            <label for="claudeToken">Long-lived token{{if .S.HasClaudeToken}} — saved{{end}}</label>
            <input id="claudeToken" type="password" name="claudeToken" autocomplete="off" placeholder="{{if .S.HasClaudeToken}}leave blank to keep the saved token{{else}}paste the value from claude setup-token{{end}}">
            <p class="hint">Optional. A setup token keeps the unattended bridge signed in longer and is stored with Windows DPAPI.</p>
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
          <div class="form-actions">
            <button class="btn primary" type="submit">Save Claude settings</button>
            <a class="btn" href="/open/folder?which=claude">{{icon "folder"}}Open folder</a>
            <a class="btn" href="/activity?stage=agent">{{icon "clock"}}Agent activity</a>
          </div>
        </div>
      </details>
    </div>
  </form>
</section>

<section class="card">
  <form method="post" action="/agents/save">
    <div class="card-head">
      <div class="card-title-row">
        <span class="mark shield">{{icon "sliders"}}</span>
        <div><h2>Shared agent behavior</h2><p>Only settings that truly apply across both agents stay here.</p></div>
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
          <p class="hint">Used when a text has no agent shortcut.</p>
        </div>
        <div class="field">
          <label for="turnTimeout">Turn timeout</label>
          <div class="input-suffix"><input id="turnTimeout" type="number" name="turnTimeout" min="1" max="600" value="{{.S.TurnTimeout}}"><span class="unit">min</span></div>
          <p class="hint">Maximum runtime for any one agent turn.</p>
        </div>
        <div class="field">
          <label for="cwd">Fallback working folder</label>
          <div class="input-group">
            <input id="cwd" type="text" name="cwd" value="{{.S.Cwd}}">
            <button class="btn" type="button" data-browse="#cwd">Browse</button>
          </div>
          <p class="hint">Used only when an agent does not have its own folder.</p>
        </div>
      </div>
      <div class="form-actions"><button class="btn primary" type="submit">Save shared behavior</button></div>
    </div>
  </form>
</section>
{{end}}`

const organizedAdvancedHTML = `{{define "content"}}
<div class="page-head">
  <div>
    <h1>Advanced</h1>
    <p>App-level diagnostics, local service health, logs, and troubleshooting.</p>
  </div>
</div>

<div class="tiles">{{range .Tiles}}{{template "tile" .}}{{end}}</div>

<section class="card">
  <div class="card-head divided">
    <div class="card-title-row">
      <span class="mark shield">{{icon "agent"}}</span>
      <div><h2>Agent configuration moved to Agents</h2><p>Executable paths, folders, access, sessions, tokens, browser controls, and SMS shortcuts are managed with the agent they belong to.</p></div>
    </div>
    <div class="head-actions"><a class="btn accent" href="/agents">Open Agents{{icon "chevron"}}</a></div>
  </div>
</section>

<div class="cards-2">
  <section class="card">
    <div class="card-head divided">
      <div class="card-title-row"><span class="mark shield">{{icon "server"}}</span><div><h2>Local service</h2><p>The loopback control service used by this desktop window.</p></div></div>
    </div>
    <div class="card-body">
      <div class="rows">
        <div class="row"><div class="label">Loopback address</div><div class="value"><b class="mono">http://{{.S.Listen}}</b><span class="pill ok">Local only</span></div></div>
        <div class="row"><div class="label">Session protection<span>Pages are available only to windows opened by FlipAi.</span></div><div class="value"><b>Active</b><span class="pill ok">Valid</span></div></div>
        {{range .Health}}<div class="row"><div class="label">{{.Label}}</div><div class="value"><b class="{{.Tone}}">{{.Value}}</b></div></div>{{end}}
      </div>
      <div class="form-actions"><form method="post" action="/health/check"><button class="btn accent" type="submit">{{icon "check"}}Run health check</button></form></div>
    </div>
  </section>

  <section class="card">
    <div class="card-head"><div class="card-title-row"><span class="mark shield">{{icon "wrench"}}</span><div><h2>Diagnostics</h2><p>App-level tools for troubleshooting FlipAi itself.</p></div></div></div>
    <div class="card-body">
      <div class="rows">
        <div class="row"><div class="label">Export logs<span>Save the local activity and bridge logs.</span></div><div class="value"><a class="btn small" href="/logs/export">{{icon "download"}}Export</a></div></div>
        <div class="row"><div class="label">Open data folder<span>{{.S.DataDir}}</span></div><div class="value"><a class="btn small" href="/open/folder?which=data">{{icon "folder"}}Open</a></div></div>
        <div class="row"><div class="label">Restart bridge<span>Restart the local background host without resetting settings.</span></div><div class="value"><form method="post" action="/bridge/restart"><button class="btn small" type="submit">{{icon "refresh"}}Restart</button></form></div></div>
      </div>
    </div>
  </section>
</div>

{{if .HasError}}
<section class="card">
  <div class="card-head"><div class="card-title-row"><span class="mark shield">{{icon "alert"}}</span><div><h2>Latest error</h2><p>The newest error recorded in FlipAi activity.</p></div></div></div>
  <div class="card-body">
    <div class="rows">
      <div class="row"><div class="label">When</div><div class="value"><b>{{stamp .LastError.Time}}</b></div></div>
      <div class="row"><div class="label">Stage</div><div class="value"><b>{{.LastError.Stage}}</b></div></div>
      <div class="row"><div class="label">Message</div><div class="value"><b>{{.LastError.Message}}</b></div></div>
    </div>
  </div>
</section>
{{end}}
{{end}}`
