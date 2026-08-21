package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type App struct {
	dataDir, configPath, statePath, tokenPath string
	mu                                        sync.Mutex
	cfg                                       Config
	mail                                      MailClient
	gmail                                     *GmailClient // OAuth backend only, for the OAuth browser flow.
	codex                                     *CodexClient
	claude                                    *ClaudeClient
	bridge                                    *Bridge
	oauth                                     *GoogleOAuthAttempt
	stop                                      func()
}

const setupHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>FlipAi — AI SMS Bridge</title>
<style>
:root{--bg:#f5f6fa;--surface:#fff;--surface2:#fafaff;--ink:#17151f;--muted:#6f6b7a;--line:#e7e4ee;--violet:#6c47ff;--violet2:#5435d8;--violetSoft:#f0ecff;--green:#18794e;--greenSoft:#eaf8f1;--amber:#9a6700;--amberSoft:#fff6df;--red:#b42318;--redSoft:#fff0ee;--shadow:0 18px 55px rgba(45,34,88,.08);--r:18px}
*{box-sizing:border-box}html{scroll-behavior:smooth}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif}button,input,select,textarea{font:inherit}.shell{min-height:100vh}.topbar{position:sticky;top:0;z-index:20;background:rgba(245,246,250,.92);backdrop-filter:blur(14px);border-bottom:1px solid rgba(231,228,238,.8)}.topbar-in{max-width:1180px;margin:auto;height:70px;padding:0 24px;display:flex;align-items:center;justify-content:space-between}.brand{display:flex;align-items:center;gap:11px;text-decoration:none;color:var(--ink)}.mark{width:38px;height:38px;border-radius:12px;background:var(--violet);color:#fff;display:grid;place-items:center;font-weight:800;box-shadow:0 8px 24px rgba(108,71,255,.28)}.brandtext b{display:block;font-size:16px;line-height:1.1}.brandtext span{color:var(--muted);font-size:12px}.trayhint{display:flex;align-items:center;gap:8px;color:var(--muted);font-size:12px}.dot{width:8px;height:8px;border-radius:50%;background:#22a06b;box-shadow:0 0 0 4px #e2f7ed}.wrap{max-width:1180px;margin:0 auto;padding:26px 24px 52px;display:grid;grid-template-columns:230px minmax(0,1fr);gap:26px}.side{position:sticky;top:96px;align-self:start}.sidebox{background:var(--surface);border:1px solid var(--line);border-radius:16px;padding:10px;box-shadow:var(--shadow)}.side a{display:flex;gap:10px;align-items:center;padding:10px 12px;border-radius:10px;text-decoration:none;color:#504b5a;font-weight:650}.side a:hover{background:#f4f1ff;color:var(--violet2)}.side .tiny{padding:12px;color:var(--muted);font-size:12px;border-top:1px solid var(--line);margin-top:8px}.content{min-width:0}.hero{background:linear-gradient(135deg,#1f163e 0%,#3d286e 52%,#6c47ff 100%);color:#fff;border-radius:26px;padding:30px;box-shadow:0 24px 70px rgba(56,35,111,.22);overflow:hidden;position:relative}.hero:after{content:"";position:absolute;right:-90px;top:-120px;width:310px;height:310px;border:1px solid rgba(255,255,255,.12);border-radius:50%;box-shadow:0 0 0 45px rgba(255,255,255,.03),0 0 0 90px rgba(255,255,255,.025)}.hero h1{font-size:34px;line-height:1.06;margin:0 0 10px;letter-spacing:-.8px;position:relative;z-index:1}.hero p{max-width:700px;margin:0;color:#ded5ff;font-size:15px;position:relative;z-index:1}.statusgrid{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin-top:24px;position:relative;z-index:1}.stat{background:rgba(255,255,255,.1);border:1px solid rgba(255,255,255,.14);border-radius:14px;padding:13px}.stat span{display:block;font-size:11px;color:#d4c9ff;text-transform:uppercase;letter-spacing:.08em}.stat b{display:block;margin-top:4px;font-size:14px}.card{background:var(--surface);border:1px solid var(--line);border-radius:var(--r);padding:22px;margin-top:18px;box-shadow:var(--shadow)}.cardhead{display:flex;justify-content:space-between;gap:18px;align-items:flex-start;margin-bottom:16px}.cardhead h2{margin:0;font-size:20px;letter-spacing:-.25px}.cardhead p{margin:4px 0 0;color:var(--muted)}.num{width:30px;height:30px;border-radius:10px;background:var(--violetSoft);color:var(--violet2);display:grid;place-items:center;font-weight:800;flex:0 0 auto}.heading{display:flex;gap:12px}.badge{display:inline-flex;align-items:center;gap:6px;padding:6px 9px;border-radius:999px;font-size:11px;font-weight:800;white-space:nowrap}.good{background:var(--greenSoft);color:var(--green)}.attention{background:var(--amberSoft);color:var(--amber)}.neutral{background:#f1eff5;color:#65606f}.bad{background:var(--redSoft);color:var(--red)}.successbar,.warnbar{border-radius:13px;padding:12px 14px;margin:14px 0;font-weight:650}.successbar{background:var(--greenSoft);color:var(--green)}.warnbar{background:var(--amberSoft);color:#765000}.progress{display:grid;grid-template-columns:repeat(5,1fr);gap:8px;margin:17px 0 0}.step{padding:10px;border-radius:11px;background:#f1eef8;color:#777080;font-size:11px;font-weight:750;text-align:center}.step.done{background:#ece7ff;color:var(--violet2)}.choicegrid{display:grid;grid-template-columns:1fr 1fr;gap:12px}.choice{display:block;border:1.5px solid var(--line);border-radius:15px;padding:15px;cursor:pointer;background:var(--surface);transition:.15s}.choice:hover{border-color:#cfc3ff}.choice:has(input:checked){border-color:var(--violet);box-shadow:0 0 0 3px var(--violetSoft);background:#fdfcff}.choice input{width:auto;margin:0 8px 0 0}.choice strong{font-size:14px}.choice small{display:block;color:var(--muted);margin:5px 0 0 24px}.methodbox{margin-top:14px;padding:16px;border-radius:14px;background:var(--surface2);border:1px solid var(--line)}.grid2{display:grid;grid-template-columns:1fr 1fr;gap:14px}.grid3{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}.field{margin-top:13px}.field label{display:flex;justify-content:space-between;gap:12px;font-weight:750;font-size:13px;margin-bottom:6px}.field label em{font-style:normal;color:var(--muted);font-weight:500;font-size:11px}.help{color:var(--muted);font-size:12px;margin:6px 0 0}.input,select,textarea,input[type=text],input[type=password],input[type=email],input[type=number],input[type=file]{width:100%;padding:11px 12px;border:1px solid #dcd8e5;border-radius:11px;background:#fff;color:var(--ink);outline:none;transition:.15s}textarea{min-height:98px;resize:vertical}input:focus,select:focus,textarea:focus{border-color:#9e87ff;box-shadow:0 0 0 3px #f0ecff}.checkrow{display:flex;align-items:flex-start;gap:10px;padding:11px 0}.checkrow input{margin-top:3px}.checkrow b{display:block}.checkrow span{display:block;color:var(--muted);font-size:12px;margin-top:2px}.actions{display:flex;flex-wrap:wrap;gap:9px;margin-top:17px}.btn,button{appearance:none;border:0;border-radius:11px;padding:10px 14px;text-decoration:none;cursor:pointer;font-weight:800;display:inline-flex;align-items:center;justify-content:center;gap:7px}.primary{background:var(--violet);color:#fff;box-shadow:0 7px 18px rgba(108,71,255,.22)}.primary:hover{background:var(--violet2)}.secondary{background:#f2eff8;color:#393342}.secondary:hover{background:#e9e4f4}.outline{background:#fff;border:1px solid var(--line);color:#3e3946}.dangerbtn{background:var(--redSoft);color:var(--red)}.full{width:100%}.codebox{padding:14px;border-radius:13px;background:#18151f;color:#f2edff;font:12px/1.7 ui-monospace,SFMono-Regular,Consolas,monospace;overflow:auto}.codebox .mutedcode{color:#a99fc1}.agentcards{display:grid;grid-template-columns:1fr 1fr;gap:12px}.agent{padding:16px;border:1px solid var(--line);border-radius:14px;background:#fff}.agenttop{display:flex;justify-content:space-between;gap:10px;align-items:center}.agent h3{margin:0;font-size:15px}.agent p{margin:7px 0 0;color:var(--muted);font-size:12px}.advanced{margin-top:14px;border-top:1px solid var(--line);padding-top:13px}.advanced summary{cursor:pointer;font-weight:750;color:#514a5d}.footer-note{margin-top:16px;color:var(--muted);font-size:12px;text-align:center}.saved{animation:fade 4s both}@keyframes fade{0%,75%{opacity:1}100%{opacity:.2}}@media(max-width:900px){.wrap{grid-template-columns:1fr}.side{display:none}.statusgrid,.progress{grid-template-columns:1fr 1fr}.choicegrid,.grid2,.agentcards{grid-template-columns:1fr}}@media(max-width:560px){.topbar-in,.wrap{padding-left:14px;padding-right:14px}.hero{padding:22px;border-radius:20px}.hero h1{font-size:28px}.statusgrid,.progress,.grid3{grid-template-columns:1fr}.card{padding:17px}.trayhint{display:none}.cardhead{align-items:center}.actions>.btn,.actions>button,.actions>form{width:100%}.actions form button{width:100%}}
</style></head>
<body><div class="shell"><header class="topbar"><div class="topbar-in"><a class="brand" href="/"><div class="mark">F</div><div class="brandtext"><b>FlipAi</b><span>AI SMS Bridge</span></div></a><div class="trayhint"><span class="dot"></span> Background bridge stays running when this page closes</div></div></header>
<div class="wrap"><aside class="side"><div class="sidebox"><a href="#overview">Overview</a><a href="#gmail">Gmail</a><a href="#security">Phone security</a><a href="#agents">Agents</a><a href="#startup">Startup</a><a href="#diagnostics">Diagnostics</a><div class="tiny">v{{.Version}} · Local-only control UI<br>127.0.0.1</div></div></aside>
<main class="content"><section class="hero" id="overview"><h1>Text your AI. Let your PC do the work.</h1><p>Google Voice forwards your SMS to Gmail. FlipAi verifies the sender and security code, then routes the command to Codex or Claude on this Windows account.</p><div class="statusgrid"><div class="stat"><span>Bridge</span><b>{{if .BridgeRunning}}Running{{else}}Waiting for setup{{end}}</b></div><div class="stat"><span>Gmail</span><b>{{if .GmailReady}}Connected{{else}}Not ready{{end}}</b></div><div class="stat"><span>Phone security</span><b>{{if .SecurityReady}}Protected{{else}}Needs setup{{end}}</b></div><div class="stat"><span>Last agent</span><b>{{if .LastAgent}}{{.LastAgent}}{{else}}None yet{{end}}</b></div></div></section>
{{if .Saved}}<div class="successbar saved">✓ Settings saved. The background host is restarting with the new configuration.</div>{{end}}
<section class="card"><div class="cardhead"><div class="heading"><div class="num">✓</div><div><h2>Setup progress</h2><p>Everything stays local to this Windows user. No administrator rights are required.</p></div></div>{{if .SetupComplete}}<span class="badge good">Ready</span>{{else}}<span class="badge attention">Finish setup</span>{{end}}</div><div class="progress"><div class="step done">App running</div><div class="step {{if .GmailReady}}done{{end}}">Gmail</div><div class="step {{if .SecurityReady}}done{{end}}">Phone security</div><div class="step">Agents</div><div class="step">Startup</div></div></section>
<form action="/setup/save" method="post" enctype="multipart/form-data">
<section class="card" id="gmail"><div class="cardhead"><div class="heading"><div class="num">1</div><div><h2>Connect Gmail</h2><p>Choose one method. There is no default and nothing routes through the project author.</p></div></div>{{if .GmailReady}}<span class="badge good">{{.GmailMethodLabel}}</span>{{else if .GmailMethod}}<span class="badge attention">Incomplete</span>{{else}}<span class="badge neutral">Not selected</span>{{end}}</div>
<div class="choicegrid"><label class="choice"><input type="radio" name="gmailMethod" value="app_password" {{if eq .GmailMethod "app_password"}}checked{{end}} onchange="toggleGmail()"><strong>App Password</strong><small>Fastest setup. Gmail IMAP IDLE wakes FlipAi almost immediately.</small></label><label class="choice"><input type="radio" name="gmailMethod" value="oauth" {{if eq .GmailMethod "oauth"}}checked{{end}} onchange="toggleGmail()"><strong>Your Google API project</strong><small>Use your own OAuth Desktop client and Gmail API access.</small></label></div>
<div id="appPasswordBox" class="methodbox"><div class="grid2"><div class="field"><label>Gmail address</label><input type="email" name="gmailEmail" value="{{.GmailEmail}}" placeholder="you@gmail.com"></div><div class="field"><label>Google App Password <em>{{if .HasAppPassword}}saved securely — leave blank to keep{{else}}16 characters{{end}}</em></label><input type="password" name="appPassword" autocomplete="new-password" placeholder="xxxx xxxx xxxx xxxx"></div></div><p class="help">Requires Google 2-Step Verification. The App Password is encrypted for this Windows user with DPAPI. FlipAi reads through IMAP/IDLE and uses SMTP only as the reply fallback.</p></div>
<div id="oauthBox" class="methodbox"><div class="field"><label>OAuth Desktop credentials JSON <em>{{if .HasCredentials}}credentials file already saved{{else}}required{{end}}</em></label><input type="file" name="credentials" accept="application/json,.json"></div><p class="help">Create a Google Cloud project, enable Gmail API, create an OAuth <b>Desktop app</b>, then upload its JSON. FlipAi stores the OAuth token locally with DPAPI.</p></div>
<div class="actions"><button class="primary" type="submit">Save Gmail & settings</button>{{if eq .GmailMethod "oauth"}}{{if .HasCredentials}}<a class="btn secondary" href="/oauth/google/start">Connect Google account</a>{{end}}{{end}}{{if .GmailMethod}}<a class="btn outline" href="/gmail/test">Test Gmail</a>{{end}}</div></section>
<section class="card" id="security"><div class="cardhead"><div class="heading"><div class="num">2</div><div><h2>Phone security</h2><p>Only exact numbers on this list can trigger computer actions.</p></div></div>{{if .SecurityReady}}<span class="badge good">{{.AllowedCount}} allowed</span>{{else}}<span class="badge attention">Required</span>{{end}}</div>
<div class="field"><label>Allowed SMS phone numbers <em>one per line</em></label><textarea name="allowedFrom" placeholder="8455551234&#10;2125551212" required>{{.AllowedFrom}}</textarea><p class="help">US numbers are normalized to 10 digits. FlipAi authorizes against the sender encoded by Google Voice—not a number merely written inside the message body.</p></div><div class="field"><label>SMS security code <em>{{if .HasSecurity}}already set — leave blank to keep{{else}}6+ characters, no spaces{{end}}</em></label><input type="password" name="securityCode" autocomplete="new-password" placeholder="Private code required at the start of every text" {{if not .HasSecurity}}required{{end}}><p class="help">Stored only as a salted, iterated hash. Example: <b>482913 C: check GitHub</b>.</p></div></section>
<section class="card" id="agents"><div class="cardhead"><div class="heading"><div class="num">3</div><div><h2>Choose your agents</h2><p>FlipAi uses your local Codex and Claude subscription logins—not OpenAI or Anthropic API billing.</p></div></div><span class="badge neutral">Local agents</span></div>
<div class="agentcards"><div class="agent"><div class="agenttop"><h3>Codex</h3><span class="badge neutral">C:</span></div><p>Uses the local Codex App Server. FlipAi requires <b>Sign in with ChatGPT</b> and rejects API/provider auth.</p><div class="actions"><a class="btn outline" href="/codex/test">Test Codex</a></div></div><div class="agent"><div class="agenttop"><h3>Claude</h3><span class="badge neutral">A:</span></div><p>Uses Claude Code under your Claude subscription login. API environment variables are stripped before launch.</p><div class="actions"><a class="btn outline" href="/claude/test">Test Claude</a></div></div></div>
<div class="grid2"><div class="field"><label>Default agent</label><select name="defaultAgent"><option value="C" {{if eq .DefaultAgent "C"}}selected{{end}}>Codex (C:)</option><option value="A" {{if eq .DefaultAgent "A"}}selected{{end}}>Claude (A:)</option></select><p class="help">Explicit <b>C:</b> or <b>A:</b> always overrides the default.</p></div><div class="field"><label>Working folder</label><input type="text" name="cwd" value="{{.Cwd}}"><p class="help">The starting folder given to local agent processes.</p></div></div>
<details class="advanced"><summary>Advanced agent paths & reply behavior</summary><div class="grid2"><div class="field"><label>Codex executable</label><input type="text" name="codexPath" value="{{.CodexPath}}" placeholder="codex"></div><div class="field"><label>Claude executable</label><input type="text" name="claudePath" value="{{.ClaudePath}}" placeholder="claude"></div></div><div class="grid2"><div class="field"><label>Reply maximum characters</label><input type="number" name="replyMaxChars" min="80" max="1000" value="{{.ReplyMaxChars}}"></div></div><label class="checkrow"><input type="checkbox" name="sendBrowserReply" value="1" {{if .SendBrowserReply}}checked{{end}}><span><b>Ask the agent to reply in Google Voice</b><span>When browser/computer tools are available, the agent is instructed to open Google Voice and reply to the exact authenticated sender.</span></span></label><label class="checkrow"><input type="checkbox" name="gmailFallback" value="1" {{if .GmailFallback}}checked{{end}}><span><b>Use Gmail reply fallback</b><span>If the agent cannot confirm a browser reply, FlipAi replies through the authenticated Google Voice email Reply-To address.</span></span></label></details>
<div class="actions"><button class="primary" type="submit">Save all settings</button></div></section></form>
<section class="card" id="startup"><div class="cardhead"><div class="heading"><div class="num">4</div><div><h2>Start with Windows</h2><p>Install for this Windows user only. No UAC prompt, service, scheduled task, driver, or Program Files write.</p></div></div><span class="badge neutral">No admin</span></div><div class="codebox"><span class="mutedcode">Installed copy</span>  %LOCALAPPDATA%\Programs\AISMSBridge\AISMSBridge.exe<br><span class="mutedcode">Startup</span>         HKCU\Software\Microsoft\Windows\CurrentVersion\Run<br><span class="mutedcode">Starts</span>          after this user signs into Windows</div><div class="actions"><form action="/install" method="post"><button class="primary" type="submit">Install & start with Windows</button></form><form action="/startup/remove" method="post"><button class="secondary" type="submit">Disable startup</button></form></div><p class="help">The bridge continues while Windows is locked. Sleep or hibernate pauses it until Windows wakes. Some managed PCs can block user-profile executables with AppLocker/WDAC; FlipAi will report the failure instead of requesting elevation.</p></section>
<section class="card" id="diagnostics"><div class="cardhead"><div class="heading"><div class="num">5</div><div><h2>Ready to text</h2><p>Closing this page does not stop FlipAi. The tray icon is the persistent control.</p></div></div>{{if .SetupComplete}}<span class="badge good">Configured</span>{{else}}<span class="badge attention">Setup incomplete</span>{{end}}</div><div class="grid2"><div><div class="codebox">YOURCODE C: check GitHub and fix the build<br>YOURCODE A: check Gmail and summarize today<br>YOURCODE STATUS</div></div><div><div class="codebox">C: → Codex<br>A: → Claude<br>No prefix → {{.DefaultAgentName}}<br>Reply → exact authenticated sender</div></div></div><details class="advanced"><summary>Technical status</summary><pre class="codebox">{{.Status}}</pre></details><div class="actions"><a class="btn outline" href="/gmail/test">Test Gmail</a><a class="btn outline" href="/codex/test">Test Codex</a><a class="btn outline" href="/claude/test">Test Claude</a><form action="/quit" method="post"><button class="dangerbtn" type="submit">Quit FlipAi completely</button></form></div><div class="footer-note">Tray → Open Settings reopens this page. Tray → Quit stops the tray, host, and watchdog.</div></section>
</main></div></div>
<script>function toggleGmail(){const v=document.querySelector('input[name="gmailMethod"]:checked')?.value||'';document.getElementById('appPasswordBox').style.display=v==='app_password'?'block':'none';document.getElementById('oauthBox').style.display=v==='oauth'?'block':'none'}toggleGmail();</script></body></html>`

const resultHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} — FlipAi</title><style>body{margin:0;background:#f5f6fa;color:#17151f;font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}.box{max-width:610px;margin:10vh auto;padding:24px}.card{background:#fff;border:1px solid #e7e4ee;border-radius:22px;padding:28px;box-shadow:0 20px 60px rgba(45,34,88,.09)}.mark{width:42px;height:42px;border-radius:13px;display:grid;place-items:center;font-weight:900;margin-bottom:16px}.ok{background:#eaf8f1;color:#18794e}.bad{background:#fff0ee;color:#b42318}.neutral{background:#f0ecff;color:#5435d8}h1{font-size:24px;margin:0 0 8px}p{color:#625d6b;margin:0 0 18px;white-space:pre-wrap}.btn{display:inline-block;background:#6c47ff;color:#fff;text-decoration:none;border-radius:11px;padding:10px 14px;font-weight:800}.sub{margin-top:18px;font-size:12px;color:#8a8591}</style></head><body><div class="box"><div class="card"><div class="mark {{.Class}}">{{.Icon}}</div><h1>{{.Title}}</h1><p>{{.Message}}</p><a class="btn" href="/">Back to FlipAi</a><div class="sub">Closing this page never stops the background bridge.</div></div></div></body></html>`

type pageData struct {
	GmailReady, HasCredentials, HasAppPassword, HasSecurity bool
	SetupComplete, SecurityReady, BridgeRunning, Busy, Saved bool
	GmailMethod, GmailMethodLabel, GmailEmail                  string
	AllowedFrom, CodexPath, ClaudePath, Cwd                    string
	DefaultAgent, DefaultAgentName, Status, LastAgent, Version string
	AllowedCount, ReplyMaxChars                                int
	SendBrowserReply, GmailFallback                            bool
}

type resultData struct{ Title, Message, Class, Icon string }

func gmailMethodLabel(v string) string {
	switch v {
	case GmailMethodAppPassword:
		return "App Password"
	case GmailMethodOAuth:
		return "Google OAuth"
	default:
		return "Not selected"
	}
}

func renderResult(w http.ResponseWriter, status int, ok bool, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	d := resultData{Title: title, Message: message, Class: "neutral", Icon: "i"}
	if ok {
		d.Class, d.Icon = "ok", "✓"
	} else if status >= 400 {
		d.Class, d.Icon = "bad", "!"
	}
	_ = template.Must(template.New("result").Parse(resultHTML)).Execute(w, d)
}

func (a *App) authorized(r *http.Request) bool {
	c, e := r.Cookie("aisms_session")
	return e == nil && c.Value == a.cfg.LocalToken
}
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(r) {
			http.Error(w, "Open FlipAi from AISMSBridge.exe or the tray icon.", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
func (a *App) page(w http.ResponseWriter, r *http.Request) {
	if t := r.URL.Query().Get("token"); t != "" && t == a.cfg.LocalToken {
		http.SetCookie(w, &http.Cookie{Name: "aisms_session", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !a.authorized(r) {
		http.Error(w, "Open FlipAi from AISMSBridge.exe or the tray icon.", http.StatusForbidden)
		return
	}
	a.mu.Lock()
	cfg := a.cfg
	mc := a.mail
	b := a.bridge
	a.mu.Unlock()
	gmailReady := mc != nil && mc.Authorized()
	allowed, _ := normalizeAllowedPhoneList(cfg.GoogleVoice.AllowedFrom)
	securityReady := cfg.Security.CodeHash != "" && len(allowed) > 0
	bridgeRunning, busy, lastAgent := false, false, ""
	st := map[string]any{"version": version, "backgroundHost": true, "gmailMethod": cfg.Gmail.Method, "gmailConfigured": gmailReady}
	if b != nil {
		b.mu.Lock()
		bridgeRunning, busy, lastAgent = true, b.busy, b.state.LastAgent
		st["running"] = true
		st["busy"] = b.busy
		st["codexThreadActive"] = b.state.CodexThreadID != ""
		st["claudeSessionActive"] = b.state.ClaudeSessionID != ""
		st["lastAgent"] = b.state.LastAgent
		b.mu.Unlock()
	} else {
		st["running"] = false
	}
	js, _ := json.MarshalIndent(st, "", "  ")
	defaultName := "Codex"
	if cfg.DefaultAgent == "A" {
		defaultName = "Claude"
	}
	d := pageData{
		GmailReady: gmailReady, HasCredentials: fileExists(cfg.Gmail.CredentialsFile), HasAppPassword: hasAppPasswordSecret(appPasswordPath(a.dataDir)), HasSecurity: cfg.Security.CodeHash != "",
		SetupComplete: gmailReady && securityReady, SecurityReady: securityReady, BridgeRunning: bridgeRunning, Busy: busy, Saved: r.URL.Query().Get("saved") == "1",
		GmailMethod: cfg.Gmail.Method, GmailMethodLabel: gmailMethodLabel(cfg.Gmail.Method), GmailEmail: cfg.Gmail.Email,
		AllowedFrom: cfg.GoogleVoice.AllowedFrom, CodexPath: cfg.CodexPath, ClaudePath: cfg.ClaudePath, Cwd: cfg.Cwd,
		DefaultAgent: cfg.DefaultAgent, DefaultAgentName: defaultName, Status: string(js), LastAgent: lastAgent, Version: version,
		AllowedCount: len(allowed), ReplyMaxChars: cfg.GoogleVoice.ReplyMaxChars, SendBrowserReply: cfg.GoogleVoice.SendReplyViaAgentBrowser, GmailFallback: cfg.GoogleVoice.GmailReplyFallback,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = template.Must(template.New("setup").Parse(setupHTML)).Execute(w, d)
}
func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }

func (a *App) resetMailCheckpoint() {
	s := loadState(a.statePath)
	s.GmailBaselineUnix = 0
	s.ProcessedMessageIDs = nil
	s.LastMessageID = ""
	_ = saveState(a.statePath, s)
}

func (a *App) saveSetup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		renderResult(w, 400, false, "Could not read settings", err.Error())
		return
	}
	a.mu.Lock()
	cfg := a.cfg
	oldMethod, oldEmail := cfg.Gmail.Method, cfg.Gmail.Email
	a.mu.Unlock()
	method := strings.TrimSpace(r.FormValue("gmailMethod"))
	oauthCredentialsChanged := false
	if method != "" && method != GmailMethodAppPassword && method != GmailMethodOAuth {
		renderResult(w, 400, false, "Choose a Gmail method", "Select App Password or your own Google API/OAuth project.")
		return
	}
	cfg.Gmail.Method = method
	allowedNumbers, err := normalizeAllowedPhoneList(r.FormValue("allowedFrom"))
	if err != nil {
		renderResult(w, 400, false, "Phone list needs attention", err.Error())
		return
	}
	cfg.GoogleVoice.AllowedFrom = strings.Join(allowedNumbers, "\n")
	cfg.GoogleVoice.ReplyTo = "" // Always derive destination from the authenticated incoming Voice message.
	cfg.CodexPath = strings.TrimSpace(r.FormValue("codexPath"))
	cfg.ClaudePath = strings.TrimSpace(r.FormValue("claudePath"))
	cfg.Cwd = strings.TrimSpace(r.FormValue("cwd"))
	cfg.DefaultAgent = strings.ToUpper(strings.TrimSpace(r.FormValue("defaultAgent")))
	if cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {
		cfg.DefaultAgent = "C"
	}
	if v := strings.TrimSpace(r.FormValue("replyMaxChars")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 80 || n > 1000 {
			renderResult(w, 400, false, "Reply length is invalid", "Choose a reply maximum between 80 and 1000 characters.")
			return
		}
		cfg.GoogleVoice.ReplyMaxChars = n
	}
	cfg.GoogleVoice.SendReplyViaAgentBrowser = r.FormValue("sendBrowserReply") == "1"
	cfg.GoogleVoice.GmailReplyFallback = r.FormValue("gmailFallback") == "1"
	if code := strings.TrimSpace(r.FormValue("securityCode")); code != "" {
		if err := setSecurityCode(&cfg, code); err != nil {
			renderResult(w, 400, false, "Security code is invalid", err.Error())
			return
		}
	}
	if cfg.Security.CodeHash == "" {
		renderResult(w, 400, false, "Set an SMS security code", "A security code is required before any SMS can trigger an agent.")
		return
	}
	if method == GmailMethodAppPassword {
		email, err := normalizeGmailAddress(r.FormValue("gmailEmail"))
		if err != nil {
			renderResult(w, 400, false, "Gmail address is invalid", err.Error())
			return
		}
		cfg.Gmail.Email = email
		pass := strings.TrimSpace(r.FormValue("appPassword"))
		secretFile := appPasswordPath(a.dataDir)
		if pass != "" {
			if err := saveAppPasswordSecret(secretFile, email, pass); err != nil {
				renderResult(w, 400, false, "App Password was not saved", err.Error())
				return
			}
		} else {
			existing, err := loadAppPasswordSecret(secretFile)
			if err != nil {
				renderResult(w, 400, false, "App Password required", "Enter the Google App Password the first time you choose App Password.")
				return
			}
			if !strings.EqualFold(existing.Email, email) {
				renderResult(w, 400, false, "Gmail account changed", "Enter a new App Password for the new Gmail address.")
				return
			}
		}
	} else if method == GmailMethodOAuth {
		cfg.Gmail.Email = ""
		if oldMethod != GmailMethodOAuth {
			_ = os.Remove(a.tokenPath)
		}
		uploaded := false
		if f, h, e := r.FormFile("credentials"); e == nil {
			defer f.Close()
			uploaded, oauthCredentialsChanged = true, true
			if !strings.HasSuffix(strings.ToLower(h.Filename), ".json") {
				renderResult(w, 400, false, "Wrong OAuth file", "Upload the JSON file created for a Google OAuth Desktop app.")
				return
			}
			bb, err := io.ReadAll(io.LimitReader(f, 2<<20))
			if err != nil {
				renderResult(w, 400, false, "Could not read OAuth file", err.Error())
				return
			}
			var cr googleCredentials
			if json.Unmarshal(bb, &cr) != nil || cr.Installed.ClientID == "" {
				renderResult(w, 400, false, "OAuth file is not a Desktop app", "Create OAuth credentials with application type Desktop app and upload that JSON.")
				return
			}
			if err := os.WriteFile(cfg.Gmail.CredentialsFile, bb, 0600); err != nil {
				renderResult(w, 500, false, "Could not store OAuth file", err.Error())
				return
			}
			_ = os.Remove(a.tokenPath)
		}
		if !uploaded && !fileExists(cfg.Gmail.CredentialsFile) {
			renderResult(w, 400, false, "OAuth credentials required", "Upload your Google OAuth Desktop credentials JSON.")
			return
		}
	}
	if err := saveConfig(a.configPath, cfg); err != nil {
		renderResult(w, 500, false, "Could not save settings", err.Error())
		return
	}
	if oldMethod != cfg.Gmail.Method || oauthCredentialsChanged || (cfg.Gmail.Method == GmailMethodAppPassword && !strings.EqualFold(oldEmail, cfg.Gmail.Email)) {
		a.resetMailCheckpoint()
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	http.Redirect(w, r, "/?saved=1", http.StatusSeeOther)
	go a.restartSoon()
}
func (a *App) restartSoon() {
	time.Sleep(1400 * time.Millisecond)
	if a.stop != nil {
		a.stop()
	}
}
func (a *App) oauthStart(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	g, cfg := a.gmail, a.cfg
	a.mu.Unlock()
	if cfg.Gmail.Method != GmailMethodOAuth {
		renderResult(w, 400, false, "OAuth is not selected", "Choose your own Google API project / Gmail OAuth first.")
		return
	}
	if g == nil {
		renderResult(w, 400, false, "OAuth credentials missing", "Upload Google Desktop OAuth credentials first.")
		return
	}
	redirect := "http://" + cfg.Listen + "/oauth/google/callback"
	u, attempt, err := g.AuthURL(redirect)
	if err != nil {
		renderResult(w, 500, false, "Could not start Google sign-in", err.Error())
		return
	}
	a.mu.Lock()
	a.oauth = &attempt
	a.mu.Unlock()
	http.Redirect(w, r, u, http.StatusFound)
}
func (a *App) oauthCallback(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	g, att := a.gmail, a.oauth
	a.mu.Unlock()
	if e := r.URL.Query().Get("error"); e != "" {
		renderResult(w, 400, false, "Google sign-in was not completed", e)
		return
	}
	if g == nil || att == nil || r.URL.Query().Get("state") != att.State {
		renderResult(w, 400, false, "Google sign-in could not be verified", "The OAuth state did not match. Start Connect Google account again from FlipAi.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := g.ExchangeCode(ctx, r.URL.Query().Get("code"), *att); err != nil {
		renderResult(w, 500, false, "Gmail connection failed", err.Error())
		return
	}
	a.mu.Lock()
	a.oauth = nil
	a.mu.Unlock()
	renderResult(w, 200, true, "Gmail connected", "Google OAuth completed successfully. The background bridge will restart automatically with the new Gmail connection.")
	go a.restartSoon()
}
func (a *App) gmailTest(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg.Gmail.Method == "" {
		renderResult(w, 400, false, "Choose a Gmail method first", "Select App Password or your own Google API/OAuth project, then save settings.")
		return
	}
	mc, _, err := buildConfiguredMailClient(cfg.Gmail, a.dataDir, a.tokenPath)
	if err != nil {
		renderResult(w, 400, false, "Gmail setup is incomplete", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := mc.Test(ctx); err != nil {
		renderResult(w, 500, false, "Gmail test failed", err.Error())
		return
	}
	renderResult(w, 200, true, "Gmail is working", gmailMethodLabel(cfg.Gmail.Method)+" connected successfully. FlipAi can access the mailbox with the selected method.")
}
func (a *App) codexTest(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c := NewCodexClient(cfg.CodexPath, cfg.Cwd)
	if err := c.Start(ctx); err != nil {
		renderResult(w, 500, false, "Codex could not start", err.Error()+"\n\nOpen Codex on this Windows account and verify the executable path in Advanced agent paths.")
		return
	}
	defer c.Close()
	raw, err := c.Account(ctx)
	if err != nil {
		renderResult(w, 500, false, "Codex account check failed", err.Error())
		return
	}
	var v struct {
		Account *struct{ Type string `json:"type"` } `json:"account"`
		Requires bool `json:"requiresOpenaiAuth"`
	}
	_ = json.Unmarshal(raw, &v)
	if v.Requires || v.Account == nil {
		renderResult(w, 400, false, "Codex is not signed in", "Open Codex on this Windows account, choose Sign in with ChatGPT, then test again.")
		return
	}
	if strings.ToLower(v.Account.Type) != "chatgpt" {
		renderResult(w, 400, false, "Codex API billing is refused", "FlipAi detected a non-ChatGPT Codex login. Sign in with ChatGPT so the bridge uses your ChatGPT subscription instead of API/provider billing.")
		return
	}
	renderResult(w, 200, true, "Codex is ready", "ChatGPT-managed Codex login detected. C: messages can be routed to Codex.")
}
func (a *App) claudeTest(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c := NewClaudeClient(cfg.ClaudePath, cfg.Cwd, cfg.Claude)
	if err := c.Test(ctx); err != nil {
		renderResult(w, 500, false, "Claude is not ready", err.Error()+"\n\nOpen Claude Code on this Windows account and sign in with your Claude subscription, then test again.")
		return
	}
	renderResult(w, 200, true, "Claude is ready", "Claude Code subscription authentication is available. A: messages can be routed to Claude.")
}
func (a *App) install(w http.ResponseWriter, r *http.Request) {
	dst, err := copySelfInstall()
	if err == nil {
		err = installAutostart(dst)
	}
	if err != nil {
		renderResult(w, 500, false, "Could not enable startup", err.Error()+"\n\nFlipAi did not request administrator rights. On a managed PC, AppLocker/WDAC or endpoint policy may block user-profile applications or HKCU startup entries.")
		return
	}
	renderResult(w, 200, true, "Start with Windows is enabled", "FlipAi was installed for this Windows user only. No administrator rights were requested.\n\nInstalled at: "+dst)
}
func (a *App) removeStartup(w http.ResponseWriter, r *http.Request) {
	if err := uninstallAutostart(); err != nil {
		renderResult(w, 500, false, "Could not disable startup", err.Error())
		return
	}
	renderResult(w, 200, true, "Windows startup disabled", "FlipAi will keep running now, but it will not start automatically the next time this Windows user signs in.")
}
func (a *App) quit(w http.ResponseWriter, r *http.Request) {
	requestQuit(a.dataDir, "settings quit")
	renderResult(w, 200, true, "FlipAi is stopping", "The tray, background host, and watchdog are being stopped completely. Launch AISMSBridge.exe again whenever you want to reconnect.")
	if a.stop != nil {
		go func() { time.Sleep(600 * time.Millisecond); a.stop() }()
	}
}
func (a *App) handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/", a.page)
	m.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"version":%q}`, version)
	})
	m.HandleFunc("/oauth/google/callback", a.oauthCallback)
	m.HandleFunc("/setup/save", a.requireAuth(a.saveSetup))
	m.HandleFunc("/oauth/google/start", a.requireAuth(a.oauthStart))
	m.HandleFunc("/gmail/test", a.requireAuth(a.gmailTest))
	m.HandleFunc("/codex/test", a.requireAuth(a.codexTest))
	m.HandleFunc("/claude/test", a.requireAuth(a.claudeTest))
	m.HandleFunc("/install", a.requireAuth(a.install))
	m.HandleFunc("/startup/remove", a.requireAuth(a.removeStartup))
	m.HandleFunc("/quit", a.requireAuth(a.quit))
	return m
}

func (a *App) startBridge(ctx context.Context) {
	a.mu.Lock()
	cfg, mc := a.cfg, a.mail
	a.mu.Unlock()
	if cfg.Gmail.Method == "" {
		log.Printf("Gmail connection method not selected; background host is alive and waiting for setup")
		return
	}
	if mc == nil || !mc.Authorized() {
		log.Printf("Gmail not configured for %s; background host is alive and waiting for setup", gmailMethodLabel(cfg.Gmail.Method))
		return
	}
	if cfg.Security.CodeHash == "" {
		log.Printf("SMS security code not configured; waiting for setup")
		return
	}
	if _, err := normalizeAllowedPhoneList(cfg.GoogleVoice.AllowedFrom); err != nil {
		log.Printf("SMS phone allowlist invalid: %v; waiting for setup", err)
		return
	}
	tctx, cancelMail := context.WithTimeout(ctx, 35*time.Second)
	if err := mc.Test(tctx); err != nil {
		cancelMail()
		log.Printf("Gmail connection test failed: %v", err)
		return
	}
	cancelMail()
	var codex *CodexClient
	c := NewCodexClient(cfg.CodexPath, cfg.Cwd)
	if err := c.Start(ctx); err != nil {
		log.Printf("Codex unavailable: %v", err)
	} else {
		tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		raw, e := c.Account(tctx)
		cancel()
		var v struct{ Account *struct{ Type string `json:"type"` } `json:"account"` }
		if e != nil || json.Unmarshal(raw, &v) != nil || v.Account == nil || strings.ToLower(v.Account.Type) != "chatgpt" {
			log.Printf("Codex disabled: requires ChatGPT-managed login")
			c.Close()
		} else {
			codex = c
		}
	}
	claude := NewClaudeClient(cfg.ClaudePath, cfg.Cwd, cfg.Claude)
	b := NewBridge(cfg, a.statePath, loadState(a.statePath), mc, codex, claude)
	if codex != nil {
		tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := b.initCodexThread(tctx); err != nil {
			log.Printf("Codex thread init: %v", err)
		}
		cancel()
	}
	a.mu.Lock()
	a.codex, a.claude, a.bridge = codex, claude, b
	a.mu.Unlock()
	go b.Run(ctx)
	if cfg.Gmail.Method == GmailMethodAppPassword {
		log.Printf("Gmail monitoring active via App Password with IMAP IDLE")
	} else {
		log.Printf("Gmail monitoring active via Google API/OAuth at %ds interval", cfg.Gmail.PollSeconds)
	}
}
