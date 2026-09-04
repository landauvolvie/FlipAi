package main

import (
	"strings"
	"testing"
)

func TestExactWebAgentMarksAreIsolatedDataImages(t *testing.T) {
	for _, name := range []string{"chatgpt", "codex", "claude-code", "claude", "grok", "gemini"} {
		mark, ok := exactWebAgentBrandMarks[name]
		if !ok {
			t.Fatalf("missing exact web-sourced brand mark for %s", name)
		}
		s := string(mark)
		if !strings.Contains(s, `src="data:image/svg+xml;base64,`) {
			t.Fatalf("%s is not rendered as an isolated SVG data image", name)
		}
		if strings.Contains(s, `<svg`) {
			t.Fatalf("%s leaked an inline SVG into the page; gradient IDs can collide", name)
		}
	}
}

func TestExactAgentsPageUsesNeutralBrandWrappers(t *testing.T) {
	body := exactWebAgentsHTML()
	for _, old := range []string{
		`class="bmark codex"`,
		`class="bmark claude-code"`,
		`class="bmark chatgpt"`,
		`class="bmark claude"`,
		`class="bmark grok"`,
		`class="bmark gemini"`,
	} {
		if strings.Contains(body, old) {
			t.Fatalf("Agents page still applies legacy product wrapper styling %q", old)
		}
	}
	for _, brand := range []string{
		`{{brand "codex"}}`,
		`{{brand "claude-code"}}`,
		`{{brand "chatgpt"}}`,
		`{{brand "claude"}}`,
		`{{brand "grok"}}`,
		`{{brand "gemini"}}`,
	} {
		if !strings.Contains(body, brand) {
			t.Fatalf("Agents page is missing %s", brand)
		}
	}
	if !strings.Contains(body, `.bmark.exact-agent-brand{background:transparent!important`) {
		t.Fatal("Agents page does not neutralize old product-specific backgrounds")
	}
}

func TestExactActivityUsesNeutralBrandWrapper(t *testing.T) {
	body := exactWebActivityHTML()
	if !strings.Contains(body, `activity2-logo exact-agent-brand`) {
		t.Fatal("Activity does not use the isolated neutral brand wrapper")
	}
	if strings.Contains(body, `activity2-logo '+m.kind+'`) {
		t.Fatal("Activity still adds product-specific classes that recolor exact logos")
	}
	if !strings.Contains(body, `.activity2-logo.exact-agent-brand{background:transparent!important`) {
		t.Fatal("Activity does not neutralize old product-specific backgrounds")
	}
	for _, kind := range []string{`kind:'codex'`, `kind:'claude-code'`, `kind:'chatgpt'`, `kind:'claude'`, `kind:'grok'`, `kind:'gemini'`} {
		if !strings.Contains(body, kind) {
			t.Fatalf("Activity is missing brand mapping %s", kind)
		}
	}
}
