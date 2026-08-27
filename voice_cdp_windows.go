//go:build windows

package main

import (
	"github.com/jchv/go-webview2/pkg/edge"
)

// webViewVoicePermissions is the in-process half of the same grant: the
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
