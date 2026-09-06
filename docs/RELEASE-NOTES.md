# FlipAi v0.46.41

Direct Google Voice SMS reads the page's own Google Voice traffic, and reports its real state.

## Google Voice SMS transport

- Direct Google Voice SMS sends and receives through the authenticated Google Voice web service instead of driving the page. No conversation is opened, clicked, or selected.
- A capture script installed before any page script wraps the page's own `fetch` and `XMLHttpRequest`, so FlipAi reads the conversation updates Google Voice already receives. A text that arrives while the page is refreshing needs no request of its own.
- The live API key, client version, and request headers now come from the request Google Voice actually made, rather than from constants that go stale.
- The active inbox poll backs off from a fixed 1.2 seconds to 3 seconds, with exponential backoff up to 60 seconds while the service is unhappy. Polling a private endpoint sub-second is what gets a session rate limited.

## Fixes

- **The listener can now report itself ready.** Readiness was proven through a timestamp belonging to the retired DOM observer, which nothing writes any more, so `ListenerRunning` and `Ready` were cleared on every read. Connections stayed "Not connected", Test timed out after 15 seconds, and every reply failed with "background connection is not ready" while the listener was in fact polling. Readiness now follows the poll that actually runs, through one shared check that the Connections card, Test, and the outbound send gate all use.
- **Requests are signed the way Google signs them.** The `SAPISIDHASH` was computed over `https://voice.google.com` while the request was sent with an `Origin` of `clients6.google.com`, and any of four cookies was signed under the plain `SAPISIDHASH` name. Both produce an HTTP 401 that is indistinguishable from a signed-out browser. The signing origin is now recovered by matching one authorization value the page itself produced, every signing cookie the session holds is sent under its own scheme name, and an unproven origin is retried rather than assumed.
- **Messages are no longer dropped silently.** Only the exact conversation item types `smsIn`, `smsOut` and `sms` were understood; any other encoding skipped the entire inbox with nothing recorded. The spellings Google has shipped are accepted, an explicit outgoing flag wins when present, and an item whose direction cannot be established is skipped and named rather than guessed at.
- **One sign-in survives a restart.** `Running`, `Starting`, `Visible` and `LoginActive` describe a process but persist in a file, so a state file that outlived its process described one that no longer existed. A persisted `Starting` blocked the restart guard and a persisted `LoginActive` made the supervisor believe a sign-in window was still open, leaving a connected install down after a reboot. Stale flags are now recognized by a state that has stopped moving with no window present, and the hidden background browser is restored.

## Safety and permissions

- Sender authorization continues to use the normalized phone number only. Saved contact names are display-only and are never used as sender identity.
- FlipAi still requires an exact Google Voice `t.+1XXXXXXXXXX` sender/thread identity before routing a text to an agent; unresolved or mismatched messages fail closed and are logged as blocked.
- Replies remain tied to the captured exact phone/thread identity.
- A second, independent loop guard remembers what FlipAi just sent, so a mislabelled conversation item cannot make FlipAi answer its own reply.
- The capture script only observes traffic the page was already making. A regression test fails the build if it ever gains the ability to drive the page.

## Diagnostics

- The Connections card now reports what each inbox check saw: how many conversations and messages were read, how many inbox updates were observed from the page, and the exact encoding of any conversation item that was skipped.
- A signed-out session is now reported as a sign-in problem in its own words, instead of as a generic listener failure.

## Regression coverage

- 21 new tests covering readiness and its expiry across processes, authorization scheme coverage and signing-origin recovery, the captured request template, every direction encoding and the unknown-encoding skip, the outgoing-reply loop guard, and restart recovery from stale process flags.

## Calling isolation

- Google Voice calling behavior, profile, settings, and call state machine are unchanged.

No Authenticode/code-signing certificate is included in this release.
