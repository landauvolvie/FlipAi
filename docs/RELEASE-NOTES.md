# FlipAi v0.46.13

This release makes the dedicated ChatGPT Chat connection persistent and always-on after the one-time sign-in.

## Connect once, then stay connected

- A successful ChatGPT sign-in is stored as a durable connection state separate from the currently loading page state.
- Existing v0.46.12 signed-in profiles migrate automatically.
- Closing the one-time ChatGPT sign-in window is safe; FlipAi restores the same profile off-screen.
- The FlipAi tray automatically restores a connected ChatGPT WebView after the sign-in window closes, after FlipAi restarts, and after Windows restarts.
- Closing the normal FlipAi control window does not stop the ChatGPT background browser. Explicit Disconnect still removes only FlipAi's dedicated ChatGPT profile.

## Readiness race fixed

v0.46.12 treated “the WebView control endpoint exists” as “ChatGPT is signed in and ready.” A fresh hidden WebView exposes its control endpoint before chatgpt.com finishes restoring the persisted account session. That is why Activity could show a failed turn followed by a successful sign-in verification in the same second.

v0.46.13 waits for the current ChatGPT page to verify the saved session before a test or chat turn is sent. New-chat navigation also waits for the restored session before using the composer.

## Better recovery and diagnostics

- Background worker launch state prevents intentional duplicate starts when the tray supervisor and a turn notice a missing worker together.
- Activity records saved-session restore, background startup, readiness, and failures with timing.
- ChatGPT `Runtime.evaluate` failures now identify the ChatGPT WebView instead of using the old Google Voice wording inherited from a shared helper.
- Agents separates **Saved connection**, **Live sign-in**, and **Browser session** so a normal restore delay is visible without looking disconnected.

## Privacy and desktop behavior

The integration still uses FlipAi's own persistent WebView2 profile. It does not copy cookies or tokens out of that profile, does not use Windows accessibility, SendKeys, global mouse/keyboard input, coordinates, or the visible ChatGPT desktop app, and does not open a remote DevTools port.

## Testing

The suite covers v0.46.12 profile migration, durable connection state across temporary page loading, tray-only automatic background startup, ChatGPT-specific control errors, the exact ChatGPT page driver in real Chromium, Windows race tests, installer lifecycle, Google Voice regressions, and the existing full Linux/Windows suites.

## What to test

Install v0.46.13 over v0.46.12. If ChatGPT already shows **Connected**, close the ChatGPT sign-in window and send another test/chat message. Then restart FlipAi or Windows and try again without pressing **Connect ChatGPT**. Activity should show the saved session restoring invisibly and then becoming ready.
