# FlipAi v0.46.48

FlipAi was signing every request with a built-in Google key instead of your own.

## What RESOURCE_EXHAUSTED was actually saying

Google Voice kept refusing replies with `RESOURCE_EXHAUSTED`. That is the answer a Google API gives when the **key** a request is signed with is out of quota — not when the message is wrong, and not when the account is busy. Waiting, retrying and slowing down could never have helped.

FlipAi reads the key Google serves this session out of the page. It read it from the main page — but the calls to the web service are made by a small helper frame, and the key is only visible there. The lookup therefore always came back empty, and FlipAi fell back to a key built into the app: a public one, long since published and long since out of quota.

The same mistake hid in three places at once: the key, the request headers, and the value that reveals how Google signs. All three were read from a page that never makes the call.

## Why nothing else revealed it

Reading your texts kept working, so the connection looked healthy. It works because FlipAi reads the conversation updates the page receives on its own — it does not need to ask the service anything. Sending has no such path, so sending was the only thing that ever showed the failure.

## The fix

All three are now read from the frame that actually calls the service, through the two things that frame shares: its storage and its own record of the requests it made.

The Connections card now says which key is in use, so this cannot hide again:

- *using this session's own Google key* — correct.
- *WARNING: this session's Google key was not found, so requests use a built-in one the service refuses* — the state every release until now was silently in.

## Regression coverage

- The key, the headers and the signing origin are all read from the calling frame; none may go back to the main page.
- The key stays reachable from an isolated world through both storage and the frame's own request record.

## Unchanged

- Sender authorization: the conversation's own phone number, never a contact name.
- A reply whose outcome is unknown is still never sent twice.

No Authenticode/code-signing certificate is included in this release.
