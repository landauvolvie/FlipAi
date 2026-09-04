# FlipAi v0.46.33

Direct Google Voice SMS connection release.

## Google Voice SMS

- Added **Google Voice SMS** as a second option under Connections alongside Gmail.
- Direct SMS uses FlipAi's existing signed-in Google Voice WebView, so Gmail forwarding is not required when this transport is selected.
- Incoming texts use the same allowlist, security-code, sticky-agent, queue, STATUS, NEW, acknowledgement, and progress paths as the existing SMS bridge.
- Replies are sent back through the Google Voice page itself.
- The direct transport works with Codex, Claude Code, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat through the existing agent routing.
- Gmail remains available as the alternate SMS transport; only one reader is selected at a time to prevent duplicate replies.

## Calling isolation

- Existing Google Voice calling settings and call-routing behavior are not changed by this release.
- The SMS observer and sender are separate from the call state machine even though they reuse the same signed-in Google Voice browser profile.

No Authenticode/code-signing certificate is included in this release.
