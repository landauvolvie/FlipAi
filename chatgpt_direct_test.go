package main

import (
	"strings"
	"testing"
)

func TestChatGPTDirectProbeSummaryDoesNotExposeCredentials(t *testing.T) {
	p := chatGPTDirectProbeResult{
		Supported:     true,
		ProcessCount:  3,
		ProcessNames:  []string{"ChatGPT.exe"},
		LoopbackPorts: []int{9222, 9222, 41123},
		CDPPorts:      []int{9222},
		NamedPipes:    []string{"openai-chat-ipc"},
		DebugPipe:     true,
	}
	got := p.summary()
	for _, want := range []string{"ChatGPT desktop processes: 3", "9222", "openai-chat-ipc", "background transport candidate"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"cookie", "bearer", "local storage", "access token"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Fatalf("summary must not expose credential material: %s", got)
		}
	}
}

func TestChatGPTDirectAgentsPaneIsExplicitlyExperimental(t *testing.T) {
	body := chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))
	for _, want := range []string{
		"ChatGPT Chat",
		"Direct backend experiment",
		`data-test="/codex/test?chatgpt-direct=1"`,
		"Visible UI automation",
		"Disabled",
		"SMS routing",
		"Not enabled yet",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("ChatGPT direct pane missing %q", want)
		}
	}
}

func TestUniqueSortedDirectProbeMetadata(t *testing.T) {
	ints := uniqueSortedInts([]int{9, 3, 9, -1, 70000})
	if len(ints) != 2 || ints[0] != 3 || ints[1] != 9 {
		t.Fatalf("unexpected ints: %#v", ints)
	}
	ss := uniqueSortedStrings([]string{" b ", "a", "b", ""})
	if len(ss) != 2 || ss[0] != "a" || ss[1] != "b" {
		t.Fatalf("unexpected strings: %#v", ss)
	}
}
