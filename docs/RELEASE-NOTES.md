# FlipAi v0.46.39

Google Voice SMS sender-resolution fix for conversations saved under a contact name such as **Me**.

## Google Voice SMS

- Fixed the remaining case where FlipAi detected an incoming direct Google Voice SMS but blocked it with **“exact sender phone number could not be resolved”** even though the phone number was already allowed for the selected agent.
- Google Voice can keep the browser on `/messages`, show only the saved contact name in the conversation list, and expose the sender number only in the opened conversation header, for example **`mobile • (845) 555-0142`**.
- FlipAi now recognizes the dedicated opened Google Voice conversation header (`gv-message-list-header`) and resolves the sender when that header contains exactly one normalized US/Canada phone number.
- The resulting sender/thread still uses the exact normalized `t.+1XXXXXXXXXX` Google Voice identity expected by the direct SMS bridge.

## Safety and routing

- Visible phone text is accepted only from the explicitly opened conversation header, not from arbitrary page text, contact names, conversation previews, or SMS message bodies.
- If the opened header contains zero or multiple phone numbers, FlipAi continues to fail closed unless trusted Google Voice metadata already provides the exact identity.
- If trusted identity metadata and the visible opened-header number disagree, the message remains blocked.
- Existing per-agent phone permissions, routing codes, sticky-agent behavior, security codes, STATUS, NEW, acknowledgements, and progress updates are unchanged.

## Regression coverage

- Added a real Chromium regression matching the reported screen: saved contact name **Me**, unchanged `/messages` URL, sender exposed only as `mobile • (845) 555-0142` in the opened header, unrelated preloaded conversation metadata, and a decoy phone number inside the SMS body.
- The regression verifies that FlipAi resolves the opened header phone, creates the correct thread identity, captures the SMS exactly once, ignores unrelated/decoy phone numbers, and never treats the conversation header as a second SMS.

## Calling isolation

- Google Voice calling behavior, profile, settings, and call state machine are unchanged.

No Authenticode/code-signing certificate is included in this release.
