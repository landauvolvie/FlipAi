package main

import (
	"os"
	"strings"
	"testing"
)

func TestGeminiWaitsForSettledFullResponse(t *testing.T) {
	b, err := os.ReadFile("gemini_chat_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"responseFinishedChrome",
		"good response|bad response|regenerate|copy response|more options|share",
		"const quietFor=at-lastChangedAt",
		"!stop()&&runningFor>=1500&&quietFor>=3000",
		"responseFinishedChrome(node)&&quietFor>=4000",
		"quietFor>=8000",
		"parts.join('\\n\\n').trim()",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Gemini completion driver is missing safeguard %q", want)
		}
	}
	for _, retired := range []string{
		"responseFinishedChrome(node)&&stable>=2",
		"!stop()&&stable>=5",
		"stable>=16",
	} {
		if strings.Contains(s, retired) {
			t.Fatalf("Gemini must not use premature 250ms stability threshold %q", retired)
		}
	}
}
