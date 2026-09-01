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
		IgnoredPipes:  []string{"codex-ipc"},
		DebugPipe:     true,
	}
	got := p.summary()
	for _, want := range []string{"ChatGPT desktop processes: 3", "9222", "openai-chat-ipc", "Codex pipes ignored", "background transport candidate"} {
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

func TestChatGPTDirectProbeDoesNotTreatPipeNamesAsOwnership(t *testing.T) {
	p := chatGPTDirectProbeResult{
		Supported:    true,
		ProcessCount: 13,
		NamedPipes:   []string{"openai-chat-ipc"},
		IgnoredPipes: []string{"codex-browser-use-abc", "codex-computer-use-def", "codex-ipc"},
	}
	if p.provenTransport() {
		t.Fatal("a machine-global pipe name must not prove ChatGPT ownership")
	}
	got := p.summary()
	for _, want := range []string{"ownership not proven", "Codex pipes ignored", "ChatGPT is NOT connected or enabled"} {
		if !strings.Contains(got, want) {
			t.Fatalf("false-positive regression summary missing %q: %s", want, got)
		}
	}
}

func TestChatGPTDirectAgentsPaneCannotBeMistakenForEnablement(t *testing.T) {
	body := chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))
	for _, want := range []string{
		"ChatGPT Chat",
		"Not connected",
		`data-test="/chatgpt-direct/probe"`,
		"Run backend diagnostic",
		"This button does not enable ChatGPT",
		"Not used",
		"SMS routing",
		"Unavailable in this build",
		"If it finds only Codex pipes, those are ignored",
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
