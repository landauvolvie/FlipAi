package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Everything that belongs to one agent lives on that agent.
//
// FlipAi used to keep a single allowlist of phone numbers, one security code,
// and one set of reply preferences, all shared, with an agent chosen per message
// by a "C:" or "A:" prefix. That put the answer to "who may command this agent"
// in a different place from the agent itself. Now a number belongs to exactly
// one agent, carries what it is allowed to do, and each agent has its own code,
// its own framing line, and its own reply behaviour.

const (
	// AccessAll lets a number both text the agent and call it.
	AccessAll = "all"
	// AccessSMS lets a number text the agent only.
	AccessSMS = "sms"
	// AccessVoice lets a number call the agent only.
	AccessVoice = "voice"
)

func accessLabel(access string) string {
	switch access {
	case AccessSMS:
		return "Texts only"
	case AccessVoice:
		return "Calls only"
	default:
		return "Texts and calls"
	}
}

func normalizeAccess(access string) string {
	switch strings.ToLower(strings.TrimSpace(access)) {
	case AccessSMS:
		return AccessSMS
	case AccessVoice:
		return AccessVoice
	default:
		return AccessAll
	}
}

// AgentPhone is one number allowed to reach one agent.
type AgentPhone struct {
	Number string    `json:"number"`
	Label  string    `json:"label,omitempty"`
	Access string    `json:"access"`
	Added  time.Time `json:"added,omitempty"`
}

func (p AgentPhone) Display() string     { return formatUSPhone(p.Number) }
func (p AgentPhone) AccessLabel() string { return accessLabel(p.Access) }
func (p AgentPhone) AllowsSMS() bool     { return p.Access == AccessAll || p.Access == AccessSMS }
func (p AgentPhone) AllowsVoice() bool   { return p.Access == AccessAll || p.Access == AccessVoice }

// AgentSettings is the part of an agent's configuration that is the same shape
// for every agent. It is embedded, so the stored JSON stays flat and the keys
// that already existed keep their meaning.
type AgentSettings struct {
	// Phones is this agent's allowlist. A number may appear on one agent only.
	Phones []AgentPhone `json:"phones,omitempty"`

	// CallerNames are the contact names Google Voice displays for callers who
	// have no number FlipAi can read. Opt-in, exact matches.
	CallerNames string `json:"callerNames,omitempty"`

	// RequireCode makes this agent refuse a text that does not begin with its
	// own code. Off for a new install; each agent decides for itself.
	RequireCode bool   `json:"requireCode,omitempty"`
	CodeSalt    string `json:"codeSalt,omitempty"`
	CodeHash    string `json:"codeHash,omitempty"`

	// Instruction is the single line of framing appended to a text before it
	// reaches this agent. Empty means FlipAi's built-in wording.
	Instruction string `json:"replyStyleHint,omitempty"`

	// ReplyAck and ProgressUpdates are per agent. A nil pointer means the agent
	// has never been configured and follows the built-in default.
	ReplyAck                *bool `json:"replyAck,omitempty"`
	ProgressUpdates         *bool `json:"progressUpdates,omitempty"`
	ProgressIntervalSeconds int   `json:"progressIntervalSeconds,omitempty"`
}

func boolPtr(v bool) *bool { return &v }

func (s AgentSettings) ackEnabled() bool {
	if s.ReplyAck == nil {
		return true
	}
	return *s.ReplyAck
}

func (s AgentSettings) progressEnabled() bool {
	if s.ProgressUpdates == nil {
		return true
	}
	return *s.ProgressUpdates
}

// agentSettings returns the settings for "C" (Codex) or "A" (Claude).
func agentSettings(cfg Config, agent string) AgentSettings {
	if agent == "A" {
		return cfg.Claude.AgentSettings
	}
	return cfg.Codex.AgentSettings
}

func agentDisplayName(agent string) string {
	if agent == "A" {
		return "Claude"
	}
	return "ChatGPT / Codex"
}

// agentForSender answers the only question that matters when a text or a call
// arrives: which agent, if any, does this number reach, and for what.
func agentForSender(cfg Config, raw string) (agent string, phone AgentPhone, ok bool) {
	number := normalizeUSPhone(raw)
	if number == "" {
		return "", AgentPhone{}, false
	}
	for _, candidate := range []string{"C", "A"} {
		for _, p := range agentSettings(cfg, candidate).Phones {
			if p.Number == number {
				return candidate, p, true
			}
		}
	}
	return "", AgentPhone{}, false
}

// allAgentPhones lists every allowed number across agents, newest agent order
// first, for the places that need to show or check the whole set.
func allAgentPhones(cfg Config) []AgentPhone {
	var out []AgentPhone
	for _, agent := range []string{"C", "A"} {
		out = append(out, agentSettings(cfg, agent).Phones...)
	}
	return out
}

// smsAllowedFrom is the newline list the Google Voice email parser checks. It is
// derived from the per-agent lists so there is still exactly one source of
// truth for who may reach FlipAi.
func smsAllowedFrom(cfg Config) string {
	seen := map[string]bool{}
	var numbers []string
	for _, p := range allAgentPhones(cfg) {
		if !p.AllowsSMS() || seen[p.Number] {
			continue
		}
		seen[p.Number] = true
		numbers = append(numbers, p.Number)
	}
	sort.Strings(numbers)
	return strings.Join(numbers, "\n")
}

// normalizeAgentPhones cleans one agent's list and rejects a number that is
// already claimed by the other agent. Exclusivity is the point: a number reaches
// one agent, so there is never a question of which one answered.
func normalizeAgentPhones(list []AgentPhone, claimedElsewhere map[string]string) ([]AgentPhone, error) {
	seen := map[string]bool{}
	out := make([]AgentPhone, 0, len(list))
	for _, p := range list {
		number := normalizeUSPhone(p.Number)
		if number == "" {
			return nil, fmt.Errorf("%q is not a 10-digit US or Canada phone number", strings.TrimSpace(p.Number))
		}
		if other, taken := claimedElsewhere[number]; taken {
			return nil, fmt.Errorf("%s is already allowed on %s; a number can reach one agent only", formatUSPhone(number), agentDisplayName(other))
		}
		if seen[number] {
			continue
		}
		seen[number] = true
		label := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(p.Label, "\r", " "), "\n", " "))
		if len(label) > 60 {
			label = strings.TrimSpace(label[:60])
		}
		added := p.Added
		if added.IsZero() {
			added = time.Now()
		}
		out = append(out, AgentPhone{Number: number, Label: label, Access: normalizeAccess(p.Access), Added: added})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// normalizeAgents cleans both agents together, because exclusivity can only be
// checked with both lists in hand.
func normalizeAgents(cfg *Config) error {
	claimed := map[string]string{}
	for _, agent := range []string{"C", "A"} {
		settings := agentSettings(*cfg, agent)
		cleaned, err := normalizeAgentPhones(settings.Phones, claimed)
		if err != nil {
			return fmt.Errorf("%s numbers: %w", agentDisplayName(agent), err)
		}
		for _, p := range cleaned {
			claimed[p.Number] = agent
		}
		names, err := normalizeAllowedCallerLabels(settings.CallerNames, true)
		if err != nil {
			return fmt.Errorf("%s caller names: %w", agentDisplayName(agent), err)
		}
		settings.Phones = cleaned
		settings.CallerNames = strings.Join(names, "\n")
		settings.Instruction = strings.TrimSpace(settings.Instruction)
		if len(settings.Instruction) > 2000 {
			settings.Instruction = strings.TrimSpace(settings.Instruction[:2000])
		}
		if settings.RequireCode && settings.CodeHash == "" {
			return fmt.Errorf("%s: set a security code before requiring one", agentDisplayName(agent))
		}
		if agent == "A" {
			cfg.Claude.AgentSettings = settings
		} else {
			cfg.Codex.AgentSettings = settings
		}
	}
	if cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {
		cfg.DefaultAgent = "C"
	}
	return nil
}

// migrateAgentSettings moves a pre-agent configuration onto the agents.
//
// The old shape had one shared allowlist and one shared security code, with the
// agent picked per message by a prefix. There is no way to split a shared list
// by intent after the fact, so every number moves to the default agent -- the
// one an unprefixed text already went to -- and the user can move any of them.
func migrateAgentSettings(cfg *Config) {
	if cfg.Security.AgentsMigrated {
		// Already moved. Running it again would refill a list the user has since
		// emptied, or hand one agent's code back to the other.
		ensureAgentReplyDefaults(cfg)
		return
	}
	cfg.Security.AgentsMigrated = true
	migrated := false
	if len(cfg.Codex.Phones) == 0 && len(cfg.Claude.Phones) == 0 {
		numbers, err := normalizeAllowedPhoneList(cfg.GoogleVoice.AllowedFrom)
		if err == nil && len(numbers) > 0 {
			labels := map[string]string{}
			added := map[string]time.Time{}
			for _, n := range cfg.GoogleVoice.AllowedNumbers {
				labels[normalizeUSPhone(n.Number)] = n.Label
				added[normalizeUSPhone(n.Number)] = n.Added
			}
			phones := make([]AgentPhone, 0, len(numbers))
			for _, n := range numbers {
				phones = append(phones, AgentPhone{Number: n, Label: labels[n], Access: AccessAll, Added: added[n]})
			}
			if cfg.DefaultAgent == "A" {
				cfg.Claude.Phones = phones
			} else {
				cfg.Codex.Phones = phones
			}
			migrated = true
		}
	}
	// A shared security code applied to every message, so both agents inherit it
	// rather than one of them silently losing the protection.
	if cfg.Security.RequireCode && cfg.Security.CodeHash != "" {
		for _, agent := range []string{"C", "A"} {
			s := agentSettings(*cfg, agent)
			if s.CodeHash != "" {
				continue
			}
			s.RequireCode = true
			s.CodeSalt = cfg.Security.CodeSalt
			s.CodeHash = cfg.Security.CodeHash
			if agent == "A" {
				cfg.Claude.AgentSettings = s
			} else {
				cfg.Codex.AgentSettings = s
			}
			migrated = true
		}
	}
	// A framing line the user actually wrote becomes each agent's own starting
	// point. The built-in wording is not copied: an empty box has to keep
	// meaning "use FlipAi's own wording", or clearing one would silently refill
	// it on the next load.
	if hint := strings.TrimSpace(cfg.GoogleVoice.ReplyStyleHint); hint != "" && hint != defaultReplyStyleHint {
		for _, agent := range []string{"C", "A"} {
			s := agentSettings(*cfg, agent)
			if s.Instruction != "" {
				continue
			}
			s.Instruction = hint
			if agent == "A" {
				cfg.Claude.AgentSettings = s
			} else {
				cfg.Codex.AgentSettings = s
			}
		}
	}
	ensureAgentReplyDefaults(cfg)
	if migrated {
		// The old shared fields stay readable so a downgrade still works, but
		// nothing reads them for authorization any more.
		_ = migrated
	}
}

// ensureAgentReplyDefaults gives an agent that has never been configured the
// same reply behaviour the shared settings used to provide.
func ensureAgentReplyDefaults(cfg *Config) {
	for _, agent := range []string{"C", "A"} {
		s := agentSettings(*cfg, agent)
		if s.ReplyAck == nil {
			s.ReplyAck = boolPtr(cfg.GoogleVoice.ReplyAck)
		}
		if s.ProgressUpdates == nil {
			s.ProgressUpdates = boolPtr(cfg.GoogleVoice.ProgressUpdates)
		}
		if agent == "A" {
			cfg.Claude.AgentSettings = s
		} else {
			cfg.Codex.AgentSettings = s
		}
	}
}

// setAgentCode stores a per-agent security code. An empty code clears it.
func setAgentCode(cfg *Config, agent, code string) error {
	s := agentSettings(*cfg, agent)
	code = strings.TrimSpace(code)
	switch {
	case code == "":
		s.CodeSalt, s.CodeHash, s.RequireCode = "", "", false
	case len(code) < 6 || strings.ContainsAny(code, " \t\r\n"):
		return errors.New("a security code must be at least 6 characters with no spaces")
	default:
		salt, err := secureRandomToken(18)
		if err != nil {
			return err
		}
		s.CodeSalt, s.CodeHash = salt, hashSecurityCode(code, salt)
	}
	if agent == "A" {
		cfg.Claude.AgentSettings = s
	} else {
		cfg.Codex.AgentSettings = s
	}
	return nil
}

// verifyAgentCode checks a code against one agent's own stored code.
func verifyAgentCode(s AgentSettings, code string) bool {
	if s.CodeSalt == "" || s.CodeHash == "" {
		return false
	}
	got := hashSecurityCode(code, s.CodeSalt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.CodeHash)) == 1
}

// salvageAgents keeps FlipAi startable when a stored configuration cannot
// satisfy the rules -- a hand-edited file, or one written by a build that
// allowed a number on both agents. Whatever does not fit is dropped rather than
// refusing to load, and the first agent to claim a number keeps it.
func salvageAgents(cfg *Config) {
	claimed := map[string]bool{}
	for _, agent := range []string{"C", "A"} {
		s := agentSettings(*cfg, agent)
		kept := make([]AgentPhone, 0, len(s.Phones))
		for _, p := range s.Phones {
			number := normalizeUSPhone(p.Number)
			if number == "" || claimed[number] {
				continue
			}
			claimed[number] = true
			p.Number = number
			p.Access = normalizeAccess(p.Access)
			kept = append(kept, p)
		}
		sort.Slice(kept, func(i, j int) bool { return kept[i].Number < kept[j].Number })
		s.Phones = kept
		names, _ := normalizeAllowedCallerLabels(s.CallerNames, false)
		s.CallerNames = strings.Join(names, "\n")
		if s.CodeHash == "" {
			s.RequireCode = false
		}
		if agent == "A" {
			cfg.Claude.AgentSettings = s
		} else {
			cfg.Codex.AgentSettings = s
		}
	}
	if cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {
		cfg.DefaultAgent = "C"
	}
}
