package main

// This file deliberately registers the Agents page after the older page files.
// It is presentation-only: every field posts to the existing validated handlers,
// so the redesigned screen cannot advertise controls the bridge does not support.
func init() {
	registerPage("agents", masterDetailAgentsHTML)
}

const masterDetailAgentsHTML = `{{define "content"}}
<style>
.content{max-width:none;padding:0;min-height:100vh}
.agents-studio{min-height:100vh;display:grid;grid-template-columns:305px minmax(0,1fr);background:var(--bg)}
.agent-switch{position:absolute;opacity:0;pointer-events:none}
.agent-rail{border-right:1px solid var(--line);padding:26px 18px;background:var(--surface);min-width:0}
.agent-rail-title{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px}
.agent-rail-title h1{font-size:22px;font-weight:650}
.agent-search{position:relative;margin-bottom:14px}
.agent-search input{padding-left:36px;background:var(--surface)}
.agent-search svg{position:absolute;left:11px;top:50%;transform:translateY(-50%);width:16px;height:16px;color:var(--muted)}
.agent-list{display:flex;flex-direction:column;gap:8px}
.agent-item{display:flex;align-items:center;gap:11px;padding:13px 12px;cursor:pointer;border:1px solid var(--line);border-radius:var(--radius-control);background:var(--surface);min-width:0;transition:.15s}

.agent-item:hover{background:var(--surface-2)}
.agent-item-copy{min-width:0;flex:1}
.agent-item-copy b{display:flex;align-items:center;gap:7px;font-size:13.5px;font-weight:620;color:var(--ink)}
.agent-item-copy span{display:block;color:var(--muted);font-size:11.5px;margin-top:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.agent-mini-status{display:inline-flex!important;margin:0!important;padding:2px 7px;border-radius:999px;font-size:10px!important;font-weight:650;background:var(--ok-soft);color:var(--ok)!important}
.agent-mini-status.warn{background:var(--warn-soft);color:var(--warn)!important}
#agent-codex:checked~.agents-studio .agent-item[for="agent-codex"],#agent-claude:checked~.agents-studio .agent-item[for="agent-claude"],#agent-shared:checked~.agents-studio .agent-item[for="agent-shared"]{background:var(--brand-soft);box-shadow:inset 0 0 0 1px var(--brand);}
.agent-workspace{padding:26px 32px 44px;min-width:0;overflow:auto}
.agent-pane{display:none;max-width:980px;margin:0 auto}
#agent-codex:checked~.agents-studio #codex-pane,#agent-claude:checked~.agents-studio #claude-pane,#agent-shared:checked~.agents-studio #shared-pane{display:block}
.agent-header{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-bottom:20px}
.agent-header-main{display:flex;align-items:center;gap:13px;min-width:0}
.agent-header-main h2{font-size:23px;font-weight:650;display:flex;align-items:center;gap:8px}
.agent-header-main p{margin:3px 0 0;color:var(--muted);font-size:12.5px}
.agent-actions{display:flex;align-items:center;gap:9px;flex-wrap:wrap;justify-content:flex-end}
.agent-section{border:1px solid var(--line);border-radius:10px;background:var(--surface);margin-bottom:13px;box-shadow:var(--shadow)}
.agent-section-head{display:flex;align-items:center;gap:10px;padding:15px 17px;border-bottom:1px solid var(--line-soft);font-size:14px;font-weight:650}
.agent-section-icon{width:26px;height:26px;border-radius:7px;background:var(--brand-soft);color:var(--brand-ink);display:grid;place-items:center}
.agent-section-icon svg{width:15px;height:15px}
.agent-section-body{padding:16px 17px}
.agent-fields-3{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}
.agent-fields-2{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}
.agent-section .field{margin-top:0}
.agent-section .field label{font-size:12px;margin-bottom:5px}
.agent-section input,.agent-section select{min-height:38px}
.agent-inline{display:flex;align-items:center;gap:10px}
.agent-inline .field{flex:1}
.agent-session-row{display:grid;grid-template-columns:minmax(180px,.8fr) minmax(140px,.55fr) minmax(0,1.5fr);gap:22px;align-items:end}
.agent-stat label{display:block;font-size:11.5px;color:var(--muted);margin-bottom:6px}
.agent-stat b{font-size:12.5px;font-weight:600}
.agent-session-id{font:12px ui-monospace,Consolas,monospace;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;color:var(--ink-2)}
.agent-shared-grid{display:grid;grid-template-columns:1fr .8fr 1.6fr;gap:14px}
.agent-note{font-size:11.5px;color:var(--muted);margin-top:6px}
.agent-form-actions{display:flex;justify-content:flex-end;margin-top:14px}
.agent-danger-form{display:inline-flex}
.agent-empty-id{color:var(--muted);font-size:12px}
.agent-auth-row{display:flex;align-items:center;gap:16px;justify-content:space-between}
.agent-auth-row .field{max-width:420px;flex:1}
.agent-toggle-line{display:flex;align-items:center;justify-content:space-between;gap:18px;padding-top:8px}
.agent-toggle-line span:first-child{font-size:12.5px;font-weight:560}
.agent-shared-title{font-size:23px!important}
.agent-shared-sub{color:var(--muted);font-size:12.5px;margin-top:3px}
@media(max-width:1050px){.agents-studio{grid-template-columns:245px minmax(0,1fr)}.agent-workspace{padding:22px 22px 40px}.agent-fields-3{grid-template-columns:1fr 1fr}.agent-session-row{grid-template-columns:1fr 1fr}.agent-session-row .agent-stat:last-child{grid-column:1/-1}.agent-shared-grid{grid-template-columns:1fr 1fr}}
@media(max-width:760px){.agents-studio{display:block}.agent-rail{border-right:0;border-bottom:1px solid var(--line);padding:18px}.agent-list{display:grid;grid-template-columns:repeat(3,1fr)}.agent-item-copy span{display:none}.agent-workspace{padding:20px 16px}.agent-header{align-items:flex-start;flex-direction:column}.agent-actions{width:100%;justify-content:flex-start}.agent-fields-3,.agent-fields-2,.agent-session-row,.agent-shared-grid{grid-template-columns:1fr}.agent-session-row .agent-stat:last-child{grid-column:auto}}
</style>

<input class="agent-switch" type="radio" name="agent-view" id="agent-codex">
<input class="agent-switch" type="radio" name="agent-view" id="agent-claude" checked>
<input class="agent-switch" type="radio" name="agent-view" id="agent-shared">

<div class="agents-studio">
  <aside class="agent-rail">
    <div class="agent-rail-title"><h1>Agents</h1></div>
    <div class="agent-search">{{icon "search"}}<input id="agent-filter" type="text" placeholder="Search agents..." autocomplete="off"></div>
    <div class="agent-list" id="agent-list">
      <label class="agent-item" for="agent-codex" data-agent-name="codex">
        <span class="bmark codex">{{brand "codex"}}</span>
        <span class="agent-item-copy"><b>Codex <span class="agent-mini-status {{if not .S.CodexCheck.OK}}warn{{end}}">{{if .S.CodexCheck.OK}}Ready{{else if .S.CodexCheck.Known}}Needs attention{{else}}Not tested{{end}}</span></b><span>Local Codex CLI</span></span>
      </label>
      <label class="agent-item" for="agent-claude" data-agent-name="claude">
        <span class="bmark claude">{{brand "claude"}}</span>
        <span class="agent-item-copy"><b>Claude <span class="agent-mini-status {{if not .S.ClaudeCheck.OK}}warn{{end}}">{{if .S.ClaudeCheck.OK}}Ready{{else if .S.ClaudeCheck.Known}}Needs attention{{else}}Not tested{{end}}</span></b><span>Claude Code CLI</span></span>
      </label>
      <label class="agent-item" for="agent-shared" data-agent-name="shared defaults">
        <span class="mark shield" style="width:34px;height:34px;border-radius:9px">{{icon "sliders"}}</span>
        <span class="agent-item-copy"><b>Shared defaults</b><span>Applied to all agents</span></span>
      </label>
    </div>
  </aside>

  <main class="agent-workspace">
    <section class="agent-pane" id="codex-pane">
      <form method="post" action="/agents/save">
        <div class="agent-header">
          <div class="agent-header-main"><span class="bmark lg codex">{{brand "codex"}}</span><div><h2>Codex <span class="pill {{if .S.CodexCheck.OK}}ok{{else}}warn{{end}}">{{if .S.CodexCheck.OK}}Ready{{else if .S.CodexCheck.Known}}Needs attention{{else}}Not tested{{end}}</span></h2><p>Local Codex CLI</p></div></div>
          <div class="agent-actions"><a class="btn accent" href="/codex/test">{{icon "play"}}Test Codex</a>{{if .S.CodexThreadActive}}<button class="btn danger" type="submit" formaction="/agents/reset" name="agent" value="C" data-confirm="Reset the current Codex conversation?">{{icon "refresh"}}Reset chat</button>{{end}}<button class="btn primary" type="submit">Save changes</button></div>
        </div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "key"}}</span>Shortcuts &amp; session</div><div class="agent-section-body">
          <div class="agent-fields-3">
            <div class="field"><label for="mdCodexPrefix">SMS shortcut</label><input id="mdCodexPrefix" name="codexPrefix" value="{{.S.CodexPrefix}}" maxlength="24" required></div>
            <div class="field"><label for="mdCodexNew">New session code</label><input id="mdCodexNew" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required><div class="agent-note">Shared keyword; your shortcut makes it Codex-specific.</div></div>
            <div class="field"><label>Reset chat</label><button class="btn" style="width:100%;min-height:38px" type="submit" formaction="/agents/reset" name="agent" value="C"{{if not .S.CodexThreadActive}} disabled{{end}}>{{icon "refresh"}}{{if .S.CodexThreadActive}}Reset current chat{{else}}No active chat{{end}}</button></div>
          </div>
        </div></div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "folder"}}</span>Workspace &amp; paths</div><div class="agent-section-body">
          <div class="field"><label for="mdCodexCwd">Working folder</label><div class="input-group"><input id="mdCodexCwd" name="codexCwd" value="{{.S.CodexCwd}}"><button class="btn" type="button" data-browse="#mdCodexCwd">Browse</button></div></div>
          <div class="field" style="margin-top:13px"><label for="mdCodexPath">Executable path</label><input id="mdCodexPath" name="codexPath" value="{{.S.CodexPath}}" placeholder="codex"></div>
        </div></div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "shield"}}</span>Access &amp; tools</div><div class="agent-section-body">
          <div class="agent-auth-row"><div class="agent-stat"><label>Permission mode</label><b>Full user access</b></div><div class="agent-stat"><label>Effective access</label><span class="pill brand">Full user access</span></div></div>
        </div></div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "key"}}</span>Authentication &amp; session</div><div class="agent-section-body">
          <div class="field" style="margin-bottom:14px"><label for="mdCodexProgress">Progress texts every</label><select id="mdCodexProgress" name="codexProgressInterval">
            <option value="0"{{if eq .S.CodexProgressInterval 0}} selected{{end}}>Follow shared setting</option>
            <option value="30"{{if eq .S.CodexProgressInterval 30}} selected{{end}}>30 seconds</option>
            <option value="60"{{if eq .S.CodexProgressInterval 60}} selected{{end}}>1 minute</option>
            <option value="300"{{if eq .S.CodexProgressInterval 300}} selected{{end}}>5 minutes</option>
            <option value="900"{{if eq .S.CodexProgressInterval 900}} selected{{end}}>15 minutes</option>
          </select><div class="agent-note">How often a long Codex turn texts that it is still working.</div></div>
          <div class="agent-session-row"><div class="agent-stat"><label>Authentication</label><b>ChatGPT / Codex CLI</b></div><div class="agent-stat"><label>Current session</label>{{if .S.CodexThreadActive}}<span class="pill ok">Active</span>{{else}}<span class="pill">None</span>{{end}}</div><div class="agent-stat"><label>Session</label><span class="agent-empty-id">Managed by Codex Desktop</span></div></div>
        </div></div>
      </form>
    </section>

    <section class="agent-pane" id="claude-pane">
      <form method="post" action="/agents/save">
        <div class="agent-header">
          <div class="agent-header-main"><span class="bmark lg claude">{{brand "claude"}}</span><div><h2>Claude <span class="pill {{if .S.ClaudeCheck.OK}}ok{{else}}warn{{end}}">{{if .S.ClaudeCheck.OK}}Ready{{else if .S.ClaudeCheck.Known}}Needs attention{{else}}Not tested{{end}}</span></h2><p>Claude Code CLI</p></div></div>
          <div class="agent-actions"><a class="btn accent" href="/claude/test">{{icon "play"}}Test Claude</a>{{if or .S.ClaudeSessionActive .S.ClaudeSessionName}}<button class="btn danger" type="submit" formaction="/agents/reset" name="agent" value="A" data-confirm="Reset the current Claude conversation?">{{icon "refresh"}}Reset chat</button>{{end}}<button class="btn primary" type="submit">Save changes</button></div>
        </div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "key"}}</span>Shortcuts &amp; session</div><div class="agent-section-body">
          <div class="agent-fields-3">
            <div class="field"><label for="mdClaudePrefix">SMS shortcut</label><input id="mdClaudePrefix" name="claudePrefix" value="{{.S.ClaudePrefix}}" maxlength="24" required></div>
            <div class="field"><label for="mdClaudeNew">New session code</label><input id="mdClaudeNew" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required><div class="agent-note">Shared keyword; your shortcut makes it Claude-specific.</div></div>
            <div class="field"><label>Resume command</label><input value="{{if .S.ClaudeSessionActive}}claude --resume {{.S.ClaudeSessionID}}{{else}}Available after first session{{end}}" readonly></div>
          </div>
        </div></div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "folder"}}</span>Workspace &amp; paths</div><div class="agent-section-body">
          <div class="field"><label for="mdClaudeCwd">Working folder</label><div class="input-group"><input id="mdClaudeCwd" name="claudeCwd" value="{{.S.ClaudeCwd}}"><button class="btn" type="button" data-browse="#mdClaudeCwd">Browse</button></div></div>
          <div class="field" style="margin-top:13px"><label for="mdClaudePath">Executable path</label><input id="mdClaudePath" name="claudePath" value="{{.S.ClaudePath}}" placeholder="claude"></div>
        </div></div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "shield"}}</span>Access &amp; tools</div><div class="agent-section-body">
          <div class="agent-fields-2"><div class="field"><label for="mdPermission">Permission mode</label><select id="mdPermission" name="permissionMode"><option value="bypassPermissions"{{if eq .S.PermissionMode "bypassPermissions"}} selected{{end}}>Full user access (matches Codex)</option><option value="dontAsk"{{if eq .S.PermissionMode "dontAsk"}} selected{{end}}>Never prompt</option><option value="acceptEdits"{{if eq .S.PermissionMode "acceptEdits"}} selected{{end}}>Accept edits only</option><option value="plan"{{if eq .S.PermissionMode "plan"}} selected{{end}}>Plan only</option><option value="default"{{if eq .S.PermissionMode "default"}} selected{{end}}>Ask</option></select></div><div class="agent-toggle-line"><span>Let Claude control Chrome</span><label class="switch"><input type="hidden" name="claudeUseChrome" value="0"><input type="checkbox" name="claudeUseChrome" value="1"{{if .S.ClaudeUseChrome}} checked{{end}}><span class="slider"></span></label></div></div>
          <div style="margin-top:12px"><span class="agent-note" style="margin-right:8px">Effective access</span><span class="pill brand">{{.S.PermissionModeLabel}}</span></div>
          {{if .S.ChromeTokenNotice}}<div class="agent-note" style="margin-top:12px"><b>Chrome is off:</b> {{.S.ChromeTokenNotice}}</div>{{end}}
        </div></div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "refresh"}}</span>Conversation mode</div><div class="agent-section-body">
          <div class="agent-fields-2">
            <div class="field"><label for="mdClaudeSessionMode">How SMS reaches Claude</label><select id="mdClaudeSessionMode" name="claudeSessionMode">
              <option value="print"{{if eq .S.ClaudeSessionMode "print"}} selected{{end}}>Per-message (recommended)</option>
              <option value="live"{{if eq .S.ClaudeSessionMode "live"}} selected{{end}}>Live session with Remote Control</option>
            </select><div class="agent-note">Per-message runs one request per text and needs nothing running in the background. Live keeps one Claude session open for the whole conversation so you can also open it at claude.ai/code.</div></div>
            <div class="field"><label for="mdClaudeProgress">Progress texts every</label><select id="mdClaudeProgress" name="claudeProgressInterval">
              <option value="0"{{if eq .S.ClaudeProgressInterval 0}} selected{{end}}>Follow shared setting</option>
              <option value="30"{{if eq .S.ClaudeProgressInterval 30}} selected{{end}}>30 seconds</option>
              <option value="60"{{if eq .S.ClaudeProgressInterval 60}} selected{{end}}>1 minute</option>
              <option value="300"{{if eq .S.ClaudeProgressInterval 300}} selected{{end}}>5 minutes</option>
              <option value="900"{{if eq .S.ClaudeProgressInterval 900}} selected{{end}}>15 minutes</option>
            </select><div class="agent-note">How often a long Claude turn texts that it is still working.</div></div>
            <div class="agent-stat"><label>Running now</label>{{if .S.LiveActive}}<span class="pill ok">Live session</span>{{else}}<span class="pill">Per-message</span>{{end}}{{if .S.LiveRemoteControl}}<span class="pill brand" style="margin-left:6px">Remote Control</span>{{end}}</div>
          </div>
          {{if .S.LiveNotice}}<div class="agent-note" style="margin-top:12px"><b>Heads up:</b> {{.S.LiveNotice}}</div>{{end}}
          {{if .S.ClaudeLiveSessionID}}<div class="field" style="margin-top:12px"><label>Live session ID</label><input value="{{.S.ClaudeLiveSessionID}}" readonly></div>{{end}}
        </div></div>

        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "key"}}</span>Authentication &amp; session</div><div class="agent-section-body">
          <div class="agent-session-row"><div class="agent-stat"><label>Connection</label><b>{{.S.ClaudeConnLabel}}</b></div><div class="agent-stat"><label>Chrome &amp; browser view</label>{{if .S.ClaudeConnChromeReady}}<span class="pill ok">Available</span>{{else if .S.ClaudeConnNeedsSignIn}}<span class="pill warn">Not available</span>{{else}}<span class="pill">Checking</span>{{end}}</div><div class="agent-stat"><label>Current session</label>{{if .S.ClaudeSessionActive}}<span class="pill ok">Active</span>{{else}}<span class="pill">None</span>{{end}}</div></div>
          <div class="agent-note" style="margin-top:12px">{{.S.ClaudeConnDetail}}</div>
          <div class="agent-actions" style="justify-content:flex-start;margin-top:12px">
            <button class="btn{{if .S.ClaudeConnNeedsSignIn}} primary{{end}}" type="submit" formaction="/claude/connect" formnovalidate>{{icon "key"}}Connect Claude</button>
            <button class="btn" type="submit" formaction="/claude/connect/verify" formnovalidate>{{icon "refresh"}}Check connection</button>
            <button class="btn danger" type="submit" formaction="/claude/disconnect" formnovalidate data-confirm="Disconnect Claude from FlipAi? Your own Claude Code sign-in on this PC is left alone.">Disconnect</button>
          </div>
          <div class="field" style="margin-top:16px"><label for="mdClaudeToken">Long-lived token — fallback only</label><input id="mdClaudeToken" type="password" name="claudeToken" autocomplete="off" placeholder="{{if .S.HasClaudeToken}}Saved — leave blank to keep{{else}}Optional{{end}}"><div class="agent-note">Optional. FlipAi runs on the sign-in above so Chrome and claude.ai/code work, and uses a saved <code>claude setup-token</code> value only if that sign-in lapses. A token on its own cannot drive the browser.</div></div>
          <div class="agent-stat" style="margin-top:14px"><label>Session ID</label>{{if .S.ClaudeSessionID}}<div class="agent-session-id">{{.S.ClaudeSessionID}}</div>{{else}}<span class="agent-empty-id">No session yet</span>{{end}}</div>
        </div></div>
      </form>
    </section>

    <section class="agent-pane" id="shared-pane">
      <form method="post" action="/agents/save">
        <div class="agent-header"><div class="agent-header-main"><span class="mark shield" style="width:42px;height:42px;border-radius:10px">{{icon "sliders"}}</span><div><h2 class="agent-shared-title">Shared defaults</h2><div class="agent-shared-sub">Settings used by both agents.</div></div></div><div class="agent-actions"><button class="btn primary" type="submit">Save changes</button></div></div>
        <div class="agent-section"><div class="agent-section-head"><span class="agent-section-icon">{{icon "sliders"}}</span>Shared behavior</div><div class="agent-section-body">
          <div class="agent-shared-grid">
            <div class="field"><label for="mdDefaultAgent">Default agent</label><select id="mdDefaultAgent" name="defaultAgent"><option value="C"{{if eq .S.DefaultAgent "C"}} selected{{end}}>Codex</option><option value="A"{{if eq .S.DefaultAgent "A"}} selected{{end}}>Claude</option></select></div>
            <div class="field"><label for="mdTurnTimeout">Turn timeout</label><div class="input-suffix"><input id="mdTurnTimeout" type="number" name="turnTimeout" min="1" max="600" value="{{.S.TurnTimeout}}"><span class="unit">min</span></div></div>
            <div class="field"><label for="mdSharedCwd">Shared fallback folder</label><div class="input-group"><input id="mdSharedCwd" name="cwd" value="{{.S.Cwd}}"><button class="btn" type="button" data-browse="#mdSharedCwd">Browse</button></div></div>
          </div>
          <div class="field" style="margin-top:14px;max-width:360px"><label for="mdSharedNew">New-session keyword</label><input id="mdSharedNew" name="newSessionCommand" value="{{.S.NewSessionCommand}}" maxlength="24" required><div class="agent-note">Used with each agent shortcut, for example {{.S.CodexPrefix}} {{.S.NewSessionCommand}} or {{.S.ClaudePrefix}} {{.S.NewSessionCommand}}.</div></div>
        </div></div>
      </form>
    </section>
  </main>
</div>

<script>
(function(){
  var q=document.getElementById('agent-filter');
  if(!q)return;
  q.addEventListener('input',function(){
    var v=q.value.trim().toLowerCase();
    document.querySelectorAll('#agent-list .agent-item').forEach(function(row){
      row.style.display=!v||row.getAttribute('data-agent-name').indexOf(v)>=0?'':'none';
    });
  });
})();
</script>
{{end}}`
