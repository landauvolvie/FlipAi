package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// This file holds the activity feed the desktop UI reads and the two agent
// tests that run a real background request instead of only checking that a
// process starts.

func (a *App) activityJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	log := activityLogForStatePath(a.statePath)
	_ = json.NewEncoder(w).Encode(log.Recent(200))
}

func (a *App) activityClear(w http.ResponseWriter, r *http.Request) {
	log := activityLogForStatePath(a.statePath)
	_ = log.Clear()
	redirectTo(w, r, "/activity", "logs-cleared")
}

func (a *App) codexTestCorrected(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	c := NewCodexClient(cfg.CodexPath, cfg.codexWorkingDir())
	if err := c.Start(ctx); err != nil {
		a.recordCheck("codex", false, err.Error())
		activityLogForStatePath(a.statePath).Add("error", "agent", "Codex test could not start: "+truncate(err.Error(), 220), "", "C", "")
		renderResult(w, r, 500, false, "Codex could not start", err.Error()+"\n\nOpen Codex on this Windows account and check the executable path on the Agents page.")
		return
	}
	defer c.Close()
	raw, err := c.Account(ctx)
	if err != nil || !codexAccountIsChatGPT(raw) {
		a.recordCheck("codex", false, "no ChatGPT-managed account detected")
		activityLogForStatePath(a.statePath).Add("error", "agent", "Codex test did not detect a ChatGPT-managed account", "", "C", "")
		renderResult(w, r, 400, false, "Codex is not ready", "FlipAi could start Codex but did not detect a ChatGPT-managed account. Open Codex, choose Sign in with ChatGPT, then test again.")
		return
	}
	if err := c.SmokeTest(ctx); err != nil {
		a.recordCheck("codex", false, err.Error())
		activityLogForStatePath(a.statePath).Add("error", "agent", "Codex real background test failed: "+truncate(err.Error(), 220), "", "C", "")
		renderResult(w, r, 500, false, "Codex background test failed", err.Error())
		return
	}
	a.recordCheck("codex", true, "ChatGPT-managed login, background turn completed")
	activityLogForStatePath(a.statePath).Add("success", "agent", "Codex real background test passed", "", "C", "")
	renderResult(w, r, 200, true, "Codex is ready", "A real ephemeral Codex request completed successfully. C: messages can be routed to Codex.")
}

func (a *App) claudeTestCorrected(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	c := a.newClaudeClient(cfg)
	if err := c.Test(ctx); err != nil {
		a.recordCheck("claude", false, friendlyAgentError(err))
		activityLogForStatePath(a.statePath).Add("error", "agent", "Claude real background test failed: "+truncate(err.Error(), 220), "", "A", "")
		renderResult(w, r, 500, false, "Claude is not ready", friendlyAgentError(err))
		return
	}
	a.recordCheck("claude", true, "Claude Code subscription login verified")
	activityLogForStatePath(a.statePath).Add("success", "agent", "Claude real background test passed", "", "A", "")
	renderResult(w, r, 200, true, "Claude is ready", "A real Claude Code background request completed successfully. A: messages can be routed to Claude.")
}
