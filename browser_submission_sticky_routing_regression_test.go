package main

import (
	"os"
	"strings"
	"testing"
)

func TestStickySMSRouteStaysSelectedUntilExplicitSwitch(t *testing.T) {
	cfg := defaultConfig(t.TempDir())

	route, err := selectStickySMSRoute("X: first question", cfg, "B", "")
	if err != nil || route.ID != smsRouteGrok {
		t.Fatalf("explicit X route = %#v, %v", route, err)
	}
	if got := explicitSMSRoute("follow-up without a prefix", cfg); got != "" {
		t.Fatalf("unprefixed follow-up unexpectedly selects route %q", got)
	}
	route, err = selectStickySMSRoute("follow-up without a prefix", cfg, "B", "route:"+smsRouteGrok)
	if err != nil || route.ID != smsRouteGrok {
		t.Fatalf("Grok sticky follow-up = %#v, %v", route, err)
	}

	route, err = selectStickySMSRoute("G: switch to Gemini", cfg, "B", "route:"+smsRouteGrok)
	if err != nil || route.ID != smsRouteGemini {
		t.Fatalf("explicit G switch = %#v, %v", route, err)
	}
	route, err = selectStickySMSRoute("second Gemini message", cfg, "B", "route:"+smsRouteGemini)
	if err != nil || route.ID != smsRouteGemini || route.Agent != "M" {
		t.Fatalf("Gemini sticky follow-up = %#v, %v", route, err)
	}

	route, err = selectStickySMSRoute("O: switch to ChatGPT", cfg, "B", "route:"+smsRouteGemini)
	if err != nil || route.ID != smsRouteChatGPTChat {
		t.Fatalf("explicit O switch = %#v, %v", route, err)
	}
	route, err = selectStickySMSRoute("now stay on ChatGPT", cfg, "B", "route:"+smsRouteChatGPTChat)
	if err != nil || route.ID != smsRouteChatGPTChat || route.Agent != "G" {
		t.Fatalf("ChatGPT sticky follow-up = %#v, %v", route, err)
	}
}

func TestBrowserDriversRequireFreshAcceptedTurn(t *testing.T) {
	checks := map[string][]string{
		"grok_chat_webview_windows.go": {
			"const sameText=",
			"const beforeCount=before.length",
			"const beforeLast=beforeCount?text(before[beforeCount-1]):''",
			"if(!now||sameText(now,input))return null",
			"Grok did not accept the Send action",
		},
		"gemini_chat_webview_windows.go": {
			"const sameText=",
			"const beforeCount=before.length",
			"const beforeLast=beforeCount?replyText(before[beforeCount-1]):''",
			"if(!now||sameText(now,input))return null",
			"Gemini did not accept the Send action",
		},
	}
	for path, wants := range checks {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, want := range wants {
			if !strings.Contains(s, want) {
				t.Fatalf("%s lost fresh-turn guard %q", path, want)
			}
		}
	}

	grok, err := os.ReadFile("grok_chat_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(grok), `[data-testid*="response" i]`) {
		t.Fatal("Grok must not classify every generic response-testid node as assistant output")
	}
	gemini, err := os.ReadFile("gemini_chat_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gemini), "beforeSet=new Set(before)") {
		t.Fatal("Gemini must compare response content, not DOM node identity, across re-renders")
	}
}
