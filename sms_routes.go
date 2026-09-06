package main

import (
	"errors"
	"fmt"
	"strings"
)

const (
	smsRouteChatGPTChat     = "O"
	smsRouteChatGPTWork     = "OW"
	smsRouteCodex           = "OC"
	smsRouteClaudeChat      = "A"
	smsRouteClaudeCodeWeb   = "AC"
	smsRouteClaudeCowork    = "AW"
	smsRouteClaudeCodeLocal = "AL"
	smsRouteGemini          = "G"
	smsRouteCopilot         = "M"
	smsRouteGrok            = "X"

	browserModeChat   = "chat"
	browserModeWork   = "work"
	browserModeCode   = "code"
	browserModeCowork = "cowork"

	browserModeMarkerStart = "[[FLIPAI_BROWSER_MODE:"
	browserModeMarkerEnd   = "]]"
)

type smsRouteSpec struct {
	ID      string
	Agent   string
	Mode    string
	Prefix  string
	Display string
}

func smsRouteSpecs(_ Config) []smsRouteSpec {
	// Public shortcuts are intentionally fixed and provider-grouped. Longer
	// prefixes come first so OW:/OC:/AC:/AW:/AL: can never be swallowed by O:
	// or A:. The old configurable prefixes remain usable internally while old
	// bridge.json files migrate naturally through rewriteSMSRoutePrefix.
	return []smsRouteSpec{
		{ID: smsRouteChatGPTWork, Agent: "G", Mode: browserModeWork, Prefix: "OW", Display: "ChatGPT Work"},
		{ID: smsRouteCodex, Agent: "C", Prefix: "OC", Display: "Codex"},
		{ID: smsRouteClaudeCodeWeb, Agent: "H", Mode: browserModeCode, Prefix: "AC", Display: "Claude Code Web"},
		{ID: smsRouteClaudeCowork, Agent: "H", Mode: browserModeCowork, Prefix: "AW", Display: "Claude Cowork"},
		{ID: smsRouteClaudeCodeLocal, Agent: "A", Prefix: "AL", Display: "Claude Code Local"},
		{ID: smsRouteChatGPTChat, Agent: "G", Mode: browserModeChat, Prefix: "O", Display: "ChatGPT Chat"},
		{ID: smsRouteClaudeChat, Agent: "H", Mode: browserModeChat, Prefix: "A", Display: "Claude Chat"},
		{ID: smsRouteGemini, Agent: "M", Prefix: "G", Display: "Gemini Chat"},
		{ID: smsRouteCopilot, Agent: "P", Prefix: "M", Display: "Microsoft Copilot Chat"},
		{ID: smsRouteGrok, Agent: "X", Prefix: "X", Display: "Grok Chat"},
	}
}

func smsRouteByID(cfg Config, id string) (smsRouteSpec, bool) {
	id = strings.ToUpper(strings.TrimSpace(id))
	for _, route := range smsRouteSpecs(cfg) {
		if route.ID == id {
			return route, true
		}
	}
	return smsRouteSpec{}, false
}

func defaultSMSRouteForAgent(agent string) string {
	switch strings.ToUpper(strings.TrimSpace(agent)) {
	case "C":
		return smsRouteCodex
	case "A":
		return smsRouteClaudeCodeLocal
	case "G":
		return smsRouteChatGPTChat
	case "H":
		return smsRouteClaudeChat
	case "M":
		return smsRouteGemini
	case "P":
		return smsRouteCopilot
	case "X":
		return smsRouteGrok
	default:
		return ""
	}
}

func routeDisplayName(cfg Config, routeID, agent string) string {
	if route, ok := smsRouteByID(cfg, routeID); ok {
		return route.Display
	}
	return agentDisplayName(agent)
}

func explicitSMSRoute(raw string, cfg Config) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	candidates := []string{raw}
	if fields := strings.Fields(raw); len(fields) > 1 {
		candidates = append(candidates, strings.TrimSpace(strings.TrimPrefix(raw, fields[0])))
	}
	newWord := configuredNewSessionCommand(cfg)
	for _, candidate := range candidates {
		for _, route := range smsRouteSpecs(cfg) {
			if _, ok := stripAgentCommandPrefix(candidate, route.Prefix); ok || isAgentNewSession(candidate, route.Prefix, newWord) {
				return route.ID
			}
		}
	}
	return ""
}

func smsRouteAllowed(sourceAgent string, route smsRouteSpec) bool {
	sourceAgent = strings.ToUpper(strings.TrimSpace(sourceAgent))
	if sourceAgent == "B" {
		return true
	}
	return route.Agent != "" && strings.Contains(sourceAgent, route.Agent)
}

func legacyStickySMSRoute(sticky string) string {
	// LastAgentBySender historically stored internal engine IDs. Keep reading
	// those values so upgrades do not point an old ChatGPT sticky G at Gemini.
	switch strings.ToUpper(strings.TrimSpace(sticky)) {
	case "C":
		return smsRouteCodex
	case "A":
		return smsRouteClaudeCodeLocal
	case "G":
		return smsRouteChatGPTChat
	case "H":
		return smsRouteClaudeChat
	case "M":
		return smsRouteGemini
	case "P":
		return smsRouteCopilot
	case "X":
		return smsRouteGrok
	default:
		return ""
	}
}

func decodeStickySMSRoute(sticky string) string {
	sticky = strings.TrimSpace(sticky)
	if strings.HasPrefix(strings.ToLower(sticky), "route:") {
		return strings.ToUpper(strings.TrimSpace(sticky[len("route:"):]))
	}
	return legacyStickySMSRoute(sticky)
}

func selectStickySMSRoute(raw string, cfg Config, sourceAgent, sticky string) (smsRouteSpec, error) {
	if explicit := explicitSMSRoute(raw, cfg); explicit != "" {
		route, ok := smsRouteByID(cfg, explicit)
		if !ok {
			return smsRouteSpec{}, fmt.Errorf("unknown SMS route %q", explicit)
		}
		if !smsRouteAllowed(sourceAgent, route) {
			return smsRouteSpec{}, wrongAgentForNumber(sourceAgent, route.Agent)
		}
		return route, nil
	}
	if stickyID := decodeStickySMSRoute(sticky); stickyID != "" {
		if route, ok := smsRouteByID(cfg, stickyID); ok && smsRouteAllowed(sourceAgent, route) {
			return route, nil
		}
	}
	clean := strings.ToUpper(strings.TrimSpace(sourceAgent))
	if len(clean) == 1 {
		if route, ok := smsRouteByID(cfg, defaultSMSRouteForAgent(clean)); ok && smsRouteAllowed(sourceAgent, route) {
			return route, nil
		}
	}
	return smsRouteSpec{}, errors.New("no SMS agent is selected for this phone yet; use O: ChatGPT, OW: ChatGPT Work, OC: Codex, A: Claude Chat, AC: Claude Code Web, AW: Claude Cowork, AL: Claude Code Local, G: Gemini, M: Copilot, or X: Grok")
}

func underlyingPrefixForRoute(cfg Config, route smsRouteSpec) string {
	switch route.Agent {
	case "C":
		return configuredCodexPrefix(cfg)
	case "A":
		return configuredClaudePrefix(cfg)
	case "G":
		return configuredChatGPTPrefix(cfg)
	case "H":
		return configuredClaudeChatPrefix(cfg)
	case "M":
		return configuredGeminiChatPrefix(cfg)
	case "P":
		return configuredCopilotChatPrefix(cfg)
	case "X":
		return configuredGrokChatPrefix(cfg)
	default:
		return route.Prefix
	}
}

func rewriteSMSRoutePrefix(raw, from, to string) string {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || strings.EqualFold(from, to) {
		return raw
	}
	rewrite := func(value string) (string, bool) {
		if tail, ok := stripAgentCommandPrefix(value, from); ok {
			return to + ": " + tail, true
		}
		fields := strings.Fields(strings.TrimSpace(value))
		if len(fields) == 2 && strings.EqualFold(fields[0], from) {
			return to + " " + fields[1], true
		}
		return value, false
	}
	if out, ok := rewrite(raw); ok {
		return out
	}
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) > 1 {
		security := fields[0]
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), security))
		if out, ok := rewrite(rest); ok {
			return security + " " + out
		}
	}
	return raw
}

func markBrowserModeCommand(command, mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case browserModeChat, browserModeWork, browserModeCode, browserModeCowork:
		return browserModeMarkerStart + mode + browserModeMarkerEnd + strings.TrimSpace(command)
	default:
		return strings.TrimSpace(command)
	}
}

func extractBrowserModeCommand(command string) (string, string) {
	command = strings.TrimSpace(command)
	start := strings.Index(command, browserModeMarkerStart)
	if start < 0 {
		return "", command
	}
	payloadStart := start + len(browserModeMarkerStart)
	relEnd := strings.Index(command[payloadStart:], browserModeMarkerEnd)
	if relEnd < 0 {
		return "", command
	}
	end := payloadStart + relEnd
	mode := strings.ToLower(strings.TrimSpace(command[payloadStart:end]))
	clean := strings.TrimSpace(command[:start] + command[end+len(browserModeMarkerEnd):])
	switch mode {
	case browserModeChat, browserModeWork, browserModeCode, browserModeCowork:
		return mode, clean
	default:
		return "", command
	}
}
