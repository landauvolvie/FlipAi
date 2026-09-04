# FlipAi v0.46.34

Direct Google Voice SMS reliability fix.

## Google Voice SMS

- Fixed direct Google Voice SMS showing **Connected** while new texts never reached Activity or an agent.
- Direct SMS now uses its own hidden Google Voice **Messages** WebView while reusing the same signed-in Google Voice profile. The calling view no longer has to be on the Messages screen for texts to arrive.
- The listener can read the sender number from Google Voice row metadata when the visible conversation shows a saved contact name instead of the number.
- Outbound direct replies use the dedicated Messages view as well, so sending a reply does not navigate or interfere with the Google Voice calling view.
- Added a live listener health probe. Connections now shows **Connected** only when the dedicated Messages listener is actually running and verified.
- Gmail no longer falsely shows Connected when Direct Google Voice is the selected SMS transport.
- Added a real Chromium regression test covering a saved-contact conversation receiving `X: hi` and confirming outgoing `You:` rows are ignored.

## Calling isolation

- Existing Google Voice calling settings, call state machine, answering logic, and audio routing remain unchanged.

All existing SMS routing remains unchanged: Codex, Claude Code, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat continue through the same allowlist, security-code, sticky-agent, queue, STATUS, NEW, acknowledgement, and reply paths.

No Authenticode/code-signing certificate is included in this release.
