//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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

	// call serializes DevTools calls on this WebView. WebView2 rejects a
	// protocol call issued while another is still outstanding, with
	// "Overlapped I/O operation is in progress" -- which is what a long-turn
	// continuation still sampling the page did to the next turn's page driver,
	// breaking a turn that had nothing wrong with it.
	call sync.Mutex
}

const (
	// devToolsOutstandingCap bounds how long a call that outlived its own
	// deadline may keep the protocol channel reserved. WebView2 has no way to
	// cancel an outstanding call, so the channel is held until its completion
	// callback fires; this stops a callback that never fires from reserving the
	// channel for the life of the process.
	devToolsOutstandingCap = 3 * time.Minute

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

	// Returned-media scans are individually short. Generated image turns repeat
	// these scans for as long as the provider remains active, so this is only a
	// per-scan safety bound and never a total model/image deadline.
	browserChatReturnedMediaDevToolsTimeout = 20 * time.Second

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

// devToolsReply is one completed protocol call.
type devToolsReply struct {
	code   uintptr
	result string
	err    error
}

// dispatch performs exactly one protocol call and waits for its answer.
//
// The lock is held for this window and nothing else. Call itself issues further
// DevTools calls around this one -- the attachment upload before a turn, the
// returned-media scan after it -- and a sync.Mutex is not reentrant, so holding
// it across Call's body made the very first browser turn re-enter the lock it
// already owned. That wedged the WebView worker permanently: the page had run
// the prompt and the model had answered, but the control channel never came
// back and every later call on that browser blocked behind it forever.
func (d *webViewDevTools) dispatch(method, body string, timeout time.Duration) (devToolsReply, bool) {
	// Waiting for the channel counts against this call's own deadline. A short
	// page probe issued while a 90-second turn holds the channel reports "did
	// not answer" on its own schedule rather than parking a goroutine until the
	// turn ends -- which, at one probe every 250ms, is how a readiness poll
	// would pile hundreds of them up behind a single turn.
	deadline := time.Now().Add(timeout)
	for !d.call.TryLock() {
		if time.Now().After(deadline) {
			return devToolsReply{}, false
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Buffered so a reply arriving after the timeout does not block the window's
	// message loop forever.
	answered := make(chan devToolsReply, 1)
	d.view.Dispatch(func() {
		err := d.chromium.CallDevToolsProtocolMethod(method, body, func(code uintptr, result string) {
			select {
			case answered <- devToolsReply{code: code, result: result}:
			default:
			}
		})
		if err != nil {
			select {
			case answered <- devToolsReply{err: err}:
			default:
			}
		}
	})

	select {
	case got := <-answered:
		d.call.Unlock()
		return got, true
	case <-time.After(timeout):
		// We have stopped waiting, but WebView2 has not: the operation is still
		// live inside the browser, and issuing the next one on top of it is what
		// returns "Overlapped I/O operation is in progress". Keep the channel
		// held until this call really finishes, so the next caller waits for a
		// free channel instead of colliding with this one.
		go func() {
			select {
			case <-answered:
			case <-time.After(devToolsOutstandingCap):
			}
			d.call.Unlock()
		}()
		return devToolsReply{}, false
	}
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

	got, answeredInTime := d.dispatch(method, body, webViewDevToolsCallTimeout(method, params))
	switch {
	case answeredInTime:
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

			// Image creation gets the same no-hard-cap behavior. Each media scan is
			// bounded, but the collector repeats while the page still shows active
			// work or an image-generation placeholder. A normal text turn keeps the
			// fast one-shot scan.
			if generatedImageTurn {
				go captureBrowserChatReturnedMediaUntilSettled(d, browserProvider)
			} else {
				captureBrowserChatReturnedMediaAfterTurn(d)
			}
		}
		return nil
	default:
		// A browser turn whose page checkpoint does not come back in time is not
		// a dead turn: the model is frequently still answering, and the answer
		// then lands in the page with nothing watching for it. Start the same
		// continuation the checkpoint would have started, so the SMS side can
		// still collect the reply instead of waiting out its whole window on a
		// state file nobody was going to write.
		if browserTurn && browserProvider != "" {
			go continueBrowserLongTurn(d, browserProvider, true)
		}
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
