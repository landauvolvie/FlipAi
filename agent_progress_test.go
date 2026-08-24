package main

import (
	"testing"
	"time"
)

// Each agent can set its own heartbeat cadence, because a fifteen-minute Codex
// build and a one-minute Claude question do not want the same reporting.
func TestPerAgentProgressIntervalFallsBackToTheSharedSetting(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	cfg.GoogleVoice.ProgressIntervalSeconds = 120

	if got := cfg.progressIntervalFor("A"); got != 2*time.Minute {
		t.Errorf("with no override Claude follows the shared setting, got %v", got)
	}
	if got := cfg.progressIntervalFor("C"); got != 2*time.Minute {
		t.Errorf("with no override Codex follows the shared setting, got %v", got)
	}

	cfg.Claude.ProgressIntervalSeconds = 900
	cfg.Codex.ProgressIntervalSeconds = 60
	if got := cfg.progressIntervalFor("A"); got != 15*time.Minute {
		t.Errorf("Claude override = %v, want 15m", got)
	}
	if got := cfg.progressIntervalFor("C"); got != time.Minute {
		t.Errorf("Codex override = %v, want 1m", got)
	}
	// One agent's override must not leak into the other.
	cfg.Codex.ProgressIntervalSeconds = 0
	if got := cfg.progressIntervalFor("C"); got != 2*time.Minute {
		t.Errorf("clearing an override returns to the shared setting, got %v", got)
	}
	if got := cfg.progressIntervalFor("A"); got != 15*time.Minute {
		t.Errorf("the other agent's override must survive, got %v", got)
	}

	// The floor applies to an override too, so no cadence can spam the phone.
	cfg.Claude.ProgressIntervalSeconds = 5
	if got := cfg.progressIntervalFor("A"); got != 2*time.Minute {
		t.Errorf("a too-small override must fall back to the safe default, got %v", got)
	}
}

func TestAgentsPageOffersPerAgentProgressInterval(t *testing.T) {
	a := newTestApp(t)
	body := a.do(t, "GET", "/agents", nil).Body.String()
	for _, want := range []string{`name="claudeProgressInterval"`, `name="codexProgressInterval"`, "Follow shared setting"} {
		if !contains(body, want) {
			t.Errorf("Agents page is missing %q", want)
		}
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
