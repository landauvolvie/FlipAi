//go:build windows

package main

// desktopInitScript runs in the FlipAi window before each local page loads.
//
// It used to rebuild the whole settings page in the browser: injecting a
// stylesheet, moving nodes around, and hiding sections to fake a multi-page
// app. The pages are now served that way by the local host itself, so all this
// has left to do is tell the page it is running inside the desktop window
// rather than a browser tab, and keep the window from offering browser-style
// navigation gestures the frameless window cannot undo.
const desktopInitScript = `
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
