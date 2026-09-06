# FlipAi v0.46.37

Google Voice SMS live-message detection fix.

## Google Voice SMS detection

- Fixed Direct Google Voice SMS showing **Connected / Ready** while detecting zero live conversation rows and therefore producing no Activity entries.
- The SMS listener now handles Google Voice's linkless SPA/custom conversation rows instead of depending on an `<a href=".../messages">` element being present.
- When a changed linkless row does not expose the sender directly, FlipAi opens that row only inside its dedicated hidden SMS browser and resolves the trusted Google Voice `itemId` conversation identity.
- Current Google Voice `/messages?itemId=t.%2B1XXXXXXXXXX` conversation locators are supported end-to-end for inbound detection and exact-thread replies.
- The Connections card now gates **Connected / Ready** on a live detector heartbeat that can actually see conversation rows (or a verified empty inbox), not merely on the Messages page having loaded.

## Sender and reply safety

- Sender authorization remains based only on the normalized phone number from trusted Google Voice identity metadata / `itemId`; saved contact names never authorize a sender.
- Phone numbers written inside an SMS body cannot be used as sender identity. The real-browser regression test includes a decoy phone number in the message body.
- Unauthorized, unresolved, calls-only, or mismatched sender/thread identities remain blocked before reaching any AI agent, with Activity logging and no reply sent.
- Replies remain fail-closed to the exact stored Google Voice thread plus the same phone number. No contact-name or ambiguous single-result fallback is used.

## Regression coverage

- Added a real Chromium regression case matching the reported live failure: saved contact name, linkless Google Voice conversation row, no visible sender number, and a decoy phone number inside the SMS body.
- The test verifies the changed row resolves to the correct Google Voice `itemId`, captures the inbound message exactly once, and ignores an outgoing `You:` update.

## Calling isolation

- Existing Google Voice calling behavior, profile, settings, and call state machine are unchanged.

No Authenticode/code-signing certificate is included in this release.
