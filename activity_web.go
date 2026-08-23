package main

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"
)

const activityHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Activity — FlipAi</title><style>
:root{--bg:#f5f6fa;--surface:#fff;--ink:#17151f;--muted:#6f6b7a;--line:#e7e4ee;--violet:#6c47ff;--violetSoft:#f0ecff;--green:#18794e;--greenSoft:#eaf8f1;--amber:#9a6700;--amberSoft:#fff6df;--red:#b42318;--redSoft:#fff0ee}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.45 Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif}.top{height:70px;border-bottom:1px solid var(--line);background:rgba(245,246,250,.94);display:flex;align-items:center}.topin{width:min(1180px,calc(100% - 32px));margin:auto;display:flex;justify-content:space-between;align-items:center}.brand{display:flex;align-items:center;gap:10px;text-decoration:none;color:var(--ink)}.mark{width:38px;height:38px;border-radius:12px;background:var(--violet);color:white;display:grid;place-items:center;font-weight:900}.brand b{display:block}.brand small{display:block;color:var(--muted)}.wrap{width:min(1180px,calc(100% - 32px));margin:26px auto 60px}.head{display:flex;justify-content:space-between;gap:18px;align-items:flex-start;margin-bottom:18px}.head h1{margin:0;font-size:30px}.head p{margin:6px 0 0;color:var(--muted);max-width:760px}.actions{display:flex;gap:9px;flex-wrap:wrap}.btn,button{border:0;border-radius:11px;padding:10px 14px;font-weight:800;text-decoration:none;cursor:pointer;font:inherit}.primary{background:var(--violet);color:white}.outline{background:white;color:#3e3946;border:1px solid var(--line)}.danger{background:var(--redSoft);color:var(--red)}.summary{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin-bottom:16px}.stat{background:white;border:1px solid var(--line);border-radius:15px;padding:15px}.stat span{display:block;color:var(--muted);font-size:11px;text-transform:uppercase;letter-spacing:.07em}.stat b{display:block;margin-top:5px;font-size:16px}.card{background:white;border:1px solid var(--line);border-radius:18px;overflow:hidden}.privacy{padding:13px 16px;background:#fafaff;border-bottom:1px solid var(--line);color:var(--muted);font-size:12px}.event{display:grid;grid-template-columns:150px 105px 90px 1fr 120px 65px;gap:12px;padding:13px 16px;border-bottom:1px solid #efedf3;align-items:start}.event:last-child{border-bottom:0}.time{color:var(--muted);font-variant-numeric:tabular-nums}.stage{font-weight:800;text-transform:capitalize}.pill{display:inline-flex;padding:4px 8px;border-radius:999px;font-size:11px;font-weight:900;width:max-content}.info{background:var(--violetSoft);color:#5435d8}.success{background:var(--greenSoft);color:var(--green)}.warn{background:var(--amberSoft);color:var(--amber)}.error{background:var(--redSoft);color:var(--red)}.message{font-weight:600}.meta{color:var(--muted);font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:12px}.empty{padding:55px 20px;text-align:center;color:var(--muted)}.empty b{display:block;color:var(--ink);font-size:18px;margin-bottom:6px}@media(max-width:900px){.summary{grid-template-columns:1fr 1fr}.event{grid-template-columns:110px 90px 80px 1fr}.sender,.agent{display:none}}@media(max-width:600px){.head{display:block}.actions{margin-top:14px}.summary{grid-template-columns:1fr 1fr}.event{grid-template-columns:1fr;gap:5px}.time,.stage{font-size:12px}.event .pill{margin-top:2px}}
</style></head><body><header class="top"><div class="topin"><a class="brand" href="/"><div class="mark">F</div><div><b>FlipAi</b><small>Activity & Logs</small></div></a><div style="color:var(--muted);font-size:12px">v{{.Version}}</div></div></header><main class="wrap"><div class="head"><div><h1>Activity & Logs</h1><p>See exactly how each SMS moves through Gmail, sender verification, optional security-code validation, Codex/Claude, and the reply channel. This page refreshes automatically every 2 seconds.</p></div><div class="actions"><a class="btn outline" href="/">Back to Settings</a><button class="btn outline" onclick="loadEvents()">Refresh</button><form method="post" action="/activity/clear" onsubmit="return confirm('Clear FlipAi activity history?')"><button class="btn danger" type="submit">Clear logs</button></form></div></div><div class="summary"><div class="stat"><span>Latest event</span><b id="latest">—</b></div><div class="stat"><span>Gmail</span><b id="gmail">Waiting</b></div><div class="stat"><span>Agent</span><b id="agent">Waiting</b></div><div class="stat"><span>Reply</span><b id="reply">Waiting</b></div></div><section class="card"><div class="privacy">Privacy: FlipAi logs statuses and errors only. SMS contents, agent prompts/results, security codes, Gmail App Passwords, OAuth tokens, and credentials are never written to this activity log.</div><div id="events"><div class="empty"><b>Loading activity…</b></div></div></section></main><script>
function esc(v){return String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function stageStatus(events,stage){const e=events.find(x=>x.stage===stage);if(!e)return 'Waiting';if(e.level==='error')return 'Error';if(e.level==='warn')return 'Attention';if(e.level==='success')return 'OK';return 'Active'}
async function loadEvents(){try{const r=await fetch('/activity.json',{cache:'no-store'});if(!r.ok)throw new Error('HTTP '+r.status);const events=await r.json();document.getElementById('latest').textContent=events.length?new Date(events[0].time).toLocaleTimeString():'—';document.getElementById('gmail').textContent=stageStatus(events,'gmail');document.getElementById('agent').textContent=stageStatus(events,'agent');document.getElementById('reply').textContent=stageStatus(events,'reply');const root=document.getElementById('events');if(!events.length){root.innerHTML='<div class="empty"><b>No activity yet</b>Send a test SMS to your Google Voice number. The first Gmail detection should appear here within seconds.</div>';return}root.innerHTML=events.map(e=>'<div class="event"><div class="time">'+esc(new Date(e.time).toLocaleString())+'</div><div class="stage">'+esc(e.stage)+'</div><div><span class="pill '+esc(e.level)+'">'+esc(e.level)+'</span></div><div class="message">'+esc(e.message)+'</div><div class="meta sender">'+(e.sender?esc(e.sender):'—')+'</div><div class="meta agent">'+(e.agent?esc(e.agent):'—')+'</div></div>').join('')}catch(e){document.getElementById('events').innerHTML='<div class="empty"><b>Could not load activity</b>'+esc(e.message)+'</div>'}}
loadEvents();setInterval(loadEvents,2000);
</script></body></html>`

type activityPageData struct{ Version string }

func (a *App) activityPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = template.Must(template.New("activity").Parse(activityHTML)).Execute(w, activityPageData{Version: version})
}

func (a *App) activityJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	log := activityLogForStatePath(a.statePath)
	_ = json.NewEncoder(w).Encode(log.Recent(200))
}

func (a *App) activityClear(w http.ResponseWriter, r *http.Request) {
	log := activityLogForStatePath(a.statePath)
	_ = log.Clear()
	http.Redirect(w, r, "/activity", http.StatusSeeOther)
}

func (a *App) codexTestCorrected(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	c := NewCodexClient(cfg.CodexPath, cfg.Cwd)
	if err := c.Start(ctx); err != nil {
		activityLogForStatePath(a.statePath).Add("error", "agent", "Codex test could not start: "+truncate(err.Error(), 220), "", "C", "")
		renderResult(w, 500, false, "Codex could not start", err.Error()+"\n\nOpen Codex on this Windows account and verify the executable path in Advanced agent paths.")
		return
	}
	defer c.Close()
	raw, err := c.Account(ctx)
	if err != nil || !codexAccountIsChatGPT(raw) {
		activityLogForStatePath(a.statePath).Add("error", "agent", "Codex test did not detect a ChatGPT-managed account", "", "C", "")
		renderResult(w, 400, false, "Codex is not ready", "FlipAi could start Codex but did not detect a ChatGPT-managed account.")
		return
	}
	if err := c.SmokeTest(ctx); err != nil {
		activityLogForStatePath(a.statePath).Add("error", "agent", "Codex real background test failed: "+truncate(err.Error(), 220), "", "C", "")
		renderResult(w, 500, false, "Codex background test failed", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("success", "agent", "Codex real background test passed", "", "C", "")
	renderResult(w, 200, true, "Codex is ready", "A real ephemeral Codex request completed successfully. C: SMS commands can be routed to Codex.")
}

func (a *App) claudeTestCorrected(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	c := a.newClaudeClient(cfg)
	if err := c.Test(ctx); err != nil {
		activityLogForStatePath(a.statePath).Add("error", "agent", "Claude real background test failed: "+truncate(err.Error(), 220), "", "A", "")
		renderResult(w, 500, false, "Claude is not ready", friendlyAgentError(err))
		return
	}
	activityLogForStatePath(a.statePath).Add("success", "agent", "Claude real background test passed", "", "A", "")
	renderResult(w, 200, true, "Claude is ready", "A real Claude Code background request completed successfully. A: SMS commands can be routed to Claude.")
}

func (a *App) enableStartupCurrent(w http.ResponseWriter, r *http.Request) {
	exe, err := os.Executable()
	if err == nil {
		err = installAutostart(exe)
	}
	if err != nil {
		renderResult(w, 500, false, "Could not enable startup", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("success", "startup", "Start with Windows enabled for the installed FlipAi executable", "", "", "")
	renderResult(w, 200, true, "Start with Windows is enabled", "FlipAi will start for this Windows user at sign-in. No second copy of the application was created.")
}

func (a *App) saveSetupEnhanced(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		renderResult(w, 400, false, "Could not read settings", err.Error())
		return
	}
	requireCode := r.FormValue("requireSecurityCode") == "1"
	a.mu.Lock()
	oldCfg := a.cfg
	cfg := a.cfg
	a.mu.Unlock()
	providedCode := strings.TrimSpace(r.FormValue("securityCode"))
	if requireCode && (!cfg.Security.RequireCode || cfg.Security.CodeHash == "") && providedCode == "" {
		renderResult(w, 400, false, "Set an SMS security code", "Enter a new security code when turning code protection on.")
		return
	}
	if !requireCode && cfg.Security.CodeHash == "" {
		placeholder, err := secureRandomToken(24)
		if err != nil || setSecurityCode(&cfg, placeholder) != nil {
			renderResult(w, 500, false, "Could not disable the SMS code", "FlipAi could not create its internal disabled-code placeholder.")
			return
		}
	}
	cfg.Security.RequireCode = requireCode
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()

	rec := httptest.NewRecorder()
	a.saveSetup(rec, r)
	if rec.Code >= 400 {
		a.mu.Lock()
		a.cfg = oldCfg
		a.mu.Unlock()
	}
	copyRecordedResponse(w, rec, rec.Body.Bytes())
}

func copyRecordedResponse(w http.ResponseWriter, rec *httptest.ResponseRecorder, body []byte) {
	for k, vv := range rec.Header() {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(body)
}

// withActivityRoutes adds the diagnostics surface without weakening the
// loopback token/cookie protection already used by the settings UI. It also
// corrects two stale UI strings from the portable-build era.
func withActivityRoutes(a *App, base http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/activity":
			a.requireAuth(a.activityPage)(w, r)
			return
		case "/activity.json":
			a.requireAuth(a.activityJSON)(w, r)
			return
		case "/activity/clear":
			a.requireAuth(a.activityClear)(w, r)
			return
		case "/setup/save":
			a.requireAuth(a.saveSetupEnhanced)(w, r)
			return
		case "/codex/test":
			a.requireAuth(a.codexTestCorrected)(w, r)
			return
		case "/claude/test":
			a.requireAuth(a.claudeTestCorrected)(w, r)
			return
		case "/install":
			a.requireAuth(a.enableStartupCurrent)(w, r)
			return
	}
		if r.URL.Path != "/" {
			base.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		base.ServeHTTP(rec, r)
		body := rec.Body.Bytes()
		if rec.Code == http.StatusOK && strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			s := string(body)
			a.mu.Lock()
			requireCode := a.cfg.Security.RequireCode
			a.mu.Unlock()
			s = strings.Replace(s, `<a href="#diagnostics">Diagnostics</a>`, `<a href="/activity">Activity &amp; Logs</a><a href="#diagnostics">Diagnostics</a>`, 1)
			s = strings.ReplaceAll(s, `%LOCALAPPDATA%\Programs\AISMSBridge\AISMSBridge.exe`, `%LOCALAPPDATA%\Programs\FlipAi\FlipAi.exe`)
			s = strings.Replace(s, `Install &amp; start with Windows`, `Enable Start with Windows`, 1)
			s = strings.Replace(s, `Install & start with Windows`, `Enable Start with Windows`, 1)
			s = strings.Replace(s, `Tray → Open Settings reopens this page.`, `Tray → Open Settings reopens this page. Use Activity & Logs to trace each SMS end-to-end.`, 1)
			s = strings.Replace(s, `FlipAi verifies the sender and security code, then routes`, `FlipAi verifies the sender and, when enabled, the security code, then routes`, 1)
			toggle := `<label class="checkrow"><input type="checkbox" name="requireSecurityCode" value="1"`
			if requireCode { toggle += ` checked` }
			toggle += `><span><b>Require SMS security code</b><span>Optional extra protection. The allowed phone-number list is always enforced even when this is off.</span></span></label>`
			s = strings.Replace(s, `<div class="field"><label>SMS security code`, toggle+`<div class="field"><label>SMS security code`, 1)
			s = strings.Replace(s, `Uses the local Codex App Server. FlipAi requires <b>Sign in with ChatGPT</b> and rejects API/provider auth.`, `Uses the local Codex App Server with <b>Sign in with ChatGPT</b>. SMS turns get full permissions of this Windows user (no Codex sandbox and no UAC/admin elevation), then the thread is released so Codex Desktop can open the same history.`, 1)
			if !requireCode {
				s = strings.Replace(s, `name="securityCode" autocomplete="new-password" placeholder="Private code required at the start of every text" required>`, `name="securityCode" autocomplete="new-password" placeholder="Private code required at the start of every text">`, 1)
				s = strings.Replace(s, `Private code required at the start of every text`, `Optional code — turn on “Require SMS security code” to enforce it`, 1)
				s = strings.ReplaceAll(s, `YOURCODE C:`, `C:`)
				s = strings.ReplaceAll(s, `YOURCODE A:`, `A:`)
				s = strings.ReplaceAll(s, `YOURCODE STATUS`, `STATUS`)
			}
			body = []byte(s)
			rec.Header().Del("Content-Length")
		}
		copyRecordedResponse(w, rec, body)
	})
}
