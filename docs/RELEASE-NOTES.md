# FlipAi v0.46.46

FlipAi learns how to send a text by watching Google Voice send one.

## What the error finally said

With the response body no longer discarded, Google Voice named its own refusal:

> RESOURCE_EXHAUSTED — `{"error_code":"RESOURCE_EXHAUSTED","base64_format":"CAI=","protojson_fava_format":"[2]"}`

That is a Google Voice error, not a generic quota message: `CAI=` decodes to two bytes meaning field 1 = 2, and `[2]` says the same thing. The service is rejecting the request itself, which is why waiting and retrying never helped and why not one reply has ever gone out.

## The fix

The body FlipAi sends was written from a guess at the shape this endpoint wants, and a guess is what the service keeps refusing.

Google Voice builds a correct one every time you send a text yourself from the window FlipAi already runs. FlipAi now records that request and reuses its exact structure, changing only what has to change: the conversation, the message, and the tracking id. The slots are found by what they contain rather than by position, so a reordering on Google's side cannot put the message where the conversation belongs — and anything whose slots cannot be identified is refused outright rather than half-rewritten.

## What this asks of you, once

**Send one text yourself from the Google Voice window FlipAi opens.** Any text, to anyone. That is what teaches FlipAi the format.

The Connections card now says which state it is in:

- *reply format learned from Google Voice* — nothing more to do.
- *reply format not yet learned; send one text yourself from the Google Voice window to teach it* — send that one text.

Until a real send has been observed, FlipAi falls back to the built-in shape, which is the one being refused.

## Unchanged

- Sender authorization: the conversation's own phone number, never a contact name.
- A reply whose outcome is unknown is still never sent twice.
- Reading the inbox, and Google Voice calling.

No Authenticode/code-signing certificate is included in this release.
