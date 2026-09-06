# FlipAi v0.46.47

Two fixes to the learned reply format shipped in v0.46.46, without which it could never have worked.

## The format was being read from the wrong place, twice over

v0.46.46 records the request Google Voice makes when you send a text, so FlipAi can reuse its shape. It recorded it correctly and then looked for it somewhere it could never be.

First, it asked the main page. The recording lives with whichever part of the page made the request, and the send is made by the small helper frame — the same separation that made v0.46.44's request get refused.

Second, and less obvious: the only way FlipAi can run code in that helper frame gives it a separate set of variables from the frame's own. It shares the frame's storage, but not what the recording script had put in memory. Asking the right frame the wrong way would still have found nothing.

The recording is now handed over through the frame's storage, which both sides can reach, and cleared once the shape is safely saved so the message it came from does not linger there. Either way the format would have read as never learned no matter how many texts you sent.

## The format is now remembered

The page keeps what it recorded only until it reloads, so an app restart or a page navigation threw it away and replies fell back to the shape the service refuses. The advertised one-time step would have been a step before every reply.

The learned shape is now saved and restored across restarts. **Only the shape is kept**: the conversation and the message text are replaced with placeholders before anything is written, so what persists is the structure Google Voice uses and never what anyone said.

FlipAi also learns the shape during its ordinary inbox checks, not only when a reply is attempted, so a text you send is picked up within seconds.

## What this asks of you, once

**Send one text yourself from the Google Voice window FlipAi opens.** Any text, to anyone.

The Connections card says which state it is in:

- *reply format learned from Google Voice* — nothing more to do, now or after any restart.
- *reply format not yet learned; send one text yourself from the Google Voice window to teach it*

## Regression coverage

- The captured send is read from the frame that made it, not the main page.
- A learned shape survives a restart, still fills in correctly afterwards, and carries neither the conversation nor the message it was learned from.
- An unusable capture is never stored.

No Authenticode/code-signing certificate is included in this release.
