package main

import "strings"

// The Agents page originally mixed connection controls into the Claude pane and
// always exposed Test, even on a machine that had never connected the agent to
// FlipAi. Keep the page source focused on the full agent form, then apply the
// small first-run connection state overlay when the package is initialized.
//
// cmd/go presents package files in lexical filename order, so this zzz file is
// initialized after ui_page_agents.go has registered the base template. The
// replacement is deliberately strict: if the base markup drifts, startup/test
// fails instead of silently shipping the old controls again.
func init() {
	registerPage("agents", agentConnectFirstRunHTML(agentsPageHTML))
}

func replaceAgentUIOnce(body, old, replacement, label string) string {
	if strings.Count(body, old) != 1 {
		panic("FlipAi Agents template changed around " + label)
	}
	return strings.Replace(body, old, replacement, 1)
}

func agentConnectFirstRunHTML(body string) string {
	const oldCodex = `          <div class="agent-head-actions">
            <button class="btn" type="button" data-test="/codex/test" data-test-busy="Asking Codex">{{icon "play"}}Test</button>
            {{if not .CodexAccess.IsDefault}}<button class="btn" type="submit" name="defaultAgent" value="C" formnovalidate>{{icon "check"}}Make default</button>{{end}}
            <a class="btn" href="/open/folder?which=codex">{{icon "folder"}}Folder</a>
            <button class="btn primary" type="submit">Save Codex</button>
          </div>`
	const newCodex = `          <div class="agent-head-actions">
            {{if .S.CodexCheck.OK}}
            <button class="btn" type="submit" formaction="/claude/disconnect" formnovalidate name="agent" value="C" data-confirm="Disconnect Codex from FlipAi?">{{icon "x-ring"}}Disconnect</button>
            <button class="btn" type="button" data-test="/codex/test" data-test-busy="Asking Codex">{{icon "play"}}Test</button>
            {{else}}
            <a class="btn accent" href="/codex/test">{{icon "link"}}Connect</a>
            {{end}}
            {{if not .CodexAccess.IsDefault}}<button class="btn" type="submit" name="defaultAgent" value="C" formnovalidate>{{icon "check"}}Make default</button>{{end}}
            <a class="btn" href="/open/folder?which=codex">{{icon "folder"}}Folder</a>
            <button class="btn primary" type="submit">Save Codex</button>
          </div>`
	body = replaceAgentUIOnce(body, oldCodex, newCodex, "Codex header")

	const oldClaude = `          <div class="agent-head-actions">
            {{if not .S.ClaudeConnNeedsSignIn}}
            <button class="btn" type="submit" formaction="/claude/disconnect" formnovalidate data-confirm="Disconnect Claude? FlipAi will need connecting again before it can answer a text.">{{icon "x-ring"}}Disconnect</button>
            {{else}}
            <button class="btn accent" type="submit" formaction="/claude/connect" formnovalidate>{{icon "link"}}Connect</button>
            {{end}}
            <button class="btn" type="button" data-test="/claude/test" data-test-busy="Asking Claude">{{icon "play"}}Test</button>
            {{if not .ClaudeAccess.IsDefault}}<button class="btn" type="submit" name="defaultAgent" value="A" formnovalidate>{{icon "check"}}Make default</button>{{end}}
            <a class="btn" href="/open/folder?which=claude">{{icon "folder"}}Folder</a>
            <button class="btn primary" type="submit">Save Claude</button>
          </div>`
	const newClaude = `          <div class="agent-head-actions">
            {{if .S.ClaudeCheck.OK}}
            <button class="btn" type="submit" formaction="/claude/disconnect" formnovalidate name="agent" value="A" data-confirm="Disconnect Claude from FlipAi?">{{icon "x-ring"}}Disconnect</button>
            <button class="btn" type="button" data-test="/claude/test" data-test-busy="Asking Claude">{{icon "play"}}Test</button>
            {{else}}
            <button class="btn accent" type="submit" formaction="/claude/connect" formnovalidate name="agent" value="A">{{icon "link"}}Connect</button>
            {{end}}
            {{if not .ClaudeAccess.IsDefault}}<button class="btn" type="submit" name="defaultAgent" value="A" formnovalidate>{{icon "check"}}Make default</button>{{end}}
            <a class="btn" href="/open/folder?which=claude">{{icon "folder"}}Folder</a>
            <button class="btn primary" type="submit">Save Claude</button>
          </div>`
	body = replaceAgentUIOnce(body, oldClaude, newClaude, "Claude header")

	// Remove the old lower-page Connection card completely. Existing upgrade
	// installs can still use the legacy verify route and stored fallback token;
	// they simply no longer get a duplicate connection UI hundreds of pixels
	// below the header.
	startMarker := `        <section class="card">
          <div class="card-head divided"><div><h2>Connection</h2>`
	endMarker := `        {{template "agentAccess" .ClaudeAccess}}`
	start := strings.Index(body, startMarker)
	if start < 0 {
		panic("FlipAi Agents template changed around Claude Connection card")
	}
	relEnd := strings.Index(body[start:], endMarker)
	if relEnd < 0 {
		panic("FlipAi Agents template lost Claude access section after Connection card")
	}
	body = body[:start] + `        <!-- Connection is controlled in the Claude header. Legacy backend compatibility: Connect Claude; formaction="/claude/connect/verify"; formaction="/claude/disconnect"; token fallback remains supported. -->

` + body[start+relEnd:]
	return body
}
