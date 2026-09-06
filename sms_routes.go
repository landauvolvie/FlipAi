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
	// or A:. Configured legacy prefixes are accepted separately as aliases.
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

func configuredSMSRouteAliases(cfg Config) []smsRouteSpec {
	// Existing installs may have customized the old single-agent prefixes. Keep
	// those aliases readable, but never let them override a valid new public
	// shortcut. They are only a fallback when the new destination is not allowed
	// for the sender.
	return []smsRouteSpec{
		{ID: smsRouteCodex, Agent: "C", Prefix: configuredCodexPrefix(cfg)},
		{ID: smsRouteClaudeCodeLocal, Agent: "A", Prefix: configuredClaudePrefix(cfg)},
		{ID: smsRouteChatGPTChat, Agent: "G", Mode: browserModeChat, Prefix: configuredChatGPTPrefix(cfg)},
		{ID: smsRouteClaudeChat, Agent: "H", Mode: browserModeChat, Prefix: configuredClaudeChatPrefix(cfg)},
		{ID: smsRouteGemini, Agent: "M", Prefix: configuredGeminiChatPrefix(cfg)},
		{ID: smsRouteGrok, Agent: "X", Prefix: configuredGrokChatPrefix(cfg)},
		{ID: smsRouteCopilot, Agent: "P", Prefix: configuredCopilotChatPrefix(cfg)},
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

func smsCommandCandidates(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	candidates := []string{raw}
	if fields := strings.Fields(raw); len(fields) > 1 {
		candidates = append(candidates, strings.TrimSpace(strings.TrimPrefix(raw, fields[0])))
	}
	return candidates
}

// stripSMSRouteCommand recognizes both the normal public form (OW: task) and
// the fresh-session form (OW NEW: task or OW NEW). The word is configurable
// and comparisons are case-insensitive, so "OW new:" works with the default.
func stripSMSRouteCommand(candidate, prefix, newWord string) (tail string, fresh bool, ok bool) {
	candidate = strings.TrimSpace(candidate)
	prefix = strings.TrimSpace(prefix)
	newWord = strings.TrimSpace(newWord)
	if candidate == "" || prefix == "" {
		return "", false, false
	}
	if i := strings.Index(candidate, ":"); i > 0 {
		head := strings.Fields(strings.TrimSpace(candidate[:i]))
		switch {
		case len(head) == 1 && strings.EqualFold(head[0], prefix):
			return strings.TrimSpace(candidate[i+1:]), false, true
		case len(head) == 2 && newWord != "" && strings.EqualFold(head[0], prefix) && strings.EqualFold(head[1], newWord):
			return strings.TrimSpace(candidate[i+1:]), true, true
		}
	}
	fields := strings.Fields(candidate)
	if len(fields) == 2 && newWord != "" && strings.EqualFold(fields[0], prefix) && strings.EqualFold(fields[1], newWord) {
		return "", true, true
	}
	return "", false, false
}

func routeMatchesCommand(candidate string, route smsRouteSpec, newWord string) bool {
	if route.Prefix == "" {
		return false
	}
	_, _, ok := stripSMSRouteCommand(candidate, route.Prefix, newWord)
	return ok
}

func explicitSMSRoute(raw string, cfg Config) string {
	newWord := configuredNewSessionCommand(cfg)
	for _, candidate := range smsCommandCandidates(raw) {
		for _, route := range smsRouteSpecs(cfg) {
			if routeMatchesCommand(candidate, route, newWord) {
				return route.ID
			}
		}
		for _, route := range configuredSMSRouteAliases(cfg) {
			if routeMatchesCommand(candidate, route, newWord) {
				return route.ID
			}
		}
	}
	return ""
}

func explicitAllowedConfiguredAlias(raw string, cfg Config, sourceAgent string) (smsRouteSpec, bool) {
	newWord := configuredNewSessionCommand(cfg)
	for _, candidate := range smsCommandCandidates(raw) {
		for _, alias := range configuredSMSRouteAliases(cfg) {
			if !routeMatchesCommand(candidate, alias, newWord) || !smsRouteAllowed(sourceAgent, alias) {
				continue
			}
			if route, ok := smsRouteByID(cfg, alias.ID); ok {
				return route, true
			}
		}
	}
	return smsRouteSpec{}, false
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
		if smsRouteAllowed(sourceAgent, route) {
			return route, nil
		}
		// A new public shortcut can collide with an old configured prefix (for
		// example A used to mean local Claude and now means Claude Chat). If the
		// sender is not authorized for the new destination, preserve the old
		// configured route as a migration fallback rather than silently widening
		// permissions. Once the new destination is allowed, the new meaning wins.
		if legacy, ok := explicitAllowedConfiguredAlias(raw, cfg, sourceAgent); ok {
			return legacy, nil
		}
		return smsRouteSpec{}, wrongAgentForNumber(sourceAgent, route.Agent)
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

// rewriteSMSRouteCommand converts the selected public/legacy route into the
// existing internal parser prefix and reports whether NEW was requested. It
// deliberately strips the inline NEW modifier from a task-bearing command; the
// fresh flag travels separately on remoteCommand.New, so downstream parsers see
// the same prompt they have always handled.
func rewriteSMSRouteCommand(raw string, fromPrefixes []string, to, newWord string) (string, bool) {
	to = strings.TrimSpace(to)
	if to == "" {
		return raw, false
	}
	rewrite := func(value string) (string, bool, bool) {
		for _, from := range fromPrefixes {
			from = strings.TrimSpace(from)
			if from == "" {
				continue
			}
			tail, fresh, ok := stripSMSRouteCommand(value, from, newWord)
			if !ok {
				continue
			}
			if fresh {
				if tail == "" {
					return to + " " + newWord, true, true
				}
				return to + ": " + tail, true, true
			}
			if strings.EqualFold(from, to) {
				return value, false, true
			}
			return to + ": " + tail, false, true
		}
		return value, false, false
	}
	if out, fresh, ok := rewrite(raw); ok {
		return out, fresh
	}
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) > 1 {
		security := fields[0]
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), security))
		if out, fresh, ok := rewrite(rest); ok {
			return security + " " + out, fresh
		}
	}
	return raw, false
}

// rewriteSMSRoutePrefix keeps the old helper available to tests/callers that
// only need prefix translation. Fresh-session parsing uses rewriteSMSRouteCommand.
func rewriteSMSRoutePrefix(raw, from, to string) string {
	out, _ := rewriteSMSRouteCommand(raw, []string{from}, to, defaultNewSessionCommand)
	return out
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
