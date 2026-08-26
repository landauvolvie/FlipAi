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
//
// The id below is the one the card actually gets, and a test holds the two
// together: when the card was renamed, this selector went on looking for a card
// that no longer existed, so every working Connections page waited four seconds
// and then announced that the voice service was not responding.
const voiceVisibilityFallbackScript = `
(() => {
  const showVoiceFailure = () => {
    if (!globalThis.__flipaiDesktop) return;
    // The controls live on Settings, the live preview on Connections; either
    // page failing to grow its card means the local voice service is down.
    const wanted = {'/connections': '#voice-preview-card', '/settings': '#voice-call-card'}[location.pathname];
    if (!wanted) return;
    setTimeout(() => {
      if (document.querySelector(wanted)) return;
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
