package main

import (
	"os"
	"strings"
	"testing"
)

func TestGeminiFinishedResponseCannotBeHeldByStaleStopControl(t *testing.T) {
	b, err := os.ReadFile("gemini_chat_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"responseFinishedChrome",
		"good response|bad response|regenerate|copy response|more options|share",
		"responseFinishedChrome(node)&&stable>=2",
		"!stop()&&stable>=5",
		"stable>=16",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Gemini completion driver is missing safeguard %q", want)
		}
	}
}
