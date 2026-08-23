from pathlib import Path
import re

p = Path("activity_web.go")
s = p.read_text(encoding="utf-8")
pattern = r'func \(a \*App\) saveSetupEnhanced\(w http\.ResponseWriter, r \*http\.Request\) \{.*?\n\}\n\n(?=func copyRecordedResponse)'
replacement = '''func (a *App) saveSetupEnhanced(w http.ResponseWriter, r *http.Request) {
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

'''
out, n = re.subn(pattern, replacement, s, count=1, flags=re.S)
if n != 1:
    raise SystemExit(f"expected one saveSetupEnhanced repair, got {n}")
p.write_text(out, encoding="utf-8")
print("repaired v0.6.4 settings wrapper")
