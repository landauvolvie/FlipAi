package main

import (
	"strings"
	"testing"
)

func TestActivityRedesignIncludesEveryAgent(t *testing.T) {
	for _, want := range []string{
		`data-activity-agent="C"`,
		`data-activity-agent="A"`,
		`data-activity-agent="G"`,
		`data-activity-agent="H"`,
		`data-activity-agent="M"`,
		`data-activity-agent="X"`,
		`ChatGPT Chat`,
		`Codex`,
		`Claude Code`,
		`Claude Chat`,
		`Gemini`,
		`Grok`,
		`company:'OpenAI'`,
		`company:'Anthropic'`,
		`company:'Google'`,
		`company:'xAI'`,
	} {
		if !strings.Contains(activityRedesignHTML, want) {
			t.Fatalf("Activity redesign is missing %q", want)
		}
	}
}

func TestActivityRedesignLogsBothDirections(t *testing.T) {
	for _, want := range []string{
		`name:'Incoming'`,
		`name:'Outgoing'`,
		`e.stage==='routing'`,
		`e.stage==='reply'`,
		`Search by agent, model, message, or status`,
	} {
		if !strings.Contains(activityRedesignHTML, want) {
			t.Fatalf("Activity redesign is missing message-flow behavior %q", want)
		}
	}
}
