# FlipAi v0.46.35

Google Voice SMS sign-in and browser-isolation fix.

## Google Voice SMS

- Replaced the v0.46.34 shared-profile hidden listener that could remain stuck on **Starting** with `Runtime.evaluate` failures.
- Direct Google Voice SMS now has its **own private WebView2 profile and process**, completely separate from the Google Voice calling browser/profile.
- Pressing **Connect** opens a visible Google Voice SMS sign-in window, like the browser-chat connections. FlipAi does not show Connected until that SMS browser is signed in and the Messages page is verified ready.
- If Direct Google Voice is selected but not actually connected, Connections now shows **Retry sign-in** instead of the misleading Disconnect state.
- While sign-in is open, the card clearly shows **Sign in** / **Opening…** rather than Needs attention.
- Disconnect stops the SMS worker and removes only the private SMS browser profile. Google Voice calling remains signed in and untouched.
- The SMS listener restores in its own hidden background browser after restart once the dedicated SMS profile has been connected.
- The SMS page reports health directly back to FlipAi instead of using `Runtime.evaluate` as the liveness test.
- If FlipAi's background host is running in Windows Session 0, the Connect request is handed to the signed-in tray process so the Google Voice SMS sign-in window opens on the user's visible desktop instead of an invisible session.
- Incoming texts and outgoing replies are handled only by the dedicated SMS browser, preventing the calling browser from becoming a second reader or sender.

## Routing

All existing SMS routing remains unchanged: Codex, Claude Code, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat continue through the existing allowlist, security-code, sticky-agent, queue, STATUS, NEW, acknowledgement, progress, and reply paths.

## Calling isolation

- Existing Google Voice calling settings, call state machine, answering logic, browser profile, and audio routing are unchanged.

No Authenticode/code-signing certificate is included in this release.
