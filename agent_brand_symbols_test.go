package main

import (
	"strings"
	"testing"
)

func TestSuppliedAgentBrandMarksCoverEveryAgent(t *testing.T) {
	for _, name := range []string{"chatgpt", "codex", "claude-code", "claude", "grok", "gemini"} {
		mark, ok := suppliedAgentBrandMarks[name]
		if !ok || !strings.Contains(string(mark), "<svg") {
			t.Fatalf("missing supplied symbol for %s", name)
		}
	}
}

func TestAgentsUseCorrectBrandForEachAgent(t *testing.T) {
	body := correctedAgentsBrandHTML()
	for _, want := range []string{
		`for="agent-codex"`, `{{brand "codex"}}`,
		`for="agent-claude"`, `{{brand "claude-code"}}`,
		`for="agent-chatgpt"`, `{{brand "chatgpt"}}`,
		`for="agent-claude-chat"`, `{{brand "claude"}}`,
		`for="agent-gemini-chat"`, `{{brand "gemini"}}`,
		`for="agent-grok-chat"`, `{{brand "grok"}}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("corrected Agents page is missing %q", want)
		}
	}
	if strings.Contains(body, `<span class="bmark grok">𝕏</span>`) {
		t.Fatal("Grok still uses the generic X glyph")
	}
}

func TestActivityUsesCorrectSharedBrands(t *testing.T) {
	body := correctedActivityBrandHTML()
	for _, want := range []string{`kind:'claude-code'`, `brandIcon(m.kind)`, `All stages`, `Privacy:`} {
		if !strings.Contains(body, want) {
			t.Fatalf("corrected Activity page is missing %q", want)
		}
	}
	for _, old := range []string{`&gt;_</span>`, `M5 5l14 14M19 5 5 19`, `M12 1.8c.7 5.7`} {
		if strings.Contains(body, old) {
			t.Fatalf("Activity still contains an old placeholder symbol fragment %q", old)
		}
	}
}
