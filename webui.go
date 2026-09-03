package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
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

	// liveClaude is the supervised Claude Code session when live mode is on,
	// nil otherwise. liveSupport records why it is nil so the Agents page can
	// explain the fallback instead of just showing the wrong mode.
	liveClaude  *ClaudeLiveClient
	liveSupport claudeLiveSupport

	// hookToken authenticates the live session's hook helper to the loopback
	// endpoint. It is minted per host run and never written to disk.
	hookToken string

	oauth *GoogleOAuthAttempt
	stop  func()
}

// resultHTML is the confirmation/failure page shown after an action. It uses
// the same stylesheet as the app so a test result or a validation error still
// looks like part of the desktop window.
const resultHTML = `<!doctype html><html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} — FlipAi</title>
<link rel="stylesheet" href="/assets/flipai.css?v={{.Version}}">
<style>.result{max-width:640px;margin:9vh auto;padding:24px}.result .mark{width:44px;height:44px;border-radius:12px;display:grid;place-items:center;margin-bottom:16px}.result h1{font-size:22px;margin-bottom:8px}.result p{color:var(--muted);white-space:pre-wrap;margin:0 0 20px}.result .mark svg{width:24px;height:24px}</style>
</head><body><div class="result"><div class="card"><div class="card-body">
<div class="mark {{.Class}}">{{.Icon | safeHTML}}</div>
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
<a class="btn primary" href="{{.Back}}">Back to FlipAi</a>
<p class="hint" style="margin-top:18px">Closing this window never stops the background bridge.</p>
</div></div></div></body></html>`

type resultData struct {
	Title, Message, Class, Icon, Back, Version string
}

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

var resultTemplate = template.Must(template.New("result").Funcs(uiFuncs).Parse(resultHTML))

// wantsInlineResult reports that the caller is the page itself, asking for the
// answer rather than a page to land on. Every test used to reply with a whole
// result page, which meant pressing Test navigated away and the user had to
// find their way back.
func wantsInlineResult(r *http.Request) bool {
	return r != nil && r.Header.Get("X-FlipAi-Inline") == "1"
}

func renderResult(w http.ResponseWriter, r *http.Request, status int, ok bool, title, message string) {
	if wantsInlineResult(r) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "title": title, "message": message})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	d := resultData{Title: title, Message: message, Class: "pill", Icon: string(uiIcon("bridge")), Back: backTarget(r), Version: version}
	if ok {
		d.Class, d.Icon = "pill ok", string(uiIcon("check"))
	} else if status >= 400 {
		d.Class, d.Icon = "pill bad", string(uiIcon("alert"))
	} else {
		d.Class, d.Icon = "pill warn", string(uiIcon("alert"))
	}
	_ = resultTemplate.Execute(w, d)
}

// backTarget keeps the "Back to FlipAi" button pointing at the page the action
// came from, and never anywhere outside this local UI.
func backTarget(r *http.Request) string {
	if r == nil {
		return "/"
	}
	ref := r.Referer()
	if ref == "" {
		return "/"
	}
	u, err := url.Parse(ref)
	if err != nil || u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return "/"
	}
	if u.Host != "" && r.Host != "" && u.Host != r.Host {
		return "/"
	}
	return u.Path
}

func (a *App) authorized(r *http.Request) bool {
	c, e := r.Cookie("aisms_session")
	if e != nil {
		return false
	}
	a.mu.Lock()
	token := a.cfg.LocalToken
	a.mu.Unlock()
	return c.Value == token
}
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(r) {
			http.Error(w, "Open FlipAi from FlipAi.exe or the tray icon.", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// enter accepts the one-time local token FlipAi passes when it opens its own
// window, exchanges it for a session cookie, and hands off to the Home page.
func (a *App) enter(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	localToken := a.cfg.LocalToken
	a.mu.Unlock()
	if t := r.URL.Query().Get("token"); t != "" && t == localToken {
		http.SetCookie(w, &http.Cookie{Name: "aisms_session", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !a.authorized(r) {
		http.Error(w, "Open FlipAi from FlipAi.exe or the tray icon.", http.StatusForbidden)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	a.homePage(w, r)
}

func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }

func (a *App) resetMailCheckpoint() {
	s := loadState(a.statePath)
	s.GmailBaselineUnix = 0
	s.ProcessedMessageIDs = nil
	s.LastMessageID = ""
	_ = saveState(a.statePath, s)
}

func (a *App) restartSoon() {
	time.Sleep(1400 * time.Millisecond)
	// End the supervised Claude session first. A settings change that switches
	// modes would otherwise leave the old session running against the working
	// folder with nothing left to talk to it.
	a.stopClaudeLive()
	if a.stop != nil {
		a.stop()
	}
}
func (a *App) oauthStart(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	g, cfg := a.gmail, a.cfg
	a.mu.Unlock()
	if cfg.Gmail.Method != GmailMethodOAuth {
		renderResult(w, r, 400, false, "OAuth is not selected", "Choose your own Google API project / Gmail OAuth first.")
		return
	}
	if g == nil {
		renderResult(w, r, 400, false, "OAuth credentials missing", "Upload Google Desktop OAuth credentials first.")
		return
	}
	redirect := "http://" + cfg.Listen + "/oauth/google/callback"
	u, attempt, err := g.AuthURL(redirect)
	if err != nil {
		renderResult(w, r, 500, false, "Could not start Google sign-in", err.Error())
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
		renderResult(w, r, 400, false, "Google sign-in was not completed", e)
		return
	}
	if g == nil || att == nil || r.URL.Query().Get("state") != att.State {
		renderResult(w, r, 400, false, "Google sign-in could not be verified", "The OAuth state did not match. Start Connect Google account again from FlipAi.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := g.ExchangeCode(ctx, r.URL.Query().Get("code"), *att); err != nil {
		renderResult(w, r, 500, false, "Gmail connection failed", err.Error())
		return
	}
	a.mu.Lock()
	a.oauth = nil
	a.mu.Unlock()
	renderResult(w, r, 200, true, "Gmail connected", "Google OAuth completed successfully. The background bridge will restart automatically with the new Gmail connection.")
	go a.restartSoon()
}
func (a *App) gmailTest(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg.Gmail.Method == "" {
		renderResult(w, r, 400, false, "Choose a Gmail method first", "Select App Password or your own Google API/OAuth project, then save settings.")
		return
	}
	mc, _, err := buildConfiguredMailClient(cfg.Gmail, a.dataDir, a.tokenPath)
	if err != nil {
		renderResult(w, r, 400, false, "Gmail setup is incomplete", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	if err := mc.Test(ctx); err != nil {
		renderResult(w, r, 500, false, "Gmail test failed", err.Error())
		return
	}
	renderResult(w, r, 200, true, "Gmail is working", gmailMethodLabel(cfg.Gmail.Method)+" connected successfully. FlipAi can access the mailbox with the selected method.")
}
func (a *App) quit(w http.ResponseWriter, r *http.Request) {
	requestQuit(a.dataDir, "settings quit")
	renderResult(w, r, 200, true, "FlipAi is stopping", "The tray, background host, and watchdog are being stopped completely. Launch AISMSBridge.exe again whenever you want to reconnect.")
	if a.stop != nil {
		go func() { time.Sleep(600 * time.Millisecond); a.stop() }()
	}
}

// handler wires the desktop UI. Every page and every action is a normal
// authenticated route: the window is an ordinary local web client, so there is
// no injected script rewriting the page after it loads.
func (a *App) handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"version":%q}`, version)
	})
	m.HandleFunc("/assets/", a.serveAsset)
	m.HandleFunc("/oauth/google/callback", a.oauthCallback)
	// The live-session hook helper posts here. It authenticates with its own
	// per-run secret rather than the page token, because its caller is a child
	// process rather than the desktop window.
	m.HandleFunc(claudeLiveHookPath, a.claudeHookEndpoint)

	// Pages.
	m.HandleFunc("/", a.enter)
	for path, page := range map[string]http.HandlerFunc{
		"/connections": a.connectionsPage,
		"/agents":      a.agentsPage,
		"/activity":    a.activityPage,
		"/settings":    a.settingsPage,
		// Retired pages. A bookmark, an in-page link from an older build, or the
		// installer's finish page can still point at these, so they land where
		// their contents went instead of on a 404.
		"/phone":    pageMovedTo("/agents"),
		"/advanced": pageMovedTo("/settings"),
	} {
		m.HandleFunc(path, a.requireAuth(page))
	}

	// Data.
	m.HandleFunc("/status.json", a.requireAuth(a.statusJSON))
	m.HandleFunc("/activity.json", a.requireAuth(a.activityJSON))
	m.HandleFunc("/folders.json", a.requireAuth(a.foldersJSON))
	m.HandleFunc("/chatgpt/status.json", a.requireAuth(a.chatGPTStatusJSON))
	m.HandleFunc("/claude-chat/status.json", a.requireAuth(a.claudeChatStatusJSON))
	m.HandleFunc("/gemini-chat/status.json", a.requireAuth(a.geminiChatStatusJSON))

	// Actions.
	for path, action := range map[string]http.HandlerFunc{
		"/bridge/pause":           a.pauseBridge,
		"/bridge/resume":          a.resumeBridge,
		"/bridge/restart":         a.restartBridge,
		"/connections/save":       a.saveConnections,
		"/connections/flowtest":   a.flowTest,
		"/agents/save":            a.saveAgents,
		"/agents/reset":           a.resetAgentConversation,
		"/claude/connect":         a.claudeConnect,
		"/claude/connect/verify":  a.claudeConnectVerify,
		"/claude/disconnect":      a.claudeDisconnect,
		"/chatgpt/connect":        a.chatGPTConnect,
		"/chatgpt/test":           a.chatGPTTest,
		"/chatgpt/chat":           a.chatGPTChat,
		"/chatgpt/disconnect":     a.chatGPTDisconnect,
		"/claude-chat/connect":    a.claudeChatConnect,
		"/claude-chat/test":       a.claudeChatTest,
		"/claude-chat/disconnect": a.claudeChatDisconnect,
		"/gemini-chat/connect":    a.geminiChatConnect,
		"/gemini-chat/test":       a.geminiChatTest,
		"/gemini-chat/disconnect": a.geminiChatDisconnect,
		"/agents/numbers/add":     a.addAgentNumber,
		"/agents/numbers/remove":  a.removeAgentNumber,
		"/settings/save":          a.saveSettings,
		"/settings/startup":       a.saveStartup,
		"/settings/updates":       a.saveUpdates,
		"/settings/bootstartup":   a.saveBootStartup,
		"/update/check":           a.updateCheck,
		"/update/install":         a.updateInstall,
		"/settings/reset":         a.resetSetup,
		"/activity/clear":         a.activityClear,
		"/health/check":           a.healthCheck,
		"/quit":                   a.quit,
	} {
		m.HandleFunc(path, a.requireAuth(requirePost(action)))
	}

	// Links that read state or open a local window.
	for path, action := range map[string]http.HandlerFunc{
		"/gmail/test":           a.gmailTest,
		"/chatgpt-direct/probe": a.chatGPTDirectProbe,
		"/codex/test":           a.codexTestCorrected,
		"/claude/test":          a.claudeTestCorrected,
		"/logs/export":          a.exportLogs,
		"/open/folder":          a.openLocalFolder,
		"/oauth/google/start":   a.oauthStart,
	} {
		m.HandleFunc(path, a.requireAuth(action))
	}
	return m
}

// pageMovedTo sends a retired page to the one that absorbed it.
func pageMovedTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}
}

// requirePost keeps state-changing routes off GET, so a link or a prefetch can
// never carry out an action.
func requirePost(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "This FlipAi action requires a form submission.", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
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
	// Gmail monitoring is transport-level and must start as soon as the mailbox
	// is connected. Phone allowlists and security codes belong to routing and are
	// enforced when a message is read; they must never prevent the mailbox from
	// being watched or leave "Last mailbox check" stuck at "Not checked yet".
	tctx, cancelMail := context.WithTimeout(ctx, 35*time.Second)
	if err := mc.Test(tctx); err != nil {
		cancelMail()
		log.Printf("Gmail connection test failed: %v", err)
		return
	}
	cancelMail()
	var codex *CodexClient
	c := NewCodexClient(cfg.CodexPath, cfg.codexWorkingDir())
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
	claude := a.newClaudeClient(cfg)
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
	// A finished turn is what keeps the Agents tiles honest; without this they
	// only ever showed the last time someone pressed a Test button.
	b.SetAgentResultSink(func(agent string, ok bool, detail string) {
		a.recordCheck(agent, ok, detail)
	})
	// Start the mailbox loop before any optional Claude live-session preflight.
	// Bridge.Run performs an immediate first poll, then App Password mode enters
	// IMAP IDLE so Gmail wakes FlipAi on EXISTS/RECENT instead of waiting for a
	// polling interval. The 30-second poll remains only as a dropped-IDLE backup.
	go b.Run(ctx)
	// Live mode is attached after the bridge exists so its preflight can log
	// through the same Activity log the user reads, and so a refusal leaves a
	// working per-message bridge rather than no bridge at all.
	a.startClaudeLive(ctx, cfg, b)
	go a.runAgentHealthProbe(ctx)
	// Establish which Claude credential this machine is on before the first text
	// arrives, so the Agents page describes the real connection and a
	// browser-less one is named in the Activity log rather than only in whatever
	// Claude ends up texting back.
	go a.warmClaudeConnection(ctx, cfg, b, claude)
	if cfg.Gmail.Method == GmailMethodAppPassword {
		log.Printf("Gmail monitoring active via App Password with IMAP IDLE")
	} else {
		log.Printf("Gmail monitoring active via Google API/OAuth at %ds interval", cfg.Gmail.PollSeconds)
	}
}
