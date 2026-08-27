//go:build windows

package main

// desktopInitScript runs in the FlipAi window before each local page loads.
//
// It used to rebuild the whole settings page in the browser: injecting a
// stylesheet, moving nodes around, and hiding sections to fake a multi-page
// app. The pages are now served that way by the local host itself, so the base
// script only marks the trusted desktop window and blocks browser navigation.
// The voice-call feature appends its optional controls without changing the
// server-rendered SMS UI.
const baseDesktopInitScript = `
(() => {
  globalThis.__flipaiDesktop = true;
  const markDesktop = () => {
    if (document.documentElement) document.documentElement.dataset.flipaiDesktop = "1";
  };
  markDesktop();
  if (!document.documentElement) {
    addEventListener("DOMContentLoaded", markDesktop, {once:true});
  }

  addEventListener("keydown", (e) => {
    if (e.key === "Backspace" && !/^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName) && !e.target.isContentEditable) {
      e.preventDefault();
    }
  });
})();
`

const voiceVisibilityFallbackScript = `
(() => {
  const showVoiceFailure = () => {
    if (!globalThis.__flipaiDesktop) return;
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

const desktopInitScript = baseDesktopInitScript + voiceDesktopInitScript + voiceAudioDesktopScript + voiceVisibilityFallbackScript
