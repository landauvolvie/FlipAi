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
  document.documentElement.dataset.flipaiDesktop = "1";
  // The window has no address bar or back button, so a stray swipe or
  // backspace must not navigate away from the app shell.
  addEventListener("keydown", (e) => {
    if (e.key === "Backspace" && !/^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName) && !e.target.isContentEditable) {
      e.preventDefault();
    }
  });
})();
`

const desktopInitScript = baseDesktopInitScript + voiceDesktopInitScript
