# FlipAi v0.46.40

Background-only direct Google Voice SMS detection.

## Google Voice SMS

- Direct Google Voice SMS no longer opens, clicks, or selects a conversation when a text arrives.
- The visible Google Voice SMS window remains a first-time sign-in/setup surface only. After setup, FlipAi keeps the dedicated Google Voice SMS profile alive in its hidden background worker, including after FlipAi restarts.
- The background listener stays on the Google Voice Messages list and resolves the exact sender/thread from Google Voice background transport data (fetch/XHR/WebSocket/EventSource) or trusted conversation-list item metadata.
- Saved contact names such as **Me** are display-only and are never used as sender identity.

## Safety and permissions

- Sender authorization continues to use the normalized phone number only.
- A phone number written inside an SMS body cannot become the sender identity.
- FlipAi requires an exact Google Voice `t.+1XXXXXXXXXX` sender/thread identity before routing the text to an agent; unresolved or mismatched messages fail closed and are logged as blocked.
- Replies remain tied to the captured exact phone/thread identity; contact-name lookup is not used.

## Regression coverage

- Added a real Chromium regression that keeps the browser on `/messages`, never opens a conversation, and never marks the row selected.
- The test delivers the sender/thread in a simulated Google Voice background response, then changes the saved-contact conversation preview.
- It verifies zero row clicks, zero conversation selection, unchanged browser URL, exact sender/thread recovery, correct handling of an unrelated conversation, rejection of a decoy phone number inside the SMS body, and no false inbound event for an outgoing `You:` preview.

## Calling isolation

- Google Voice calling behavior, profile, settings, and call state machine are unchanged.

No Authenticode/code-signing certificate is included in this release.
