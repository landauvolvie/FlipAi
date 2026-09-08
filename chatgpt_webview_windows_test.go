//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestChatGPTWebViewUsesDedicatedProfileAndPrivateLoopback(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"DataPath: chatGPTProfilePath(dataDir)",
		`net.Listen("tcp", "127.0.0.1:0")`,
		`r.Header.Get("X-FlipAi-Token")`,
		`--chatgpt-worker`,
		`-30000, -30000`,
		`NoActivate = true`,
		`data-message-author-role="assistant"`,
		`data-testid="send-button"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("ChatGPT WebView implementation lost %q", want)
		}
	}
}

func TestChatGPTWorkModeWaitsForFreshPageUI(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"const composerReady=()=>!!document.querySelector",
		"for(let i=0;i<100&&!composerReady();i++)await sleep(200);",
		"modeResult := prepareFresh(body.Mode)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("fresh ChatGPT Work mode lost readiness guard %q", want)
		}
	}
}

func TestChatGPTWorkNewUsesNativeNewChatAndBroadModeControls(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"const chatGPTClickNewChatJS",
		`button,a,[role="button"]`,
		"aria-current",
		"prepareFresh := func(mode string) chatGPTTurnResult",
		"target.click()",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("ChatGPT Work NEW lost robust fresh-session behavior %q", want)
		}
	}
}

func TestChatGPTWebViewDoesNotUseGlobalUIAutomation(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(b))
	for _, forbidden := range []string{
		"sendkeys",
		"uiautomation",
		"setforegroundwindow",
		"input.dispatchmouseevent",
		"--remote-debugging-port",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("ChatGPT WebView must not use global/visible UI automation marker %q", forbidden)
		}
	}
}

func TestChatGPTNeverUsesCompletionStatusAsReply(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "reply:now||'ChatGPT completed the turn.'") {
		t.Fatal("ChatGPT completion status must never be substituted for an empty final reply")
	}
	if !strings.Contains(s, "if(!stop()&&stable>=5&&now)return {ok:true,reply:now") {
		t.Fatal("ChatGPT must wait for non-empty assistant text before declaring a successful reply")
	}
}

func TestChatGPTRouteBoundariesAreExplicit(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"const workEvidence=named('work');",
		"if(workEvidence.length===0)return {ok:true,mode:'chat'",
		"O: means regular ChatGPT Chat",
		"openFreshChat := func() chatGPTTurnResult",
		"if fresh := openFreshChat(); !fresh.OK",
		"if work := ensureMode(browserModeWork); !work.OK",
		"return openFreshChat()",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("ChatGPT route boundary lost %q", want)
		}
	}
	if strings.Contains(s, "if(!findSelected('work'))return {ok:true,mode:'chat'") {
		t.Fatal("Chat mode must not be accepted merely because Work lacks selected-state attributes")
	}
	if strings.Contains(s, "chatGPTEval(dev, chatGPTClickNewChatJS") {
		t.Fatal("OW NEW must not depend on ChatGPT Work exposing a New chat button")
	}
}

func TestChatGPTRichCardRenderErrorsAreNotTextedAsReplies(t *testing.T) {
	b, err := os.ReadFile("chatgpt_webview_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"Unable to display this message due to an error",
		"Reload the page to try again",
		"const text=n=>clean(",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("ChatGPT reply cleanup lost %q", want)
		}
	}
}
