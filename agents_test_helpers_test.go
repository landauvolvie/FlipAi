package main

// Helpers for tests written before allowed numbers, security codes and reply
// behaviour moved onto the agent they reach.

func allowTestNumber(cfg *Config, agent string, numbers ...string) {
	s := agentSettings(*cfg, agent)
	for _, n := range numbers {
		if number := normalizeUSPhone(n); number != "" {
			s.Phones = append(s.Phones, AgentPhone{Number: number, Access: AccessAll})
		}
	}
	if agent == "A" {
		cfg.Claude.AgentSettings = s
	} else {
		cfg.Codex.AgentSettings = s
	}
	cfg.GoogleVoice.AllowedFrom = smsAllowedFrom(*cfg)
}

// requireTestCode gives both agents the same code, which is how a single shared
// code behaved before codes became per agent.
func requireTestCode(cfg *Config, code string) error {
	for _, agent := range []string{"C", "A"} {
		if err := setAgentCode(cfg, agent, code); err != nil {
			return err
		}
		s := agentSettings(*cfg, agent)
		s.RequireCode = true
		if agent == "A" {
			cfg.Claude.AgentSettings = s
		} else {
			cfg.Codex.AgentSettings = s
		}
	}
	return nil
}

func setTestReplyBehaviour(cfg *Config, ack, progress bool) {
	for _, agent := range []string{"C", "A"} {
		s := agentSettings(*cfg, agent)
		s.ReplyAck = boolPtr(ack)
		s.ProgressUpdates = boolPtr(progress)
		if agent == "A" {
			cfg.Claude.AgentSettings = s
		} else {
			cfg.Codex.AgentSettings = s
		}
	}
}
