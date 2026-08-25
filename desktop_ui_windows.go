//go:build windows

package main

// desktopInitScript runs in the FlipAi window before each local page loads.
//
// It used to rebuild the whole settings page in the browser: injecting a
// stylesheet, moving nodes around, and hiding sections to fake a multi-page
// app. The pages are now served that way by the local host itself, so the base
// script only marks the trusted desktop window and blocks browser navigation.
// The voice-call feature appends its optional controls from voice_ui_windows.go
// without changing the server-rendered SMS UI.
const baseDesktopInitScript = `
(() => {
  // WebView2 runs this script at document-created time, before the HTML parser
  // is guaranteed to have created <html>. v0.20.0 touched documentElement
  // immediately, which could throw here and prevent the entire voice UI script
  // appended below from ever running. Mark the window on globalThis first, then
  // mirror the marker onto <html> once it exists.
  globalThis.__flipaiDesktop = true;
  const markDesktop = () => {
    if (document.documentElement) document.documentElement.dataset.flipaiDesktop = "1";
  };
  markDesktop();
  if (!document.documentElement) {
    addEventListener("DOMContentLoaded", markDesktop, {once:true});
  }

  // The window has no address bar or back button, so a stray swipe or
  // backspace must not navigate away from the app shell.
  addEventListener("keydown", (e) => {
    if (e.key === "Backspace" && !/^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName) && !e.target.isContentEditable) {
      e.preventDefault();
    }
  });
})();
`

// If the optional voice-control service itself cannot answer, keep the feature
// discoverable instead of failing silently. The full controls normally appear
// well before this timer fires; this banner is only the failure state.
const voiceVisibilityFallbackScript = `
(() => {
  const showVoiceFailure = () => {
    if (!globalThis.__flipaiDesktop) return;
    if (!['/connections','/settings','/agents'].includes(location.pathname)) return;
    setTimeout(() => {
      if (document.querySelector('#voice-call-connection-card,#voice-call-settings-card,#voice-call-agent-codex,#voice-call-agent-claude')) return;
      if (document.querySelector('#voice-call-unavailable')) return;
      const content = document.querySelector('.content');
      if (!content) return;
      const banner = document.createElement('div');
      banner.id = 'voice-call-unavailable';
      banner.className = 'banner warn';
      const text = document.createElement('span');
      text.textContent = 'Google Voice calling is installed, but its local voice service is not responding. Restart FlipAi; if it remains unavailable, check Activity for the voice-service error.';
      banner.append(text);
      content.prepend(banner);
    }, 4000);
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', showVoiceFailure, {once:true});
  else showVoiceFailure();
})();
`

const desktopInitScript = baseDesktopInitScript + voiceDesktopInitScript + voiceVisibilityFallbackScript
