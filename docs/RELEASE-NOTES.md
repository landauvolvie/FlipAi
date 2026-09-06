# FlipAi v0.46.43

An agent's answer is no longer thrown away when Google asks FlipAi to slow down.

## The fix

Texting an agent worked: the message was authenticated, routed, and answered. Delivering that answer back then failed with **"Google Voice reply failed: Google Voice web service is rate limiting FlipAi"**, and the reply was lost.

The reply got exactly one attempt. Google answered `429` — its "you are asking too often" response — and FlipAi treated that as final. The agent had already spent its turn producing the answer, and the person waiting on it had no other way to receive it.

Three things changed:

- **A busy answer is now asked again.** `429` and `5xx` responses, and connections that never completed, are retried on a growing schedule that fits inside the delivery budget. Google's own `Retry-After` wins whenever it asks for longer. A refusal is still final: retrying one only wastes the budget and fails anyway.
- **A reply now has priority over reading the inbox.** Both share one quota with Google, so the inbox poll stands aside while a reply is being delivered. A reply that cannot get out is a worse failure than an inbox read a few seconds late.
- **FlipAi asks for the inbox far less often.** The active poll moves from every 3 seconds to every 15, backing off to 5 minutes while Google is unhappy. Frequent polling is what earned the rate limit in the first place, and it was spending the same quota the reply needed. Inbound text is not slower for it: the page's own conversation updates are still read as they arrive, which is what carries a newly arrived text.

## Also fixed

- The message shown when a reply failed said "it will retry more slowly", which described the inbox poll and not the reply. Nothing retried. The reply now genuinely retries, and the message says what actually happened.
- Readiness and the poll that proves it are now held together by a test. Slowing the poll without widening the readiness window would have declared a healthy listener dead between two of its own heartbeats — flickering "Not connected" and blocking replies for the gap.

## Regression coverage

- A rate-limited reply is retried and Google's requested wait is honored; a refusal is not retried.
- `Retry-After` is understood as seconds and as an HTTP date, and nonsense values are ignored.
- The retry schedule grows, stays capped, and fits inside the delivery budget the bridge allows.
- The readiness window outlasts the poll interval with margin for a missed poll.

## Unchanged

- Sender authorization: the conversation's own phone number, never a contact name.
- Google Voice calling behavior, profile, settings, and call state machine.

No Authenticode/code-signing certificate is included in this release.
