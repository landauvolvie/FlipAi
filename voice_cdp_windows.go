//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
)

// webViewDevTools is the DevTools protocol reached through WebView2 itself.
//
// Every WebView2 call has to be made on the thread that created it, and that
// thread is inside the window's message loop, so each call is dispatched there
// and its reply is waited for here. It must therefore never be called *from*
// that thread -- from inside a page binding, for instance -- or it would wait
// for a loop that is waiting for it.
type webViewDevTools struct {
	view     webview2.WebView
	chromium *edge.Chromium
}

const (
	// voiceDevToolsTimeout bounds ordinary DevTools calls. Google Voice probes
	// must fail quickly if its page is navigating or wedged so the call observer
	// does not disappear for a long time.
	voiceDevToolsTimeout = 8 * time.Second

	// A ChatGPT turn is intentionally one awaited Runtime.evaluate promise. The
	// JavaScript itself may wait up to 90 seconds for the model to finish, so the
	// generic 8-second Google Voice deadline would falsely report failure while
	// ChatGPT continued answering in the page.
	chatGPTTurnDevToolsTimeout = 95 * time.Second
)

func newWebViewDevTools(view webview2.WebView) *webViewDevTools {
	chromium := voiceChromium(view)
	if view == nil || chromium == nil {
		return nil
	}
	return &webViewDevTools{view: view, chromium: chromium}
}

// webViewDevToolsCallTimeout keeps the short Google Voice timeout as the
// default, but recognizes FlipAi's long-running ChatGPT turn driver and gives
// only that awaited expression enough time to finish. This channel is shared by
// both private WebViews, so a single global timeout is not correct for both.
func webViewDevToolsCallTimeout(method string, params any) time.Duration {
	if method != "Runtime.evaluate" {
		return voiceDevToolsTimeout
	}
	m, ok := params.(map[string]any)
	if !ok {
		return voiceDevToolsTimeout
	}
	await, _ := m["awaitPromise"].(bool)
	expression, _ := m["expression"].(string)
	if await &&
		strings.Contains(expression, "const deadline=Date.now()+90000;") &&
		(strings.Contains(expression, `data-message-author-role="assistant"`) ||
			strings.Contains(expression, "model-response") ||
			strings.Contains(expression, "grokResponse")) {
		return chatGPTTurnDevToolsTimeout
	}
	return voiceDevToolsTimeout
}

func (d *webViewDevTools) Call(method string, params any, out any) error {
	if d == nil || d.view == nil || d.chromium == nil {
		return errNoVoiceControlChannel
	}
	body := "{}"
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		body = string(b)
	}

	type reply struct {
		code   uintptr
		result string
		err    error
	}
	// Buffered so a reply arriving after the timeout below does not block the
	// window's message loop forever.
	answered := make(chan reply, 1)
	d.view.Dispatch(func() {
		err := d.chromium.CallDevToolsProtocolMethod(method, body, func(code uintptr, result string) {
			select {
			case answered <- reply{code: code, result: result}:
			default:
			}
		})
		if err != nil {
			select {
			case answered <- reply{err: err}:
			default:
			}
		}
	})

	timeout := webViewDevToolsCallTimeout(method, params)
	select {
	case got := <-answered:
		if got.err != nil {
			return got.err
		}
		if got.code != 0 {
			return fmt.Errorf("%s failed in the WebView page (0x%X)", method, got.code)
		}
		if out == nil || got.result == "" {
			return nil
		}
		return json.Unmarshal([]byte(got.result), out)
	case <-time.After(timeout):
		return errors.New("the WebView page did not answer " + method)
	}
}

// webViewVoicePermissions is the in-process half of the permission grant: the
// WebView2 host answers the page's permission prompts itself, so a call never
// waits behind a dialog in a window nobody is looking at.
func webViewVoicePermissions(chromium *edge.Chromium) {
	if chromium == nil {
		return
	}
	chromium.SetPermission(edge.CoreWebView2PermissionKindMicrophone, edge.CoreWebView2PermissionStateAllow)
	chromium.SetPermission(edge.CoreWebView2PermissionKindNotifications, edge.CoreWebView2PermissionStateAllow)
	chromium.SetPermission(edge.CoreWebView2PermissionKindCamera, edge.CoreWebView2PermissionStateDeny)
	chromium.SetPermission(edge.CoreWebView2PermissionKindGeolocation, edge.CoreWebView2PermissionStateDeny)
	chromium.SetPermission(edge.CoreWebView2PermissionKindOtherSensors, edge.CoreWebView2PermissionStateDeny)
	chromium.SetPermission(edge.CoreWebView2PermissionKindClipboardRead, edge.CoreWebView2PermissionStateDeny)
}
