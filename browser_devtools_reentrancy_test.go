package main

import (
	"os"
	"strings"
	"testing"
)

// readGoSource reads a source file with its line endings normalized. The
// Windows CI runner checks the tree out with CRLF, so a test that matches a
// multi-line pattern with "\n" finds nothing there and fails a release that is
// perfectly fine.
func readGoSource(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// devToolsFunctionBody returns the source of one function in voice_cdp_windows.go.
func devToolsFunctionBody(t *testing.T, signature string) string {
	t.Helper()
	src := readGoSource(t, "voice_cdp_windows.go")
	start := strings.Index(src, signature)
	if start < 0 {
		t.Fatalf("%s not found", signature)
	}
	rest := src[start+len(signature):]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("%s is not terminated", signature)
	}
	return rest[:end]
}

// v0.46.81 serialized DevTools calls to stop a stale page sampler colliding
// with the next turn's driver -- and took the lock across the whole of Call.
// Call issues further DevTools calls around the one it is making: the
// attachment upload before a turn and the returned-media scan after it. A
// sync.Mutex is not reentrant, so the very first browser turn re-entered the
// lock it already held and wedged that WebView worker for good: the model had
// the prompt and answered it, and the control channel never came back.
//
// The lock belongs to the single protocol call, never to Call's body.
func TestDevToolsLockIsNeverHeldAcrossANestedCall(t *testing.T) {
	call := devToolsFunctionBody(t, "func (d *webViewDevTools) Call(method string, params any, out any) error {")
	if strings.Contains(call, "d.call.Lock()") {
		t.Fatal("Call takes the DevTools lock around its own body, which re-enters it through the nested calls below and deadlocks the WebView")
	}
	// These are the nested DevTools calls Call makes. They must run unlocked.
	for _, nested := range []string{"uploadBrowserChatImages", "captureBrowserChatReturnedMediaAfterTurn"} {
		if !strings.Contains(call, nested) {
			t.Fatalf("Call no longer calls %s; this guard needs updating", nested)
		}
	}

	dispatch := devToolsFunctionBody(t, "func (d *webViewDevTools) dispatch(method, body string, timeout time.Duration) (devToolsReply, bool) {")
	if !strings.Contains(dispatch, "d.call.TryLock()") || !strings.Contains(dispatch, "d.call.Unlock()") {
		t.Fatal("the single protocol call is no longer serialized, so a page sampler can collide with a turn again")
	}
	for _, nested := range []string{
		"uploadBrowserChatImages",
		"captureBrowserChatReturnedMedia",
		"continueBrowserLongTurn",
		"voiceEval",
		"d.Call(",
	} {
		if strings.Contains(dispatch, nested) {
			t.Fatalf("dispatch holds the lock across %s, which calls back into it", nested)
		}
	}
}
