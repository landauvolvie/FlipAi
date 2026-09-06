# FlipAi v0.46.42

Direct Google Voice SMS reads the sender from the conversation, so allowed numbers are answered.

## The fix

Every inbound text was blocked with **"conversation phone does not match sender; no reply sent"**, including texts from a number that was on the allowlist.

FlipAi read the sender from the `did` field the Google Voice web service reports on each conversation item. That field is not the person texting: it is the Google Voice number on this account's own side of the conversation. So the sender never matched the conversation it arrived in, and the identity cross-check blocked every genuine text.

The sender is now taken from the conversation's own `t.+1XXXXXXXXXX` identity, which is what Google uses to name a one-to-one SMS thread. The `did` is recorded for diagnostics and is never treated as the sender.

## Why this is the safer identity, not a relaxed one

The conversation's number is also the exact address a reply is delivered to. Authorizing that number and answering that number are therefore the same decision: metadata arriving alongside a conversation cannot borrow an allowed number and have an unrelated conversation answered, because the reply still goes to the conversation.

- A conversation with no phone-number identity of its own — a group thread, or an older locator — is now blocked outright. It could never be attributed to a sender or replied to, so it fails closed at the gate instead of part-way through delivery.
- Sender authorization continues to use the normalized phone number only. Contact names are never consulted, in Google Voice or anywhere else.
- Every shape of the same number is the same number: `+18453241813`, `8453241813`, `845-324-1813`, `(845) 324-1813`, `845 324 1813` and `1-845-324-1813` all resolve to one identity, both in the allowlist and in what Google reports.

## Also fixed

- A text whose first word was "You:" was dropped silently, with nothing written to Activity. That heuristic belonged to the retired conversation-list reader; direction now comes from the conversation item itself, and the outgoing-reply ledger catches anything mislabelled. A real text is no longer swallowed for beginning with a word.
- The outgoing-reply ledger is now keyed on the same number in both directions. It previously recorded a reply against the recipient but looked it up against this account's own number, so it could not have matched.

## Regression coverage

- The sender is taken from the conversation and this account's own number is kept separate.
- A claimed sender that is on the allowlist cannot get an unallowed conversation answered.
- A conversation without a phone-number identity is blocked.
- All seven written shapes of one phone number authorize that number and resolve to one sender.

## Unchanged

- Google Voice calling behavior, profile, settings, and call state machine.
- The reply path, which remains tied to the exact captured conversation.

No Authenticode/code-signing certificate is included in this release.
