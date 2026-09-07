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

	// The provider page drivers still use a 90-second first-pass checkpoint so
	// the private worker HTTP request can return before its legacy transport
	// deadline. A timed-out *model turn* is no longer a failure: Call starts the
	// indefinite browserLongTurn continuation before returning that checkpoint
	// to the SMS layer, which then waits on the persisted continuation state.
	chatGPTTurnDevToolsTimeout = 95 * time.Second

	// The generated-media collector is itself an awaited page expression and is
	// deliberately allowed to outlive the ordinary 90-second text detector.
	browserChatReturnedMediaDevToolsTimeout = browserChatGeneratedImageWait + 15*time.Second

	// A Google Voice UI send waits for the real composer to clear, an outgoing
	// bubble to appear, or a page error to surface. That confirmation window is
	// intentionally longer than an ordinary probe but much shorter than a model
	// turn.
	googleVoiceSMSUITurnDevToolsTimeout = 15 * time.Second

	// MMS capture reads the actual media bytes from the signed-in Google Voice
	// page. A phone photo or voice note can need more than the generic 8-second
	// probe window to come out of Google's cache, so only this marked expression
	// gets a longer deadline.
	googleVoiceMediaCaptureDevToolsTimeout = 30 * time.Second
)

func newWebViewDevTools(view webview2.WebView) *webViewDevTools {
	chromium := voiceChromium(view)
	if view == nil || chromium == nil {
		return nil
	}
	return &webViewDevTools{view: view, chromium: chromium}
}

// webViewDevToolsCallTimeout keeps the short Google Voice timeout as the
// default, but recognizes long-running page turns and gives only those awaited
// expressions enough time to reach their first checkpoint. Long model work is
// continued separately after that checkpoint; ordinary page probes stay short.
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
	if await && strings.Contains(expression, browserChatReturnedMediaMarker) {
		return browserChatReturnedMediaDevToolsTimeout
	}
	if await && isBrowserChatTurnExpression(expression) {
		return chatGPTTurnDevToolsTimeout
	}
	if await && strings.Contains(expression, googleVoiceSMSUITurnMarker) {
		return googleVoiceSMSUITurnDevToolsTimeout
	}
	if await && strings.Contains(expression, googleVoiceMediaCaptureMarker) {
		return googleVoiceMediaCaptureDevToolsTimeout
	}
	// A Google Voice web-service request runs in the page and can take as long
	// as any network call. The generic probe deadline is far shorter, and a
	// fetch the host stops waiting for is not cancelled -- it can still deliver
	// the text while FlipAi reports the reply as failed.
	if await && strings.Contains(expression, googleVoiceSMSPageRequestMarker) {
		return googleVoiceSMSPageRequestDeadline
	}
	return voiceDevToolsTimeout
}

func (d *webViewDevTools) Call(method string, params any, out any) error {
	if d == nil || d.view == nil || d.chromium == nil {
		return errNoVoiceControlChannel
	}

	browserTurn := false
	browserProvider := ""
	generatedImageTurn := false
	// Browser-chat media turns carry a private marker inside the prompt sent to
	// the worker. Strip it before the page sees the prompt, upload those local
	// temp files through the site's own file input, then run the normal provider
	// turn unchanged. The nested DevTools calls contain no marker, so they do not
	// recurse into this branch.
	if method == "Runtime.evaluate" {
		if m, ok := params.(map[string]any); ok {
			if expression, ok := m["expression"].(string); ok {
				browserTurn = isBrowserChatTurnExpression(expression)
				if browserTurn {
					clearCapturedBrowserChatReturnedMedia()
					generatedImageTurn = browserChatPromptRequestsGeneratedImage(expression)
					browserProvider = beginBrowserLongTurn(expression)
				}
				clean, attachments, found, err := extractBrowserChatAttachmentMarker(expression)
				if err != nil {
					return err
				}
				if found {
					if err := uploadBrowserChatImages(d, attachments); err != nil {
						return err
					}
					copyParams := make(map[string]any, len(m))
					for k, v := range m {
						copyParams[k] = v
					}
					copyParams["expression"] = clean
					params = copyParams
				}
			}
		}
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
		if out != nil && got.result != "" {
			if err := json.Unmarshal([]byte(got.result), out); err != nil {
				return err
			}
		}
		if browserTurn {
			// The page drivers' 90-second result is a checkpoint, not a failure.
			// If the provider visibly says it is still working, continue sampling
			// that same page without resending the prompt. The main FlipAi process
			// waits on the persisted state and can keep texting safe visible status
			// updates for as long as the provider remains active.
			if value, ok := browserTurnValueFromDevTools(got.result); ok && !value.OK && browserLongTurnTimeoutDetail(value.Detail) && browserProvider != "" {
				started := strings.Contains(strings.ToLower(value.Detail), "started answering") || strings.Contains(strings.ToLower(value.Detail), "started responding")
				go continueBrowserLongTurn(d, browserProvider, started)
			}

			// Image creation frequently continues after the provider's text DOM
			// has stabilized or after its first checkpoint. Keep the existing media
			// collector in parallel with the long-turn continuation so the actual
			// image can still be handed to the Google Voice MMS path.
			if generatedImageTurn {
				go captureBrowserChatReturnedMediaAfterTurnWithWait(d, browserChatGeneratedImageWait)
			} else {
				captureBrowserChatReturnedMediaAfterTurn(d)
			}
		}
		return nil
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
