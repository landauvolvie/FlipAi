package edge

// The completed handler for ICoreWebView2::CallDevToolsProtocolMethod.
//
// This is a local addition for FlipAi. CallDevToolsProtocolMethod is the
// supported, in-process way to reach the DevTools protocol from a WebView2
// host: no TCP port is opened, nothing is listening on the machine, and the
// call is made against this WebView2 and no other. FlipAi uses it to deliver a
// real pointer press to a ringing Google Voice call and to attach an image to
// an outgoing message -- two things a page script cannot do for itself.

type _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type iCoreWebView2CallDevToolsProtocolMethodCompletedHandler struct {
	vtbl *_ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerVtbl
	impl _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerImpl
}

func _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerIUnknownQueryInterface(this *iCoreWebView2CallDevToolsProtocolMethodCompletedHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}

func _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerIUnknownAddRef(this *iCoreWebView2CallDevToolsProtocolMethodCompletedHandler) uintptr {
	return this.impl.AddRef()
}

func _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerIUnknownRelease(this *iCoreWebView2CallDevToolsProtocolMethodCompletedHandler) uintptr {
	return this.impl.Release()
}

func _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerInvoke(this *iCoreWebView2CallDevToolsProtocolMethodCompletedHandler, errorCode uintptr, returnObjectAsJson *uint16) uintptr {
	return this.impl.CallDevToolsProtocolMethodCompleted(errorCode, returnObjectAsJson)
}

type _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerImpl interface {
	_IUnknownImpl
	CallDevToolsProtocolMethodCompleted(errorCode uintptr, returnObjectAsJson *uint16) uintptr
}

var _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerFn = _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(_ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerIUnknownQueryInterface),
		NewComProc(_ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerIUnknownAddRef),
		NewComProc(_ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerIUnknownRelease),
	},
	NewComProc(_ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerInvoke),
}

func newICoreWebView2CallDevToolsProtocolMethodCompletedHandler(impl _ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerImpl) *iCoreWebView2CallDevToolsProtocolMethodCompletedHandler {
	return &iCoreWebView2CallDevToolsProtocolMethodCompletedHandler{
		vtbl: &_ICoreWebView2CallDevToolsProtocolMethodCompletedHandlerFn,
		impl: impl,
	}
}
