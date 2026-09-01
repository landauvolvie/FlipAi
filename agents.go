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
// and one set of reply preferences, all shared. Each agent now owns its own
// allowlist, access kind, security code and reply behaviour. The same real phone
// may deliberately appear on both agents; in that case C:/A: selects the SMS
// destination and an unprefixed SMS uses the configured default agent.

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
	// Phones is this agent's allowlist. The same number may also be present on
	// the other agent; each copy keeps its own SMS/voice access policy.
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
	switch agent {
	case "A":
		return "Claude"
	case "B":
		return "ChatGPT / Codex and Claude"
	default:
		return "ChatGPT / Codex"
	}
}

func agentPhoneForSender(cfg Config, agent, raw string) (AgentPhone, bool) {
	number := normalizeUSPhone(raw)
	if number == "" {
		return AgentPhone{}, false
	}
	for _, p := range agentSettings(cfg, agent).Phones {
		if p.Number == number {
			return p, true
		}
	}
	return AgentPhone{}, false
}

// combinedAgentPhone is used only while admitting a message from a number that
// exists on both agents. The actual per-agent permissions stay separate; this
// synthetic value tells the transport whether either copy permits the transport.
func combinedAgentPhone(a, b AgentPhone) AgentPhone {
	p := a
	sms := a.AllowsSMS() || b.AllowsSMS()
	voice := a.AllowsVoice() || b.AllowsVoice()
	switch {
	case sms && voice:
		p.Access = AccessAll
	case sms:
		p.Access = AccessSMS
	default:
		p.Access = AccessVoice
	}
	return p
}

// agentForSender resolves a unique number directly. "B" is an internal marker
// meaning the number is allowed to text both agents, so the SMS shortcut must
// choose the destination. If only one copy permits SMS, that agent remains the
// only SMS destination even though the number may also call the other agent.
func agentForSender(cfg Config, raw string) (agent string, phone AgentPhone, ok bool) {
	c, cOK := agentPhoneForSender(cfg, "C", raw)
	a, aOK := agentPhoneForSender(cfg, "A", raw)
	switch {
	case cOK && aOK:
		switch {
		case c.AllowsSMS() && a.AllowsSMS():
			return "B", combinedAgentPhone(c, a), true
		case c.AllowsSMS():
			return "C", c, true
		case a.AllowsSMS():
			return "A", a, true
		default:
			return "B", combinedAgentPhone(c, a), true
		}
	case cOK:
		return "C", c, true
	case aOK:
		return "A", a, true
	default:
		return "", AgentPhone{}, false
	}
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

// normalizeAgentPhones cleans one agent's list. Duplicates inside the same
// list collapse, while the same number on the other agent is intentionally valid.
func normalizeAgentPhones(list []AgentPhone, _ map[string]string) ([]AgentPhone, error) {
	seen := map[string]bool{}
	out := make([]AgentPhone, 0, len(list))
	for _, p := range list {
		number := normalizeUSPhone(p.Number)
		if number == "" {
			return nil, fmt.Errorf("%q is not a 10-digit US or Canada phone number", strings.TrimSpace(p.Number))
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

// normalizeAgents cleans each phone list independently. Caller names remain
// exclusive because a phone call has no C:/A: shortcut with which to resolve the
// same displayed contact name on two agents.
func normalizeAgents(cfg *Config) error {
	claimedNames := map[string]string{}
	for _, agent := range []string{"C", "A"} {
		settings := agentSettings(*cfg, agent)
		cleaned, err := normalizeAgentPhones(settings.Phones, nil)
		if err != nil {
			return fmt.Errorf("%s numbers: %w", agentDisplayName(agent), err)
		}
		names, err := normalizeAllowedCallerLabels(settings.CallerNames, true)
		if err != nil {
			return fmt.Errorf("%s caller names: %w", agentDisplayName(agent), err)
		}
		// A caller belongs to one agent, exactly as a number does. Allowing the
		// same name on both agents would mean a caller reached whichever agent
		// happened to be the default -- a permission granted on one agent
		// silently deciding for the other, which is the one thing these lists
		// exist to prevent.
		for _, name := range names {
			key := strings.ToLower(name)
			if other, taken := claimedNames[key]; taken {
				return fmt.Errorf("%s caller names: %q is already an allowed caller on %s; a caller can reach one agent only",
					agentDisplayName(agent), name, agentDisplayName(other))
			}
			claimedNames[key] = agent
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

// salvageAgents keeps FlipAi startable when a stored configuration has bad
// entries. A shared number is valid and is preserved on both agents; only invalid
// or duplicate entries inside the same agent are dropped. Caller names remain
// exclusive because calls have no SMS shortcut.
func salvageAgents(cfg *Config) {
	claimedNames := map[string]bool{}
	for _, agent := range []string{"C", "A"} {
		s := agentSettings(*cfg, agent)
		seenNumbers := map[string]bool{}
		kept := make([]AgentPhone, 0, len(s.Phones))
		for _, p := range s.Phones {
			number := normalizeUSPhone(p.Number)
			if number == "" || seenNumbers[number] {
				continue
			}
			seenNumbers[number] = true
			p.Number = number
			p.Access = normalizeAccess(p.Access)
			kept = append(kept, p)
		}
		sort.Slice(kept, func(i, j int) bool { return kept[i].Number < kept[j].Number })
		s.Phones = kept
		names, _ := normalizeAllowedCallerLabels(s.CallerNames, false)
		// A caller name claimed by an earlier agent is dropped here, exactly as
		// a number is. Keeping it would leave a caller allowed on two agents,
		// and a caller allowed on two agents is decided by a default rather
		// than by anything the user granted.
		keptNames := names[:0]
		for _, name := range names {
			key := strings.ToLower(name)
			if claimedNames[key] {
				continue
			}
			claimedNames[key] = true
			keptNames = append(keptNames, name)
		}
		s.CallerNames = strings.Join(keptNames, "\n")
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
