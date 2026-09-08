//go:build windows

package edge

import "unsafe"

// Close releases the WebView2 controller and the COM references that Chromium
// explicitly AddRef'd while embedding the browser. FlipAi's browser agents reuse
// a dedicated user-data directory when reconnecting; leaving these references
// alive until process teardown can keep that profile locked long enough for the
// next sign-in WebView to open as a blank white window.
//
// This must run on the WebView's owning UI thread. The webview package calls it
// from WM_DESTROY, before it ends the message loop.
func (e *Chromium) Close() {
	if e == nil {
		return
	}
	if e.controller != nil {
		_, _, _ = e.controller.vtbl.Close.Call(uintptr(unsafe.Pointer(e.controller)))
	}
	if e.webview != nil {
		_, _, _ = e.webview.vtbl.Release.Call(uintptr(unsafe.Pointer(e.webview)))
		e.webview = nil
	}
	if e.controller != nil {
		_, _, _ = e.controller.vtbl.Release.Call(uintptr(unsafe.Pointer(e.controller)))
		e.controller = nil
	}
	if e.environment != nil {
		_, _, _ = e.environment.vtbl.Release.Call(uintptr(unsafe.Pointer(e.environment)))
		e.environment = nil
	}
}
