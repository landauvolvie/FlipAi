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

const setupHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>AI SMS Bridge</title><style>
body{font:15px system-ui;max-width:860px;margin:32px auto;padding:0 18px;color:#171717}h1{font-size:28px;margin-bottom:5px}.sub{color:#666}.section{border:1px solid #ddd;border-radius:10px;padding:16px;margin-top:18px}label{display:block;font-weight:650;margin-top:14px}input,select{width:100%;box-sizing:border-box;padding:10px;margin-top:5px}input[type=radio]{width:auto;margin:0 8px 0 0}.choice{display:block;border:1px solid #d8d8d8;border-radius:9px;padding:12px;margin-top:10px;font-weight:600;cursor:pointer}.choice input{vertical-align:middle}.methodbox{margin:10px 0 0 27px;padding:0 0 4px 0}button,a.btn{display:inline-block;margin:14px 7px 0 0;padding:10px 14px;background:#111;color:white;border:0;border-radius:7px;text-decoration:none;cursor:pointer}.danger{background:#9b1c1c}.ok{background:#eef8ee;padding:11px;border-radius:8px}.warn{background:#fff4df;padding:11px;border-radius:8px}.grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}@media(max-width:650px){.grid{grid-template-columns:1fr}}pre{white-space:pre-wrap;background:#f5f5f5;padding:12px;border-radius:8px}code{background:#f1f1f1;padding:2px 5px}.help{color:#666;font-weight:400;font-size:13px}
</style></head><body><h1>AI SMS Bridge</h1><div class="sub">Google Voice → Gmail → C: Codex / A: Claude</div>
{{if .GmailReady}}<p class="ok">✓ Gmail method configured: <b>{{.GmailMethodLabel}}</b></p>{{else if .GmailMethod}}<p class="warn">Gmail method selected but not fully connected: <b>{{.GmailMethodLabel}}</b></p>{{else}}<p class="warn">Choose a Gmail connection method below. There is no default.</p>{{end}}
<form action="/setup/save" method="post" enctype="multipart/form-data">
<div class="section"><h3>1. Gmail connection</h3><p>Choose exactly one. Nothing is routed through the project author.</p>
<label class="choice"><input type="radio" name="gmailMethod" value="app_password" {{if eq .GmailMethod "app_password"}}checked{{end}} onchange="toggleGmail()">Option 1 — Gmail App Password (easiest, no Google Cloud project)</label>
<div id="appPasswordBox" class="methodbox"><label>Gmail address</label><input name="gmailEmail" value="{{.GmailEmail}}" placeholder="you@gmail.com"><label>Google App Password {{if .HasAppPassword}}(leave blank to keep current){{end}}</label><input type="password" name="appPassword" placeholder="16-character App Password"><p class="help">Turn on Google 2-Step Verification, create an App Password, then paste it here. The bridge uses Gmail IMAP to read and Gmail SMTP to reply. The password is protected locally with Windows DPAPI.</p></div>
<label class="choice"><input type="radio" name="gmailMethod" value="oauth" {{if eq .GmailMethod "oauth"}}checked{{end}} onchange="toggleGmail()">Option 2 — Your own Google API project / Gmail OAuth</label>
<div id="oauthBox" class="methodbox"><label>Google OAuth Desktop credentials JSON</label><input type="file" name="credentials" accept="application/json"><p class="help">Create your own Google Cloud project, enable Gmail API, create an OAuth <b>Desktop app</b>, and upload its JSON here. The bridge asks for Gmail read + send access.</p></div></div>
<div class="section"><h3>2. SMS and agents</h3><div class="grid"><div><label>Authorized phone number</label><input name="allowedFrom" value="{{.AllowedFrom}}" placeholder="8455551234" required></div><div><label>Reply phone number</label><input name="replyTo" value="{{.ReplyTo}}" placeholder="Usually the same"></div></div><div class="grid"><div><label>Default agent</label><select name="defaultAgent"><option value="C" {{if eq .DefaultAgent "C"}}selected{{end}}>Codex (C:)</option><option value="A" {{if eq .DefaultAgent "A"}}selected{{end}}>Claude (A:)</option></select></div><div><label>SMS security code {{if .HasSecurity}}(leave blank to keep current){{end}}</label><input type="password" name="securityCode" placeholder="6+ characters, no spaces" {{if not .HasSecurity}}required{{end}}></div></div><div class="grid"><div><label>Codex executable</label><input name="codexPath" value="{{.CodexPath}}" placeholder="codex"></div><div><label>Claude executable</label><input name="claudePath" value="{{.ClaudePath}}" placeholder="claude"></div></div><label>Working folder</label><input name="cwd" value="{{.Cwd}}"></div>
<button type="submit">Save setup</button></form>
{{if eq .GmailMethod "oauth"}}{{if .HasCredentials}}<a class="btn" href="/oauth/google/start">Connect / Reconnect Gmail</a>{{end}}{{end}}{{if .GmailMethod}}<a class="btn" href="/gmail/test">Test Gmail connection</a>{{end}}<a class="btn" href="/codex/test">Test Codex</a><a class="btn" href="/claude/test">Test Claude</a><form action="/install" method="post" style="display:inline"><button>Start with Windows</button></form><form action="/quit" method="post" style="display:inline"><button class="danger">Quit Bridge</button></form>
<h3>How to text</h3><p><code>YOURCODE C: check GitHub...</code> → Codex<br><code>YOURCODE A: check Gmail...</code> → Claude</p><h3>Status</h3><pre>{{.Status}}</pre><p class="sub">Closing this browser tab does not stop the bridge.</p>
<script>function toggleGmail(){const v=document.querySelector('input[name="gmailMethod"]:checked')?.value||'';document.getElementById('appPasswordBox').style.display=v==='app_password'?'block':'none';document.getElementById('oauthBox').style.display=v==='oauth'?'block':'none'}toggleGmail();</script></body></html>`

type pageData struct {
	GmailReady, HasCredentials, HasAppPassword, HasSecurity bool
	GmailMethod, GmailMethodLabel, GmailEmail               string
	AllowedFrom, ReplyTo, CodexPath, ClaudePath, Cwd        string
	DefaultAgent, Status                                    string
}

func gmailMethodLabel(v string) string {
	switch v {
	case GmailMethodAppPassword:
		return "App Password (IMAP/SMTP)"
	case GmailMethodOAuth:
		return "Google API / OAuth"
	default:
		return "Not selected"
	}
}

func (a *App) authorized(r *http.Request) bool {
	c, e := r.Cookie("aisms_session")
	return e == nil && c.Value == a.cfg.LocalToken
}
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(r) {
			http.Error(w, "Open the bridge from AISMSBridge.exe", http.StatusForbidden)
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
		http.Error(w, "Open the bridge from AISMSBridge.exe", http.StatusForbidden)
		return
	}
	a.mu.Lock()
	cfg := a.cfg
	mc := a.mail
	b := a.bridge
	a.mu.Unlock()
	gmailReady := mc != nil && mc.Authorized()
	st := map[string]any{"version": version, "backgroundHost": true, "gmailMethod": cfg.Gmail.Method, "gmailConfigured": gmailReady}
	if b != nil {
		b.mu.Lock()
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
	d := pageData{
		GmailReady: gmailReady, HasCredentials: fileExists(cfg.Gmail.CredentialsFile), HasAppPassword: hasAppPasswordSecret(appPasswordPath(a.dataDir)), HasSecurity: cfg.Security.CodeHash != "",
		GmailMethod: cfg.Gmail.Method, GmailMethodLabel: gmailMethodLabel(cfg.Gmail.Method), GmailEmail: cfg.Gmail.Email,
		AllowedFrom: cfg.GoogleVoice.AllowedFrom, ReplyTo: cfg.GoogleVoice.ReplyTo, CodexPath: cfg.CodexPath, ClaudePath: cfg.ClaudePath, Cwd: cfg.Cwd, DefaultAgent: cfg.DefaultAgent, Status: string(js),
	}
	_ = template.Must(template.New("x").Parse(setupHTML)).Execute(w, d)
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
		http.Error(w, err.Error(), 400)
		return
	}
	a.mu.Lock()
	cfg := a.cfg
	oldMethod, oldEmail := cfg.Gmail.Method, cfg.Gmail.Email
	a.mu.Unlock()

	method := strings.TrimSpace(r.FormValue("gmailMethod"))
	oauthCredentialsChanged := false
	if method != "" && method != GmailMethodAppPassword && method != GmailMethodOAuth {
		http.Error(w, "Choose App Password or Google API/OAuth", 400)
		return
	}
	cfg.Gmail.Method = method
	cfg.GoogleVoice.AllowedFrom = strings.TrimSpace(r.FormValue("allowedFrom"))
	cfg.GoogleVoice.ReplyTo = strings.TrimSpace(r.FormValue("replyTo"))
	if cfg.GoogleVoice.ReplyTo == "" {
		cfg.GoogleVoice.ReplyTo = cfg.GoogleVoice.AllowedFrom
	}
	cfg.CodexPath = strings.TrimSpace(r.FormValue("codexPath"))
	cfg.ClaudePath = strings.TrimSpace(r.FormValue("claudePath"))
	cfg.Cwd = strings.TrimSpace(r.FormValue("cwd"))
	cfg.DefaultAgent = strings.ToUpper(strings.TrimSpace(r.FormValue("defaultAgent")))
	if code := strings.TrimSpace(r.FormValue("securityCode")); code != "" {
		if err := setSecurityCode(&cfg, code); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	if cfg.Security.CodeHash == "" {
		http.Error(w, "Set an SMS security code", 400)
		return
	}

	if method == GmailMethodAppPassword {
		email, err := normalizeGmailAddress(r.FormValue("gmailEmail"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		cfg.Gmail.Email = email
		pass := strings.TrimSpace(r.FormValue("appPassword"))
		secretFile := appPasswordPath(a.dataDir)
		if pass != "" {
			if err := saveAppPasswordSecret(secretFile, email, pass); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
		} else {
			existing, err := loadAppPasswordSecret(secretFile)
			if err != nil {
				http.Error(w, "Enter the Google App Password the first time you choose App Password", 400)
				return
			}
			if !strings.EqualFold(existing.Email, email) {
				http.Error(w, "Gmail address changed; enter a new App Password for that account", 400)
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
			uploaded = true
			oauthCredentialsChanged = true
			if !strings.HasSuffix(strings.ToLower(h.Filename), ".json") {
				http.Error(w, "credentials must be JSON", 400)
				return
			}
			bb, err := io.ReadAll(io.LimitReader(f, 2<<20))
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			var cr googleCredentials
			if json.Unmarshal(bb, &cr) != nil || cr.Installed.ClientID == "" {
				http.Error(w, "Use Google OAuth credentials for a Desktop app", 400)
				return
			}
			if err := os.WriteFile(cfg.Gmail.CredentialsFile, bb, 0600); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			// Never reuse a token that may have been issued for a different
			// Desktop OAuth client or Google account.
			_ = os.Remove(a.tokenPath)
		}
		if !uploaded && !fileExists(cfg.Gmail.CredentialsFile) {
			http.Error(w, "Upload your Google OAuth Desktop credentials JSON", 400)
			return
		}
	}

	if err := saveConfig(a.configPath, cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if oldMethod != cfg.Gmail.Method || oauthCredentialsChanged || (cfg.Gmail.Method == GmailMethodAppPassword && !strings.EqualFold(oldEmail, cfg.Gmail.Email)) {
		a.resetMailCheckpoint()
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	http.Redirect(w, r, "/", 303)
	go a.restartSoon()
}
func (a *App) restartSoon() {
	time.Sleep(500 * time.Millisecond)
	if a.stop != nil {
		a.stop()
	}
}
func (a *App) oauthStart(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	g := a.gmail
	cfg := a.cfg
	a.mu.Unlock()
	if cfg.Gmail.Method != GmailMethodOAuth {
		http.Error(w, "Google API/OAuth is not the selected Gmail method", 400)
		return
	}
	if g == nil {
		http.Error(w, "Upload Google Desktop OAuth credentials first", 400)
		return
	}
	redirect := "http://" + cfg.Listen + "/oauth/google/callback"
	u, attempt, err := g.AuthURL(redirect)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.mu.Lock()
	a.oauth = &attempt
	a.mu.Unlock()
	http.Redirect(w, r, u, 302)
}
func (a *App) oauthCallback(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	g := a.gmail
	att := a.oauth
	a.mu.Unlock()
	if g == nil || att == nil || r.URL.Query().Get("state") != att.State {
		http.Error(w, "Invalid OAuth state", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := g.ExchangeCode(ctx, r.URL.Query().Get("code"), *att); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprint(w, "<h2>Gmail connected.</h2><p>The background bridge will restart automatically. You may close this tab.</p>")
	go a.restartSoon()
}
func (a *App) gmailTest(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg.Gmail.Method == "" {
		http.Error(w, "Choose a Gmail connection method first", 400)
		return
	}
	// Build from the saved configuration every time. This avoids testing a
	// stale backend during the brief restart window after switching methods.
	mc, _, err := buildConfiguredMailClient(cfg.Gmail, a.dataDir, a.tokenPath)
	if err != nil {
		http.Error(w, "Gmail setup incomplete: "+err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := mc.Test(ctx); err != nil {
		http.Error(w, "Gmail test failed: "+err.Error(), 500)
		return
	}
	fmt.Fprintf(w, "<h2>Gmail OK</h2><p>%s connection succeeded. You may close this tab.</p>", template.HTMLEscapeString(gmailMethodLabel(cfg.Gmail.Method)))
}
func (a *App) codexTest(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c := NewCodexClient(cfg.CodexPath, cfg.Cwd)
	if err := c.Start(ctx); err != nil {
		http.Error(w, "Codex failed: "+err.Error(), 500)
		return
	}
	defer c.Close()
	raw, err := c.Account(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var v struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
		Requires bool `json:"requiresOpenaiAuth"`
	}
	_ = json.Unmarshal(raw, &v)
	if v.Requires || v.Account == nil {
		http.Error(w, "Codex is not signed in. Open Codex on this Windows account, choose Sign in with ChatGPT, then run Test Codex again.", 400)
		return
	}
	if strings.ToLower(v.Account.Type) != "chatgpt" {
		http.Error(w, "Codex is not using Sign in with ChatGPT; API-key/provider auth is refused", 400)
		return
	}
	fmt.Fprint(w, "<h2>Codex OK</h2><p>ChatGPT-managed Codex login detected. You can close this tab.</p>")
}
func (a *App) claudeTest(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c := NewClaudeClient(cfg.ClaudePath, cfg.Cwd, cfg.Claude)
	if err := c.Test(ctx); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprint(w, "<h2>Claude Code found</h2><p>The bridge strips Anthropic API-key environment variables. Claude must already be signed into your Claude subscription.</p>")
}
func (a *App) install(w http.ResponseWriter, r *http.Request) {
	dst, err := copySelfInstall()
	if err == nil {
		err = installAutostart(dst)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintf(w, "<h2>Startup enabled.</h2><p>No administrator rights were requested. The watchdog starts when this Windows user signs in.</p><p>Installed at <code>%s</code>.</p>", template.HTMLEscapeString(dst))
}
func (a *App) quit(w http.ResponseWriter, r *http.Request) {
	_ = os.WriteFile(a.dataDir+string(os.PathSeparator)+"quit.flag", []byte("quit"), 0600)
	fmt.Fprint(w, "<h2>Bridge stopped.</h2><p>Closing the GUI normally does not stop it; this Quit button does.</p>")
	if a.stop != nil {
		go func() { time.Sleep(300 * time.Millisecond); a.stop() }()
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
	m.HandleFunc("/quit", a.requireAuth(a.quit))
	return m
}

func (a *App) startBridge(ctx context.Context) {
	a.mu.Lock()
	cfg := a.cfg
	mc := a.mail
	a.mu.Unlock()
	if cfg.Gmail.Method == "" {
		log.Printf("Gmail connection method not selected; background host is alive and waiting for setup")
		return
	}
	if mc == nil || !mc.Authorized() {
		log.Printf("Gmail not configured for %s; background host is alive and waiting for setup", gmailMethodLabel(cfg.Gmail.Method))
		return
	}
	if cfg.GoogleVoice.AllowedFrom == "" || cfg.Security.CodeHash == "" {
		log.Printf("phone/security code not configured; waiting for setup")
		return
	}
	// Verify Gmail authentication before an always-on poll loop is started.
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
		var v struct {
			Account *struct {
				Type string `json:"type"`
			} `json:"account"`
		}
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
	a.codex = codex
	a.claude = claude
	a.bridge = b
	a.mu.Unlock()
	go b.Run(ctx)
	log.Printf("Gmail monitoring active via %s every %ds", gmailMethodLabel(cfg.Gmail.Method), cfg.Gmail.PollSeconds)
}
