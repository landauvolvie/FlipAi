package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestChatGPTDirectProbeSummaryDoesNotExposeCredentials(t *testing.T) {
	p := chatGPTDirectProbeResult{
		Supported:           true,
		ProcessCount:        3,
		ProcessNames:        []string{"ChatGPT.exe"},
		LoopbackPorts:       []int{9222, 9222, 41123},
		CDPPorts:            []int{9222},
		NamedPipes:          []string{"openai-chat-ipc"},
		IgnoredPipes:        []string{"codex-ipc"},
		DebugPipe:           true,
		StaticFilesScanned:  2,
		StaticResourceFiles: []string{"resources/app.asar", "resources/preload.js"},
		ProtocolMarkers:     []string{"/backend-api/conversation", "https://chatgpt.com"},
		RuntimeArchitecture: "WinUI/native Windows shell with WebView2 content",
		RuntimeSignals:      []string{"pid=1 module=WebView2Loader.dll"},
		AppExtensions:       []string{"windows.appService name=ChatBridge"},
		DirectAssessment:    "The ChatGPT package declares a Windows AppService.",
	}
	got := p.summary()
	for _, want := range []string{"ChatGPT desktop processes: 3", "9222", "openai-chat-ipc", "Codex pipes ignored", "Static ChatGPT app resource files scanned: 2", "/backend-api/conversation", "Runtime architecture", "WebView2", "windows.appService", "Direct-backend assessment", "background transport candidate"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"cookie:", "bearer ", "local storage", "access token"} {
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

func TestChatGPTStaticProtocolMarkersAreUsefulAndScrubbed(t *testing.T) {
	data := []byte(`
const endpoint = "https://chatgpt.com/backend-api/conversation?token=super-secret";
const socket = "wss://chatgpt.com/ws?access_token=also-secret";
const relative = "/backend-api/conversation";
const id = "conversationId";
const auth = "Authorization: Bearer do-not-return";
`)
	got := strings.Join(extractChatGPTProtocolMarkers(data), "\n")
	for _, want := range []string{"https://chatgpt.com/backend-api/conversation", "wss://chatgpt.com/ws", "/backend-api/conversation", "marker: conversationId"} {
		if !strings.Contains(got, want) {
			t.Fatalf("static protocol scan missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"super-secret", "also-secret", "bearer do-not-return", "access_token="} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(forbidden)) {
			t.Fatalf("static protocol scan leaked secret material %q: %s", forbidden, got)
		}
	}
}

func TestChatGPTStaticMarkersDoNotClaimConnection(t *testing.T) {
	p := chatGPTDirectProbeResult{
		Supported:          true,
		ProcessCount:       13,
		StaticFilesScanned: 1,
		ProtocolMarkers:    []string{"/backend-api/conversation"},
	}
	if p.provenTransport() {
		t.Fatal("static protocol strings must not count as a live ChatGPT connection")
	}
	got := p.summary()
	for _, want := range []string{"no live ChatGPT-owned local transport is exposed", "architecture survey"} {
		if !strings.Contains(got, want) {
			t.Fatalf("static-only summary missing %q: %s", want, got)
		}
	}
}

func TestChatGPTArchitectureEvidenceStillDoesNotClaimConnection(t *testing.T) {
	p := chatGPTDirectProbeResult{
		Supported:           true,
		ProcessCount:        13,
		RuntimeArchitecture: "WebView2/Edge-hosted desktop client",
		RuntimeSignals:      []string{"pid=2 module=WebView2Loader.dll"},
		ProtocolSchemes:     []string{"chatgpt://"},
		MarkerSources:       []string{"resources/app.asar -> /backend-api/conversation"},
		DirectAssessment:    "No externally callable local API is exposed.",
	}
	if p.provenTransport() {
		t.Fatal("runtime/package evidence must not count as a live ChatGPT connection")
	}
	got := p.summary()
	for _, want := range []string{"Runtime architecture", "Registered activation protocols", "App-specific protocol marker sources", "Direct-backend assessment", "no live ChatGPT-owned local transport is exposed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("architecture summary missing %q: %s", want, got)
		}
	}
}

func TestChatGPTMappedInternalIPCIsNotClaimedAsExternalAPI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the direct-path assessment is Windows-specific")
	}
	p := chatGPTDirectProbeResult{
		ASARIPCCandidates: []string{
			"BRIDGE EXPOSURE: .vite/build/preload.js -> contextBridge exposes electronBridge",
			"IPC BINDING: .vite/build/preload.js -> renderer.invoke -> chatgpt:send-conversation",
		},
		ASARMarkerSources: []string{
			"BACKEND ROUTE: .vite/build/main.js -> /backend-api/conversation",
			"REQUEST KEY: .vite/build/main.js -> messages",
			"AUTH FLOW: .vite/build/main.js -> PKCE code_challenge",
		},
	}
	got := assessChatGPTDirectPath(p)
	for _, want := range []string{"renderer-to-main IPC", "independently authenticated cloud request path", "private session credentials"} {
		if !strings.Contains(got, want) {
			t.Fatalf("mapped assessment missing %q: %s", want, got)
		}
	}
}

func TestChatGPTAgentsPaneShowsWorkingWebViewConnection(t *testing.T) {
	body := chatGPTWebUI(agentConnectFirstRunHTML(agentsPageHTML))
	for _, want := range []string{
		"ChatGPT Chat", "Connect ChatGPT", "Test", "Show session", "Answers G: messages",
		"private FlipAi browser session", "network response", "DOM", "Open Activity",
		"G: your message", "G NEW", "Windows accessibility", "Not used", "Disconnect",
	} {
		if !strings.Contains(body, want) { t.Fatalf("ChatGPT WebView pane missing %q", want) }
	}
	for _, forbidden := range []string{"Unavailable until proven", "Map Chat request protocol", "full read-only protocol map"} {
		if strings.Contains(body, forbidden) { t.Fatalf("retired ChatGPT diagnostic UI still contains %q", forbidden) }
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
