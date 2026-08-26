from pathlib import Path
import re
import textwrap

# Patch the vendored WebView2 binding. Its PermissionRequested callback passes
# GetPermissionKind's output value instead of the address of the output value,
# so every site permission request is read as kind 0.
edge = Path("third_party/go-webview2/pkg/edge/chromium.go")
text = edge.read_text(encoding="utf-8")
old = "\t\tuintptr(kind),\n"
new = "\t\tuintptr(unsafe.Pointer(&kind)),\n"
if text.count(old) != 1:
    raise SystemExit(f"expected one broken GetPermissionKind pointer call, found {text.count(old)}")
edge.write_text(text.replace(old, new, 1), encoding="utf-8")

# Make the patched dependency part of FlipAi itself so every build uses the
# permission fix instead of relying on the module cache.
mod = Path("go.mod")
text = mod.read_text(encoding="utf-8")
replacement = "replace github.com/jchv/go-webview2 => ./third_party/go-webview2"
if replacement not in text:
    text = text.rstrip() + "\n\n" + replacement + "\n"
mod.write_text(text, encoding="utf-8")

win = Path("voice_call_windows.go")
text = win.read_text(encoding="utf-8")
old_args = '''const voiceBrowserArguments = "--disable-background-timer-throttling " +
\t"--disable-backgrounding-occluded-windows " +
\t"--disable-renderer-backgrounding " +
\t"--disable-features=CalculateNativeWinOcclusion,IntensiveWakeUpThrottling " +
\t"--autoplay-policy=no-user-gesture-required"'''
new_args = '''const voiceBrowserArguments = "--disable-background-timer-throttling " +
\t"--disable-backgrounding-occluded-windows " +
\t"--disable-renderer-backgrounding " +
\t"--disable-features=CalculateNativeWinOcclusion,IntensiveWakeUpThrottling " +
\t"--autoplay-policy=no-user-gesture-required " +
\t"--disable-popup-blocking"'''
if old_args not in text:
    raise SystemExit("expected voiceBrowserArguments block was not found")
text = text.replace(old_args, new_args, 1)

old_plain = '''\tplain := googleVoiceRenderMode{
\t\tname: "plain",
\t\tnote: "Google Voice started without FlipAi's background-timer switches, because WebView2 refused them. A call should still be answered; if one is ever missed while FlipAi is in the background, this is the first thing to look at.",
\t\targs: "",
\t}'''
new_plain = '''\tplain := googleVoiceRenderMode{
\t\tname: "plain",
\t\tnote: "Google Voice started without FlipAi's background-timer switches, because WebView2 refused them. Call pop-ups remain enabled. If one is ever missed while FlipAi is in the background, this render mode is the first thing to look at.",
\t\targs: "--disable-popup-blocking",
\t}'''
if old_plain not in text:
    raise SystemExit("expected plain Google Voice render mode was not found")
text = text.replace(old_plain, new_plain, 1)

permission_pattern = re.compile(
    r'\t// Per-kind permissions do not work with this WebView2 binding:.*?'
    r'\n\tif chromium := voiceChromium\(w\); chromium != nil \{.*?'
    r'\n\t\}\n\n\t// The agents',
    re.S,
)
permission_replacement = '''\t// Google Voice needs microphone and notification permission before it will
\t// behave as a browser that can place and receive calls. FlipAi carries a
\t// one-line local fix for the WebView2 binding's permission-kind callback,
\t// so these can now be granted explicitly instead of using a global allow.
\t// The embedded window therefore never hides a permission prompt the user
\t// cannot reach, while unrelated capabilities stay denied.
\tif chromium := voiceChromium(w); chromium != nil {
\t\tchromium.SetPermission(edge.CoreWebView2PermissionKindMicrophone, edge.CoreWebView2PermissionStateAllow)
\t\tchromium.SetPermission(edge.CoreWebView2PermissionKindNotifications, edge.CoreWebView2PermissionStateAllow)
\t\tchromium.SetPermission(edge.CoreWebView2PermissionKindCamera, edge.CoreWebView2PermissionStateDeny)
\t\tchromium.SetPermission(edge.CoreWebView2PermissionKindGeolocation, edge.CoreWebView2PermissionStateDeny)
\t\tchromium.SetPermission(edge.CoreWebView2PermissionKindOtherSensors, edge.CoreWebView2PermissionStateDeny)
\t\tchromium.SetPermission(edge.CoreWebView2PermissionKindClipboardRead, edge.CoreWebView2PermissionStateDeny)
\t} else {
\t\tmutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
\t\t\ts.LastError = "FlipAi could not configure Google Voice microphone and notification permissions in WebView2. Restart the Google Voice window before testing calls."
\t\t})
\t}

\t// The agents'''
text, count = permission_pattern.subn(permission_replacement, text, count=1)
if count != 1:
    raise SystemExit(f"expected one old permission block, replaced {count}")
win.write_text(text, encoding="utf-8")

page = Path("voice_page_script.go")
text = page.read_text(encoding="utf-8")
old_comment = "// WebView2 grants this window's permissions globally, because per-permission\n  // grants are broken in the browser binding FlipAi uses. Everything except\n  // what a phone call needs is therefore taken away here, before Google Voice\n  // can ask."
new_comment = "// FlipAi grants only the microphone and notification permissions a phone\n  // call needs at the WebView2 host. Keep defense-in-depth denials here for\n  // browser capabilities Google Voice does not need."
if old_comment not in text:
    raise SystemExit("expected old Google Voice permissions comment was not found")
text = text.replace(old_comment, new_comment, 1)
old_click = '''  document.addEventListener('click', (e) => {
    const a = e.target && e.target.closest ? e.target.closest('a[href]') : null;
    if (a && !allowedTopLevel(a.href)) e.preventDefault();
  }, true);'''
new_click = '''  document.addEventListener('click', (e) => {
    const a = e.target && e.target.closest ? e.target.closest('a[href]') : null;
    if (!a) return;
    const raw = (a.getAttribute('href') || '').trim().toLowerCase();
    // An outgoing Google Voice dial can be dispatched through a phone URI.
    // The old navigation guard cancelled it before Google's handler saw it.
    if (raw.startsWith('tel:') || raw.startsWith('callto:')) return;
    if (!allowedTopLevel(a.href)) e.preventDefault();
  }, true);'''
if old_click not in text:
    raise SystemExit("expected top-level click guard was not found")
page.write_text(text.replace(old_click, new_click, 1), encoding="utf-8")

ui = Path("voice_ui_script.go")
text = ui.read_text(encoding="utf-8")
old_pill = "pill('Handled by FlipAi','ok')"
if old_pill not in text:
    raise SystemExit("expected browser-permission status pill was not found")
ui.write_text(text.replace(old_pill, "pill('Mic + notifications allowed','ok')", 1), encoding="utf-8")

# Keep every release-facing version source in lockstep.
config = Path("config.go")
text = config.read_text(encoding="utf-8")
if 'const version = "0.26.0"' not in text:
    raise SystemExit("expected 0.26.0 compiled version was not found")
config.write_text(text.replace('const version = "0.26.0"', 'const version = "0.26.1"', 1), encoding="utf-8")

installer = Path("installer/FlipAi.iss")
text = installer.read_text(encoding="utf-8")
if '#define MyVersion "0.13.0"' in text:
    text = text.replace('#define MyVersion "0.13.0"', '#define MyVersion "0.26.1"', 1)
installer.write_text(text, encoding="utf-8")
Path("VERSION").write_text("0.26.1\n", encoding="utf-8")

# Regression tests: keep popup/dialing support and the permission-kind pointer
# repair from silently regressing.
test = textwrap.dedent('''\
//go:build windows

package main

import (
\t"os"
\t"strings"
\t"testing"
)

func TestGoogleVoiceWebViewAllowsRequiredCallCapabilities(t *testing.T) {
\tif !strings.Contains(voiceBrowserArguments, "--disable-popup-blocking") {
\t\tt.Fatal("Google Voice WebView must allow call-related popups")
\t}
\tfor _, protocol := range []string{"tel:", "callto:"} {
\t\tif !strings.Contains(googleVoiceInitScript, protocol) {
\t\t\tt.Fatalf("Google Voice script must preserve %s dialing links", protocol)
\t\t}
\t}
}

func TestWebView2PermissionKindPatchIsPresent(t *testing.T) {
\tb, err := os.ReadFile("third_party/go-webview2/pkg/edge/chromium.go")
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif !strings.Contains(string(b), "uintptr(unsafe.Pointer(&kind))") {
\t\tt.Fatal("WebView2 permission-kind callback must pass GetPermissionKind an output pointer")
\t}
}
''')
Path("voice_permissions_windows_test.go").write_text(test, encoding="utf-8")
