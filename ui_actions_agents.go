package main

import (
	"net/http"
	"strconv"
	"strings"
)

// Actions for the things an agent owns: who may reach it, its security code,
// and how it answers. These replaced the Phone page, whose whole subject was a
// single shared allowlist.

// agentFromForm reads the agent letter a form is acting on.
func agentFromForm(r *http.Request, field string) string {
	switch strings.ToUpper(strings.TrimSpace(r.FormValue(field))) {
	case "A":
		return "A"
	case "G":
		return "G"
	default:
		return "C"
	}
}

func putAgentSettings(cfg *Config, agent string, s AgentSettings) {
	switch agent {
	case "A":
		cfg.Claude.AgentSettings = s
	case "G":
		cfg.ChatGPT.AgentSettings = s
	default:
		cfg.Codex.AgentSettings = s
	}
}

func (a *App) addAgentNumber(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the number", err.Error())
		return
	}
	agent := agentFromForm(r, "agent")
	number := normalizeUSPhone(r.FormValue("newNumber"))
	if number == "" {
		renderResult(w, r, 400, false, "Number was not added",
			"Enter a 10-digit US or Canada phone number. A leading +1 is fine.")
		return
	}
	err := a.updateConfig(func(cfg *Config) error {
		s := agentSettings(*cfg, agent)
		access := normalizeAccess(r.FormValue("newAccess"))
		if agent == "G" {
			access = AccessSMS
		}
		s.Phones = append(s.Phones, AgentPhone{
			Number: number,
			Label:  r.FormValue("newLabel"),
			Access: access,
		})
		putAgentSettings(cfg, agent, s)
		return normalizeAgents(cfg)
	})
	if err != nil {
		renderResult(w, r, 400, false, "Number was not added", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "security",
		"Allowed phone number added to "+agentDisplayName(agent), number, agent, "")
	redirectTo(w, r, "/agents", "number-added")
	go a.restartSoon()
}

func (a *App) removeAgentNumber(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderResult(w, r, 400, false, "Could not read the number", err.Error())
		return
	}
	// The button carries both halves, because one button can send one value.
	agent, number, _ := strings.Cut(r.FormValue("number"), ":")
	agent = strings.ToUpper(strings.TrimSpace(agent))
	if agent != "A" && agent != "G" {
		agent = "C"
	}
	number = normalizeUSPhone(number)
	err := a.updateConfig(func(cfg *Config) error {
		s := agentSettings(*cfg, agent)
		kept := s.Phones[:0]
		for _, p := range s.Phones {
			if p.Number != number {
				kept = append(kept, p)
			}
		}
		s.Phones = kept
		putAgentSettings(cfg, agent, s)
		return normalizeAgents(cfg)
	})
	if err != nil {
		renderResult(w, r, 400, false, "Number was not removed", err.Error())
		return
	}
	activityLogForStatePath(a.statePath).Add("info", "security",
		"Allowed phone number removed from "+agentDisplayName(agent), number, agent, "")
	redirectTo(w, r, "/agents", "number-removed")
	go a.restartSoon()
}

// applyAgentAccessForm folds the per-agent access, code and reply fields of the
// Agents form into the configuration. It is called from saveAgents so one Save
// covers everything on the pane.
func applyAgentAccessForm(cfg *Config, r *http.Request, agent string) error {
	// Keep accepting the retired defaultAgent field for old forms only. It is
	// never rendered and SMS routing never reads it.
	if agent == "C" && r.Form.Has("defaultAgent") {
		if v := strings.ToUpper(strings.TrimSpace(r.FormValue("defaultAgent"))); v == "A" || v == "C" {
			cfg.DefaultAgent = v
		}
	}

	s := agentSettings(*cfg, agent)
	for i, p := range s.Phones {
		if v := r.FormValue("access-" + agent + "-" + p.Number); v != "" {
			if agent == "G" {
				s.Phones[i].Access = AccessSMS
			} else {
				s.Phones[i].Access = normalizeAccess(v)
			}
		}
	}
	if agent != "G" && r.Form.Has(agentFieldName(agent, "callerNames")) {
		s.CallerNames = r.FormValue(agentFieldName(agent, "callerNames"))
	}
	if v, ok := formFlag(r, agentFieldName(agent, "ack")); ok {
		s.ReplyAck = boolPtr(v)
	}
	if v, ok := formFlag(r, agentFieldName(agent, "progress")); ok {
		s.ProgressUpdates = boolPtr(v)
	}
	if r.Form.Has(agentFieldName(agent, "progressInterval")) {
		if v, err := strconv.Atoi(r.FormValue(agentFieldName(agent, "progressInterval"))); err == nil && v >= 30 {
			s.ProgressIntervalSeconds = v
		}
	}
	if r.Form.Has(agentFieldName(agent, "ackDelay")) {
		if v, err := strconv.Atoi(r.FormValue(agentFieldName(agent, "ackDelay"))); err == nil && v >= 0 && v <= 300 {
			s.AckDelaySeconds = v
		}
	}
	putAgentSettings(cfg, agent, s)

	if code := strings.TrimSpace(r.FormValue(agentFieldName(agent, "code"))); code != "" {
		if err := setAgentCode(cfg, agent, code); err != nil {
			return err
		}
	}
	s = agentSettings(*cfg, agent)
	if want, ok := formFlag(r, agentFieldName(agent, "requireCode")); ok {
		if want && s.CodeHash == "" {
			return errAgentCodeMissing(agent)
		}
		s.RequireCode = want
	}
	putAgentSettings(cfg, agent, s)
	return nil
}

func errAgentCodeMissing(agent string) error {
	return &agentCodeError{agent: agent}
}

type agentCodeError struct{ agent string }

func (e *agentCodeError) Error() string {
	return "Set a security code for " + agentDisplayName(e.agent) + " before requiring one."
}
