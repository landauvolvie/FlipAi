# FlipAi v0.46.38

Google Voice SMS sender-resolution fix for saved-contact conversations.

## Google Voice SMS

- Fixed the case where FlipAi detected an incoming Google Voice SMS but then blocked it with **“exact sender phone number could not be resolved”** even though the sender's phone number was already allowed for the agent.
- This occurs when Google Voice shows only a saved contact name such as **US Mobile** and opening the conversation does not change the browser URL to an `itemId` URL.
- After opening the changed conversation, FlipAi now resolves the exact Google Voice `t.+1XXXXXXXXXX` identity from trusted opened-conversation metadata, browser history state, or newly observed same-origin Google Voice resource metadata.
- Conversation headers are identity sources only; they cannot be mistaken for a second incoming SMS.
- The direct SMS listener still never treats a saved contact name or a phone number written inside the SMS body as sender identity.

## Security and routing

- The normalized sender phone must still match the exact Google Voice thread identity before the message reaches an agent.
- Existing per-agent phone permissions, routing codes, security codes, sticky-agent behavior, STATUS, NEW, acknowledgements, and progress updates remain unchanged.
- Unauthorized, unresolved, calls-only, or mismatched identities remain blocked and logged in Activity without a reply.

## Regression coverage

- Added a real Chromium regression matching the reported live failure: a linkless Google Voice row labeled only with a saved contact name, an unchanged `/messages` browser URL, unrelated preloaded conversation metadata, and a decoy phone number inside the SMS body.
- The test verifies that FlipAi resolves the newly opened conversation's exact sender/thread, captures the SMS exactly once, ignores the decoy identities, and does not treat the opened conversation header as a message.

## Calling isolation

- Google Voice calling behavior, profile, settings, and call state machine are unchanged.

No Authenticode/code-signing certificate is included in this release.
