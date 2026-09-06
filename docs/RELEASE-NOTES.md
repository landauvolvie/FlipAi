# FlipAi v0.46.45

Restores the SMS listener, and sends from a frame whose origin Google accepts.

## What v0.46.44 broke, and why

v0.46.44 moved every Google Voice web-service request into the signed-in page. It was based on a wrong reading of the evidence: FlipAi's capture script sees the page's own calls to the service, and that looked like proof those calls come from the Voice page itself.

They do not. A script installed at page start runs in **every frame**, so what the capture saw were the calls of a small helper frame Google loads from the web service's own address. A request issued from the main Voice page instead carries one address while talking to another, and Google refuses it:

> Bad request: Origin doesn't match Host for XD3.

Because that path was used for reading as well as sending, the SMS listener stopped starting at all. Inbound text detection stopped with it. That regression is the first thing this release fixes.

## The fix

FlipAi now looks for a frame already loaded from the web service's own address and runs the request there, where the two agree. When no such frame is present there is nothing to run in, so the request goes back to the direct client, which sets the matching address itself — the path that read the inbox successfully in every release before v0.46.44.

The result is that reading works again immediately, and sending gets a genuinely different attempt rather than the one Google was refusing.

## Diagnostics kept

The error above is visible only because v0.46.44 started reading Google's own words out of a failed response instead of discarding them. That stays, and it is what made this a single-look diagnosis instead of another guess.

## Regression coverage

- The page request runs in a frame on the service's own address, never in the main page.
- With no such frame, the request reports the one error the caller falls back on, so reading the inbox keeps working.
- The frame search and the isolated context it opens are both pinned.

## Unchanged

- Sender authorization: the conversation's own phone number, never a contact name.
- A reply whose outcome is unknown is still never sent twice.
- Google Voice calling behavior, profile, settings, and call state machine.

No Authenticode/code-signing certificate is included in this release.
