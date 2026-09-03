package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	AccessAll   = "all"
	AccessSMS   = "sms"
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

type AgentSettings struct {
	Phones      []AgentPhone `json:"phones,omitempty"`
	CallerNames string       `json:"callerNames,omitempty"`

	RequireCode bool   `json:"requireCode,omitempty"`
	CodeSalt    string `json:"codeSalt,omitempty"`
	CodeHash    string `json:"codeHash,omitempty"`

	Instruction string `json:"replyStyleHint,omitempty"`

	ReplyAck                *bool `json:"replyAck,omitempty"`
	AckDelaySeconds         int   `json:"ackDelaySeconds,omitempty"`
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

func (s AgentSettings) ackDelay() time.Duration {
	if s.AckDelaySeconds <= 0 {
		return 0
	}
	return time.Duration(s.AckDelaySeconds) * time.Second
}

func agentSettings(cfg Config, agent string) AgentSettings {
	switch strings.ToUpper(strings.TrimSpace(agent)) {
	case "A":
		return cfg.Claude.AgentSettings
	case "G":
		return cfg.ChatGPT.AgentSettings
	case "H":
		return cfg.ClaudeChat.AgentSettings
	case "M":
		return cfg.GeminiChat.AgentSettings
	default:
		return cfg.Codex.AgentSettings
	}
}

func putAgentSettingsConfig(cfg *Config, agent string, s AgentSettings) {
	switch strings.ToUpper(strings.TrimSpace(agent)) {
	case "A":
		cfg.Claude.AgentSettings = s
	case "G":
		cfg.ChatGPT.AgentSettings = s
	case "H":
		cfg.ClaudeChat.AgentSettings = s
	case "M":
		cfg.GeminiChat.AgentSettings = s
	default:
		cfg.Codex.AgentSettings = s
	}
}

func agentDisplayName(agent string) string {
	marker := strings.ToUpper(strings.TrimSpace(agent))
	if marker == "B" {
		marker = "CA"
	}
	var names []string
	for _, item := range []struct{ key, name string }{
		{"C", "Codex"}, {"A", "Claude"}, {"G", "ChatGPT Chat"}, {"H", "Claude Chat"}, {"M", "Gemini Chat"},
	} {
		if strings.Contains(marker, item.key) {
			names = append(names, item.name)
		}
	}
	if len(names) == 0 {
		return "Codex"
	}
	if len(names) == 1 {
		return names[0]
	}
	if len(names) == 2 {
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
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

// agentForSender returns a marker containing every SMS-capable destination for
// the number. Browser chat agents are SMS-only and therefore never widen call
// permissions.
func agentForSender(cfg Config, raw string) (agent string, phone AgentPhone, ok bool) {
	var marker strings.Builder
	found := false
	sms, voice := false, false
	for _, candidate := range []string{"C", "A", "G", "H", "M"} {
		p, exists := agentPhoneForSender(cfg, candidate, raw)
		if !exists {
			continue
		}
		if !found {
			phone = p
			found = true
		}
		if p.AllowsSMS() {
			marker.WriteString(candidate)
			sms = true
		}
		if p.AllowsVoice() {
			voice = true
		}
	}
	if !found {
		return "", AgentPhone{}, false
	}
	switch {
	case sms && voice:
		phone.Access = AccessAll
	case sms:
		phone.Access = AccessSMS
	default:
		phone.Access = AccessVoice
	}
	return marker.String(), phone, true
}

func allAgentPhones(cfg Config) []AgentPhone {
	var out []AgentPhone
	for _, agent := range []string{"C", "A", "G", "H", "M"} {
		out = append(out, agentSettings(cfg, agent).Phones...)
	}
	return out
}

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

func normalizeAgents(cfg *Config) error {
	claimedNames := map[string]string{}
	for _, agent := range []string{"C", "A", "G", "H", "M"} {
		settings := agentSettings(*cfg, agent)
		cleaned, err := normalizeAgentPhones(settings.Phones, nil)
		if err != nil {
			return fmt.Errorf("%s numbers: %w", agentDisplayName(agent), err)
		}
		browserChat := agent == "G" || agent == "H" || agent == "M"
		if browserChat {
			for i := range cleaned {
				cleaned[i].Access = AccessSMS
			}
			settings.CallerNames = ""
		} else {
			names, err := normalizeAllowedCallerLabels(settings.CallerNames, true)
			if err != nil {
				return fmt.Errorf("%s caller names: %w", agentDisplayName(agent), err)
			}
			for _, name := range names {
				key := strings.ToLower(name)
				if other, taken := claimedNames[key]; taken {
					return fmt.Errorf("%s caller names: %q is already an allowed caller on %s; a caller can reach one agent only", agentDisplayName(agent), name, agentDisplayName(other))
				}
				claimedNames[key] = agent
			}
			settings.CallerNames = strings.Join(names, "\n")
		}
		settings.Phones = cleaned
		settings.Instruction = ""
		if settings.RequireCode && settings.CodeHash == "" {
			return fmt.Errorf("%s: set a security code before requiring one", agentDisplayName(agent))
		}
		putAgentSettingsConfig(cfg, agent, settings)
	}
	return nil
}

func migrateAgentSettings(cfg *Config) {
	if cfg.Security.AgentsMigrated {
		ensureAgentReplyDefaults(cfg)
		migrateChatGPTAgent(cfg)
		migrateClaudeChatAgent(cfg)
		migrateGeminiChatAgent(cfg)
		return
	}
	cfg.Security.AgentsMigrated = true
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
		}
	}
	if cfg.Security.RequireCode && cfg.Security.CodeHash != "" {
		for _, agent := range []string{"C", "A"} {
			s := agentSettings(*cfg, agent)
			if s.CodeHash != "" {
				continue
			}
			s.RequireCode = true
			s.CodeSalt = cfg.Security.CodeSalt
			s.CodeHash = cfg.Security.CodeHash
			putAgentSettingsConfig(cfg, agent, s)
		}
	}
	if hint := strings.TrimSpace(cfg.GoogleVoice.ReplyStyleHint); hint != "" && hint != defaultReplyStyleHint {
		for _, agent := range []string{"C", "A"} {
			s := agentSettings(*cfg, agent)
			if s.Instruction == "" {
				s.Instruction = hint
				putAgentSettingsConfig(cfg, agent, s)
			}
		}
	}
	ensureAgentReplyDefaults(cfg)
	migrateChatGPTAgent(cfg)
	migrateClaudeChatAgent(cfg)
	migrateGeminiChatAgent(cfg)
}

func migrateChatGPTAgent(cfg *Config) {
	if cfg.Security.ChatGPTAgentMigrated {
		return
	}
	cfg.Security.ChatGPTAgentMigrated = true
	migrateBrowserChatAgent(cfg, "G", []string{"C", "A"})
}

func migrateClaudeChatAgent(cfg *Config) {
	if cfg.Security.ClaudeChatAgentMigrated {
		return
	}
	// Claude Chat is a new security boundary. Mark the migration complete but
	// start with no phone numbers or PIN copied from any existing agent.
	cfg.Security.ClaudeChatAgentMigrated = true
}

func migrateGeminiChatAgent(cfg *Config) {
	if cfg.Security.GeminiChatAgentMigrated {
		return
	}
	// Gemini Chat is also a new security boundary: never inherit a phone or PIN.
	cfg.Security.GeminiChatAgentMigrated = true
}

func migrateBrowserChatAgent(cfg *Config, target string, sources []string) {
	t := agentSettings(*cfg, target)
	if len(t.Phones) == 0 {
		seen := map[string]bool{}
		for _, agent := range sources {
			for _, p := range agentSettings(*cfg, agent).Phones {
				if !p.AllowsSMS() || seen[p.Number] {
					continue
				}
				seen[p.Number] = true
				p.Access = AccessSMS
				t.Phones = append(t.Phones, p)
			}
		}
	}
	if t.CodeHash == "" {
		var secure []AgentSettings
		for _, agent := range sources {
			s := agentSettings(*cfg, agent)
			if s.RequireCode && s.CodeHash != "" {
				secure = append(secure, s)
			}
		}
		if len(secure) > 0 {
			first, same := secure[0], true
			for _, s := range secure[1:] {
				if s.CodeHash != first.CodeHash || s.CodeSalt != first.CodeSalt {
					same = false
					break
				}
			}
			if same {
				t.RequireCode = true
				t.CodeHash = first.CodeHash
				t.CodeSalt = first.CodeSalt
			}
		}
	}
	putAgentSettingsConfig(cfg, target, t)
}

func ensureAgentReplyDefaults(cfg *Config) {
	for _, agent := range []string{"C", "A", "G", "H", "M"} {
		s := agentSettings(*cfg, agent)
		if s.ReplyAck == nil {
			s.ReplyAck = boolPtr(true)
		}
		if s.ProgressUpdates == nil {
			s.ProgressUpdates = boolPtr(true)
		}
		if s.ProgressIntervalSeconds <= 0 {
			s.ProgressIntervalSeconds = 120
		}
		if s.AckDelaySeconds < 0 {
			s.AckDelaySeconds = 0
		}
		putAgentSettingsConfig(cfg, agent, s)
	}
}

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
	putAgentSettingsConfig(cfg, agent, s)
	return nil
}

func verifyAgentCode(s AgentSettings, code string) bool {
	if s.CodeSalt == "" || s.CodeHash == "" {
		return false
	}
	got := hashSecurityCode(code, s.CodeSalt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.CodeHash)) == 1
}

func salvageAgents(cfg *Config) {
	claimedNames := map[string]bool{}
	for _, agent := range []string{"C", "A", "G", "H", "M"} {
		s := agentSettings(*cfg, agent)
		seenNumbers := map[string]bool{}
		kept := make([]AgentPhone, 0, len(s.Phones))
		browserChat := agent == "G" || agent == "H" || agent == "M"
		for _, p := range s.Phones {
			number := normalizeUSPhone(p.Number)
			if number == "" || seenNumbers[number] {
				continue
			}
			seenNumbers[number] = true
			p.Number = number
			p.Access = normalizeAccess(p.Access)
			if browserChat {
				p.Access = AccessSMS
			}
			kept = append(kept, p)
		}
		sort.Slice(kept, func(i, j int) bool { return kept[i].Number < kept[j].Number })
		s.Phones = kept
		if browserChat {
			s.CallerNames = ""
		} else {
			names, _ := normalizeAllowedCallerLabels(s.CallerNames, false)
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
		}
		if s.CodeHash == "" {
			s.RequireCode = false
		}
		s.Instruction = ""
		putAgentSettingsConfig(cfg, agent, s)
	}
}
