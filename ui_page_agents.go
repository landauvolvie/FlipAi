package main

import (
	"net/http"
	"strings"
)

// The Agents screen is a master/detail workbench: a rail listing Codex, Claude
// and the settings that genuinely apply to both, and one pane per entry.
//
// Everything an agent owns lives in that agent's pane — its SMS shortcut, its
// folder, its executable, its conversation, its access, and the instruction
// FlipAi sends with every text. Nothing agent-specific is repeated on another
// page, and the shared pane holds only what both agents really share.
//
// The pane switch is three hidden radio inputs and CSS sibling selectors, so
// selecting an agent needs no script and no round trip.
func init() {
	registerPage("agents", agentsPageHTML)
}

type agentsView struct {
	pageView

	// One SMS instruction editor per agent. There is no shared editor any more:
	// the instruction is part of what makes an agent behave the way it does, so
	// it belongs to the agent.
	CodexPrompt  promptEditorView
	ClaudePrompt promptEditorView

	// Who may reach each agent, and how it answers them.
	CodexAccess  agentAccessView
	ClaudeAccess agentAccessView
}

// agentAccessView is everything about who may reach one agent and how it
// replies. A phone number lives here, on the agent it reaches, rather than on a
// shared list that a prefix had to steer.
type agentAccessView struct {
	Agent       string // "C" or "A"
	Name        string
	Prefix      string
	Phones      []AgentPhone
	CallerNames string
	RequireCode bool
	HasCode     bool
	Ack         bool
	Progress    bool
	Interval    int
	IsDefault   bool
	Voice       bool
}

func (v agentAccessView) Field(name string) string { return agentFieldName(v.Agent, name) }

// agentFieldName keeps every per-agent form field distinct without repeating
// the agent letter at each use.
func agentFieldName(agent, name string) string {
	if agent == "A" {
		return "claude" + strings.ToUpper(name[:1]) + name[1:]
	}
	return "codex" + strings.ToUpper(name[:1]) + name[1:]
}

// promptEditorView is one SMS instruction editor. Fallback is the wording that
// applies when the box is left empty, which is what makes an empty box mean
// "follow the shared default" instead of "send no framing at all".
type promptEditorView struct {
	Name     string
	Title    string
	Value    string
	Fallback string
	Hint     string
	Custom   bool
	Max      int
}

const agentsPageHTML = `
{{define "agentAccess"}}
<section class="card">
  <div class="card-head divided">
    <div><h2>Allowed phone numbers</h2><p>Only these numbers can reach {{.Name}}. A number belongs to one agent, so adding it here removes it from the other.</p></div>
  </div>
  <div class="card-body">
    {{if .Phones}}
    <div class="rows">
      {{range .Phones}}
      <div class="row">
        <div class="label">{{.Display}}{{if .Label}}<span>{{.Label}}</span>{{end}}</div>
        <div class="value">
          <select name="access-{{$.Agent}}-{{.Number}}" aria-label="What {{.Display}} may do">
            <option value="all"{{if eq .Access "all"}} selected{{end}}>Texts and calls</option>
            <option value="sms"{{if eq .Access "sms"}} selected{{end}}>Texts only</option>
            <option value="voice"{{if eq .Access "voice"}} selected{{end}}>Calls only</option>
          </select>
          <button class="btn small danger" type="submit" formaction="/agents/numbers/remove" formnovalidate name="number" value="{{$.Agent}}:{{.Number}}" data-confirm="Remove {{.Display}} from {{$.Name}}?">{{icon "x-ring"}}Remove</button>
        </div>
      </div>
      {{end}}
    </div>
    {{else}}
    <p class="callout">No number can reach {{.Name}} yet. Add the phone you text from.</p>
    {{end}}

    <div class="grid-2">
      <div class="field">
        <label for="{{.Agent}}-newNumber">Add a number</label>
        <input id="{{.Agent}}-newNumber" type="text" name="newNumber" placeholder="845 555 1234" autocomplete="off">
        <p class="hint">US or Canada, 10 digits. A leading +1 is fine.</p>
      </div>
      <div class="field">
        <label for="{{.Agent}}-newLabel">Name it (optional)</label>
        <input id="{{.Agent}}-newLabel" type="text" name="newLabel" placeholder="My phone" autocomplete="off">
      </div>
    </div>
    <div class="grid-2">
      <div class="field">
        <label for="{{.Agent}}-newAccess">What it may do</label>
        <select id="{{.Agent}}-newAccess" name="newAccess">
          <option value="all">Texts and calls</option>
          <option value="sms">Texts only</option>
          <option value="voice">Calls only</option>
        </select>
      </div>
      <div class="field">
        <label>&nbsp;</label>
        <button class="btn accent" type="submit" formaction="/agents/numbers/add" formnovalidate name="agent" value="{{.Agent}}">{{icon "check"}}Add to {{.Name}}</button>
      </div>
    </div>

    <div class="field">
      <label for="{{.Field "callerNames"}}">Allowed caller names</label>
      <textarea id="{{.Field "callerNames"}}" name="{{.Field "callerNames"}}" rows="2" placeholder="Jane Appleseed">{{.CallerNames}}</textarea>
      <p class="hint">Only for calls. Google Voice shows a contact name instead of a number when the caller is in your Google Contacts, and there is no number to match. Type the name exactly as it appears, one per line.</p>
    </div>
  </div>
</section>

<section class="card">
  <div class="card-head divided">
    <div><h2>Security code</h2><p>Make {{.Name}} refuse a text that does not start with its own code. Off unless you turn it on.</p></div>
  </div>
  <div class="card-body">
    <div class="toggle">
      <div class="label">Require a code for {{.Name}}<span>{{if .HasCode}}A code is set.{{else}}Set one below first.{{end}}</span></div>
      <label class="switch"><input type="checkbox" name="{{.Field "requireCode"}}" value="1"{{if .RequireCode}} checked{{end}}><span class="slider"></span></label>
    </div>
    <div class="field">
      <label for="{{.Field "code"}}">{{if .HasCode}}Replace the code{{else}}Set a code{{end}}</label>
      <input id="{{.Field "code"}}" type="password" name="{{.Field "code"}}" placeholder="{{if .HasCode}}Leave blank to keep the current code{{else}}At least 6 characters{{end}}" autocomplete="new-password">
      <p class="hint">Text it in front of your message: <b class="mono">yourcode {{.Prefix}}: check the build</b>. Each agent has its own.</p>
    </div>
  </div>
</section>

<section class="card">
  <div class="card-head divided">
    <div><h2>Replies from {{.Name}}</h2><p>What {{.Name}} texts back while it works.</p></div>
  </div>
  <div class="card-body">
    <div class="toggle">
      <div class="label">Confirm receipt<span>Texts "working on it" the moment a command is accepted.</span></div>
      <label class="switch"><input type="checkbox" name="{{.Field "ack"}}" value="1"{{if .Ack}} checked{{end}}><span class="slider"></span></label>
    </div>
    <div class="toggle">
      <div class="label">Progress texts<span>Sends a short "still working" line during a long turn.</span></div>
      <label class="switch"><input type="checkbox" name="{{.Field "progress"}}" value="1"{{if .Progress}} checked{{end}}><span class="slider"></span></label>
    </div>
    <div class="field">
      <label for="{{.Field "progressInterval"}}">How often</label>
      <select id="{{.Field "progressInterval"}}" name="{{.Field "progressInterval"}}">
        <option value="60"{{if eq .Interval 60}} selected{{end}}>Every minute</option>
        <option value="120"{{if eq .Interval 120}} selected{{end}}>Every 2 minutes</option>
        <option value="300"{{if eq .Interval 300}} selected{{end}}>Every 5 minutes</option>
        <option value="900"{{if eq .Interval 900}} selected{{end}}>Every 15 minutes</option>
      </select>
    </div>
  </div>
</section>
{{end}}

{{define "content"}}
<div class="page-head">
  <div>
    <h1>Agents</h1>
    <p>Codex and Claude each keep their own routing, workspace, access, conversation, and SMS instruction here.</p>
  </div>
  <div class="page-actions">
    <a class="btn" href="/activity?stage=agent">{{icon "clock"}}Agent activity</a>
  </div>
</div>


<input class="agent-switch" type="radio" name="agent-view" id="agent-codex" checked>
<input class="agent-switch" type="radio" name="agent-view" id="agent-claude">

<div class="agents-shell">
  <aside class="agent-rail">
    <div class="nav-label">Agents</div>
    <div class="agent-list">
      <label class="agent-item" for="agent-codex">
        <span class="bmark codex">{{brand "codex"}}</span>
        <span class="agent-item-copy">
          <b>Codex <span class="agent-chip{{if not .S.CodexCheck.Ready}} warn{{end}}">{{if not .S.CodexCheck.Known}}Untested{{else if .S.CodexCheck.OK}}Ready{{else}}Attention{{end}}</span></b>
          <span>Answers {{.S.CodexPrefix}}: messages</span>
        </span>
      </label>
      <label class="agent-item" for="agent-claude">
        <span class="bmark claude">{{brand "claude"}}</span>
        <span class="agent-item-copy">
          <b>Claude <span class="agent-chip{{if not .S.ClaudeCheck.Ready}} warn{{end}}">{{if not .S.ClaudeCheck.Known}}Untested{{else if .S.ClaudeCheck.OK}}Ready{{else}}Attention{{end}}</span></b>
          <span>Answers {{.S.ClaudePrefix}}: messages</span>
        </span>
      </label>
    </div>
  </aside>

  <div>
    <!-- ------------------------------ Codex ------------------------------ -->
    <section class="agent-pane" id="codex-pane">
      <form method="post" action="/agents/save">
        <div class="agent-head">
          <div class="agent-head-main">
            <span class="bmark lg codex">{{brand "codex"}}</span>
            <div>
              <h2>Codex
                <span class="pill {{if .S.CodexCheck.Ready}}ok{{else}}warn{{end}}">{{if not .S.CodexCheck.Known}}Not tested{{else if .S.CodexCheck.OK}}Ready{{else}}Needs attention{{end}}</span>
                {{if eq .S.DefaultAgent "C"}}<span class="pill brand">Default agent</span>{{end}}
              </h2>
              <p>Local Codex CLI signed in with ChatGPT{{if .S.CodexCheck.Known}} · last tested {{ago .S.CodexCheck.At}}{{end}}</p>
            </div>
          </div>
          <div class="agent-head-actions">
            <button class="btn" type="button" data-test="/codex/test" data-test-busy="Asking Codex">{{icon "play"}}Test</button>
            {{if not .CodexAccess.IsDefault}}<button class="btn" type="submit" name="defaultAgent" value="C" formnovalidate>{{icon "check"}}Make default</button>{{end}}
            <a class="btn" href="/open/folder?which=codex">{{icon "folder"}}Folder</a>
            <button class="btn primary" type="submit">Save Codex</button>
          </div>
        </div>

        <section class="card">
          <div class="card-head divided"><div><h2>Routing &amp; workspace</h2><p>How a text reaches Codex, and where Codex runs it.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="codexPrefix">SMS shortcut</label>
                <input id="codexPrefix" type="text" name="codexPrefix" value="{{.S.CodexPrefix}}" maxlength="24" required>
                <p class="hint">Text <b>{{.S.CodexPrefix}}: check the latest build</b> to reach Codex. Must differ from the Claude shortcut.</p>
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
            <div class="field">
              <label for="codexPath">Executable path</label>
              <div class="input-suffix">
                <div class="input-group">
                  <input id="codexPath" type="text" name="codexPath" value="{{.S.CodexPath}}" placeholder="codex">
                  <button class="btn" type="button" data-browse="#codexPath">Browse</button>
                </div>
                <span class="check {{if .S.CodexFound}}ok{{else}}bad{{end}}">{{if .S.CodexFound}}{{icon "check"}}{{else}}{{icon "x-ring"}}{{end}}</span>
              </div>
              <p class="hint">{{if .S.CodexFound}}Resolves to {{.S.CodexResolved}}{{else}}Not found — leave blank to search the usual install locations.{{end}}</p>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided">
            <div><h2>SMS instruction</h2><p>The one line FlipAi adds after your text, so Codex shapes its answer for a phone instead of a terminal.</p></div>
          </div>
          <div class="card-body">{{template "promptEditor" .CodexPrompt}}</div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>Conversation</h2><p>Codex keeps one SMS thread and releases it after each turn, so Codex Desktop can open the same history.</p></div></div>
          <div class="card-body">
            <div class="rows">
              <div class="row">
                <div class="label">Current thread<span>Resumed by the next Codex text.</span></div>
                <div class="value">{{if .S.CodexThreadActive}}<span class="pill ok">Active</span>{{else}}<span class="pill">None yet</span>{{end}}</div>
              </div>
              <div class="row">
                <div class="label">Start a clean thread<span>Send this from your phone whenever you want Codex to forget the conversation.</span></div>
                <div class="value"><b class="mono">{{.S.CodexPrefix}} {{.S.NewSessionCommand}}</b></div>
              </div>
              <div class="row">
                <div class="label">Reset it from here<span>Forgets the saved thread and restarts the bridge with a clean one.</span></div>
                <div class="value">
                  {{if .S.CodexThreadActive}}
                  <button class="btn small danger" type="submit" formaction="/agents/reset" formnovalidate name="agent" value="C" data-confirm="Reset the current Codex conversation? The next Codex text starts a clean thread.">{{icon "refresh"}}Reset thread</button>
                  {{else}}<span class="pill">Nothing to reset</span>{{end}}
                </div>
              </div>
            </div>
            <div class="field">
              <label for="codexProgressInterval">Progress texts while Codex works</label>
              <select id="codexProgressInterval" name="codexProgressInterval">
                <option value="0"{{if eq .S.CodexProgressInterval 0}} selected{{end}}>Follow the shared setting ({{.S.ProgressInterval}} sec)</option>
                <option value="60"{{if eq .S.CodexProgressInterval 60}} selected{{end}}>Every minute</option>
                <option value="120"{{if eq .S.CodexProgressInterval 120}} selected{{end}}>Every 2 minutes</option>
                <option value="300"{{if eq .S.CodexProgressInterval 300}} selected{{end}}>Every 5 minutes</option>
                <option value="900"{{if eq .S.CodexProgressInterval 900}} selected{{end}}>Every 15 minutes</option>
              </select>
              <p class="hint">Overrides the shared cadence on the Phone page for Codex turns only.</p>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>Access</h2><p>What a Codex text is allowed to do on this PC.</p></div></div>
          <div class="card-body">
            <div class="rows">
              <div class="row"><div class="label">Access level<span>SMS turns run with this Windows user's normal permissions — no Codex sandbox, no elevation.</span></div><div class="value"><span class="pill brand">Full user access</span></div></div>
              <div class="row"><div class="label">Sign-in</div><div class="value"><b>ChatGPT account via the Codex CLI</b></div></div>
              <div class="row"><div class="label">Last test</div><div class="value"><b>{{if .S.CodexCheck.Known}}{{ago .S.CodexCheck.At}}{{else}}Never{{end}}</b>{{if .S.CodexCheck.Detail}}<span>{{.S.CodexCheck.Detail}}</span>{{end}}</div></div>
            </div>
          </div>
        </section>

        {{template "agentAccess" .CodexAccess}}
      </form>
    </section>

    <!-- ------------------------------ Claude ----------------------------- -->
    <section class="agent-pane" id="claude-pane">
      <form method="post" action="/agents/save">
        <div class="agent-head">
          <div class="agent-head-main">
            <span class="bmark lg claude">{{brand "claude"}}</span>
            <div>
              <h2>Claude
                <span class="pill {{if .S.ClaudeCheck.Ready}}ok{{else}}warn{{end}}">{{if not .S.ClaudeCheck.Known}}Not tested{{else if .S.ClaudeCheck.OK}}Ready{{else}}Needs attention{{end}}</span>
                {{if eq .S.DefaultAgent "A"}}<span class="pill brand">Default agent</span>{{end}}
              </h2>
              <p>Claude Code CLI under your Claude subscription{{if .S.ClaudeCheck.Known}} · last tested {{ago .S.ClaudeCheck.At}}{{end}}</p>
            </div>
          </div>
          <div class="agent-head-actions">
            {{if not .S.ClaudeConnNeedsSignIn}}
            <button class="btn" type="submit" formaction="/claude/disconnect" formnovalidate data-confirm="Disconnect Claude? FlipAi will need connecting again before it can answer a text.">{{icon "x-ring"}}Disconnect</button>
            {{else}}
            <button class="btn accent" type="submit" formaction="/claude/connect" formnovalidate>{{icon "link"}}Connect</button>
            {{end}}
            <button class="btn" type="button" data-test="/claude/test" data-test-busy="Asking Claude">{{icon "play"}}Test</button>
            {{if not .ClaudeAccess.IsDefault}}<button class="btn" type="submit" name="defaultAgent" value="A" formnovalidate>{{icon "check"}}Make default</button>{{end}}
            <a class="btn" href="/open/folder?which=claude">{{icon "folder"}}Folder</a>
            <button class="btn primary" type="submit">Save Claude</button>
          </div>
        </div>

        <section class="card">
          <div class="card-head divided"><div><h2>Routing &amp; workspace</h2><p>How a text reaches Claude, and where Claude runs it.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="claudePrefix">SMS shortcut</label>
                <input id="claudePrefix" type="text" name="claudePrefix" value="{{.S.ClaudePrefix}}" maxlength="24" required>
                <p class="hint">Text <b>{{.S.ClaudePrefix}}: review this issue</b> to reach Claude. Must differ from the Codex shortcut.</p>
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
            <div class="field">
              <label for="claudePath">Executable path</label>
              <div class="input-suffix">
                <div class="input-group">
                  <input id="claudePath" type="text" name="claudePath" value="{{.S.ClaudePath}}" placeholder="claude">
                  <button class="btn" type="button" data-browse="#claudePath">Browse</button>
                </div>
                <span class="check {{if .S.ClaudeFound}}ok{{else}}bad{{end}}">{{if .S.ClaudeFound}}{{icon "check"}}{{else}}{{icon "x-ring"}}{{end}}</span>
              </div>
              <p class="hint">{{if .S.ClaudeFound}}Resolves to {{.S.ClaudeResolved}}{{else}}Not found — leave blank to search the usual install locations.{{end}}</p>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided">
            <div><h2>SMS instruction</h2><p>The one line FlipAi adds after your text, so Claude shapes its answer for a phone instead of a terminal.</p></div>
          </div>
          <div class="card-body">{{template "promptEditor" .ClaudePrompt}}</div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>Access &amp; tools</h2><p>Texting is unattended, so anything that waits for approval blocks the turn.</p></div></div>
          <div class="card-body">
            <div class="field">
              <label for="permissionMode">Permission mode</label>
              <select id="permissionMode" name="permissionMode">
                <option value="bypassPermissions"{{if eq .S.PermissionMode "bypassPermissions"}} selected{{end}}>Full user access (matches Codex)</option>
                <option value="dontAsk"{{if eq .S.PermissionMode "dontAsk"}} selected{{end}}>Never prompt (your Claude rules decide)</option>
                <option value="acceptEdits"{{if eq .S.PermissionMode "acceptEdits"}} selected{{end}}>Accept edits only (blocks Chrome)</option>
                <option value="plan"{{if eq .S.PermissionMode "plan"}} selected{{end}}>Plan only</option>
                <option value="default"{{if eq .S.PermissionMode "default"}} selected{{end}}>Ask (blocks unattended turns)</option>
              </select>
              <p class="hint">Currently effective: <b>{{.S.PermissionModeLabel}}</b>.</p>
            </div>
            <div class="toggle">
              <div class="label">Let Claude control Chrome<span>Passes --chrome so a text can use the browser exactly as Claude does at the desktop. Needs full user access; a narrower mode refuses the browser tools.</span></div>
              <label class="switch"><input type="hidden" name="claudeUseChrome" value="0"><input type="checkbox" name="claudeUseChrome" value="1"{{if .S.ClaudeUseChrome}} checked{{end}}><span class="slider"></span></label>
            </div>
            {{if .S.ChromeTokenNotice}}<p class="callout" style="margin-top:14px"><b>Chrome is off:</b> {{.S.ChromeTokenNotice}}</p>{{end}}
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>Conversation</h2><p>How each text reaches the Claude Code session, and how to reopen that session yourself.</p></div></div>
          <div class="card-body">
            <div class="grid-2">
              <div class="field">
                <label for="claudeSessionMode">Session mode</label>
                <select id="claudeSessionMode" name="claudeSessionMode">
                  <option value="print"{{if eq .S.ClaudeSessionMode "print"}} selected{{end}}>Per-message (recommended)</option>
                  <option value="live"{{if eq .S.ClaudeSessionMode "live"}} selected{{end}}>Live session with Remote Control</option>
                </select>
                <p class="hint">Per-message runs one request per text and needs nothing running in the background. Live keeps one session open for the whole conversation, so you can also open it at claude.ai/code.</p>
              </div>
              <div class="field">
                <label for="claudeProgressInterval">Progress texts while Claude works</label>
                <select id="claudeProgressInterval" name="claudeProgressInterval">
                  <option value="0"{{if eq .S.ClaudeProgressInterval 0}} selected{{end}}>Follow the shared setting ({{.S.ProgressInterval}} sec)</option>
                  <option value="60"{{if eq .S.ClaudeProgressInterval 60}} selected{{end}}>Every minute</option>
                  <option value="120"{{if eq .S.ClaudeProgressInterval 120}} selected{{end}}>Every 2 minutes</option>
                  <option value="300"{{if eq .S.ClaudeProgressInterval 300}} selected{{end}}>Every 5 minutes</option>
                  <option value="900"{{if eq .S.ClaudeProgressInterval 900}} selected{{end}}>Every 15 minutes</option>
                </select>
                <p class="hint">Overrides the shared cadence on the Phone page for Claude turns only.</p>
              </div>
            </div>
            {{if .S.LiveNotice}}<p class="callout" style="margin-top:14px"><b>Heads up:</b> {{.S.LiveNotice}}</p>{{end}}
            <div class="section-label">Session</div>
            <div class="rows">
              <div class="row">
                <div class="label">Running now</div>
                <div class="value">{{if .S.LiveActive}}<span class="pill ok">Live session</span>{{else}}<span class="pill">Per-message</span>{{end}}{{if .S.LiveRemoteControl}}<span class="pill brand">Remote Control</span>{{end}}</div>
              </div>
              <div class="row">
                <div class="label">Current session{{if .S.ClaudeSessionName}}<span>Named “{{.S.ClaudeSessionName}}”.</span>{{end}}</div>
                <div class="value">{{if .S.ClaudeSessionActive}}<span class="pill ok">Active</span><span class="agent-id">{{.S.ClaudeSessionID}}</span>{{else}}<span class="pill">None yet</span>{{end}}</div>
              </div>
              {{if .S.ClaudeLiveSessionID}}<div class="row"><div class="label">Live session id</div><div class="value"><span class="agent-id">{{.S.ClaudeLiveSessionID}}</span></div></div>{{end}}
              <div class="row">
                <div class="label">Start a clean session<span>Send this from your phone whenever you want Claude to forget the conversation.</span></div>
                <div class="value"><b class="mono">{{.S.ClaudePrefix}} {{.S.NewSessionCommand}}</b></div>
              </div>
              {{if .S.ClaudeSessionActive}}
              <div class="row"><div class="label">Reopen it in Claude Code<span>SMS sessions stay out of the interactive picker, so open this one by id.</span></div><div class="value"><b class="mono">claude --resume {{.S.ClaudeSessionID}}</b></div></div>
              <div class="row"><div class="label">Move it to Claude Desktop<span>Resume it as above, then type this. Claude saves the session and opens it in the desktop app.</span></div><div class="value"><b class="mono">/desktop</b></div></div>
              {{end}}
              <div class="row">
                <div class="label">Reset it from here<span>Forgets the saved session and prepares a clean named one for the next Claude text.</span></div>
                <div class="value">
                  {{if or .S.ClaudeSessionActive .S.ClaudeSessionName}}
                  <button class="btn small danger" type="submit" formaction="/agents/reset" formnovalidate name="agent" value="A" data-confirm="Reset the current Claude conversation? The next Claude text starts a clean session.">{{icon "refresh"}}Reset session</button>
                  {{else}}<span class="pill">Nothing to reset</span>{{end}}
                </div>
              </div>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-head divided"><div><h2>Connection</h2><p>Which credential FlipAi runs Claude with. This one fact decides whether a text can drive Chrome or reach claude.ai/code.</p></div></div>
          <div class="card-body">
            <div class="agent-stats">
              <div class="agent-stat"><label>Signed in with</label><b>{{.S.ClaudeConnLabel}}</b></div>
              <div class="agent-stat"><label>Chrome &amp; browser view</label>{{if .S.ClaudeConnChromeReady}}<span class="pill ok">Available</span>{{else if .S.ClaudeConnNeedsSignIn}}<span class="pill warn">Not available</span>{{else}}<span class="pill">Checking</span>{{end}}</div>
              <div class="agent-stat"><label>Saved fallback token</label>{{if .S.HasClaudeToken}}<span class="pill brand">Stored</span>{{else}}<span class="pill">None</span>{{end}}</div>
            </div>
            <p class="hint">{{.S.ClaudeConnDetail}}</p>
            <div class="inline-actions">
              <button class="btn{{if .S.ClaudeConnNeedsSignIn}} primary{{end}}" type="submit" formaction="/claude/connect" formnovalidate>{{icon "key"}}Connect Claude</button>
              <button class="btn" type="submit" formaction="/claude/connect/verify" formnovalidate>{{icon "refresh"}}Check connection</button>
              <button class="btn danger" type="submit" formaction="/claude/disconnect" formnovalidate data-confirm="Disconnect Claude from FlipAi? Your own Claude Code sign-in on this PC is left alone.">Disconnect</button>
            </div>
            <details class="disclosure">
              <summary>Long-lived token — fallback only</summary>
              <div class="disclosure-body">
                <div class="field">
                  <label for="claudeToken">Token from claude setup-token{{if .S.HasClaudeToken}} — saved{{end}}</label>
                  <input id="claudeToken" type="password" name="claudeToken" autocomplete="off" placeholder="{{if .S.HasClaudeToken}}leave blank to keep the saved token{{else}}paste the value from claude setup-token{{end}}">
                  <p class="hint">FlipAi runs on the sign-in above so Chrome and claude.ai/code work, and uses a saved token only if that sign-in lapses. A token on its own cannot drive the browser. Stored with Windows DPAPI.</p>
                </div>
                {{if .S.HasClaudeToken}}
                <div class="toggle">
                  <div class="label">Remove the saved token<span>Go back to the Claude Code sign-in alone.</span></div>
                  <label class="switch"><input type="hidden" name="clearClaudeToken" value="0"><input type="checkbox" name="clearClaudeToken" value="1"><span class="slider"></span></label>
                </div>
                {{end}}
              </div>
            </details>
          </div>
        </section>

        {{template "agentAccess" .ClaudeAccess}}
      </form>
    </section>

  </div>
</div>
{{end}}`

func (a *App) agentsPage(w http.ResponseWriter, r *http.Request) {
	s := a.status()
	view := agentsView{pageView: pageView{Shell: a.shell(r, "agents", "Agents"), S: s}}
	view.CodexPrompt = promptEditorView{
		Name: "codexReplyStyle", Title: "What Codex is told about SMS",
		Value: s.CodexReplyStyle, Fallback: s.DefaultReplyStyle, Custom: s.CodexReplyStyleCustom,
		Hint: "Leave empty to use the wording FlipAi ships with.", Max: s.ReplyStyleMaxChars,
	}
	view.ClaudePrompt = promptEditorView{
		Name: "claudeReplyStyle", Title: "What Claude is told about SMS",
		Value: s.ClaudeReplyStyle, Fallback: s.DefaultReplyStyle, Custom: s.ClaudeReplyStyleCustom,
		Hint: "Leave empty to use the wording FlipAi ships with.", Max: s.ReplyStyleMaxChars,
	}

	cfg := a.snapshotConfig()
	view.CodexAccess = newAgentAccessView(cfg, "C", s.CodexPrefix)
	view.ClaudeAccess = newAgentAccessView(cfg, "A", s.ClaudePrefix)
	a.render(w, "agents", view)
}

func newAgentAccessView(cfg Config, agent, prefix string) agentAccessView {
	settings := agentSettings(cfg, agent)
	name := "Codex"
	if agent == "A" {
		name = "Claude"
	}
	interval := settings.ProgressIntervalSeconds
	if interval <= 0 {
		interval = 120
	}
	return agentAccessView{
		Agent: agent, Name: name, Prefix: prefix,
		Phones: settings.Phones, CallerNames: settings.CallerNames,
		RequireCode: settings.RequireCode, HasCode: settings.CodeHash != "",
		Ack: settings.ackEnabled(), Progress: settings.progressEnabled(),
		Interval: interval, IsDefault: cfg.DefaultAgent == agent,
	}
}

// promptSourceLabel says, in three words, whether the agents are still sending
// the wording FlipAi ships with or something the user wrote.
func promptSourceLabel(s uiStatus) string {
	custom := 0
	if s.CodexReplyStyleCustom {
		custom++
	}
	if s.ClaudeReplyStyleCustom {
		custom++
	}
	switch {
	case custom == 2:
		return "Custom per agent"
	case custom == 1:
		return "One custom"
	case s.SharedReplyStyle != s.DefaultReplyStyle:
		return "Custom shared"
	default:
		return "FlipAi default"
	}
}

func foundLabel(ok bool) string {
	if ok {
		return "Found"
	}
	return "Not found"
}

// resetAgentConversation is the Agents-page reset control. It refuses to
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
