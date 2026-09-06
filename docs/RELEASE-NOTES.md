# FlipAi v0.46.43

An agent's answer is no longer thrown away when Google asks FlipAi to slow down.

## The fix

Texting an agent worked: the message was authenticated, routed, and answered. Delivering that answer back then failed with **"Google Voice reply failed: Google Voice web service is rate limiting FlipAi"**, and the reply was lost.

The reply got exactly one attempt. Google answered `429` — its "you are asking too often" response — and FlipAi treated that as final. The agent had already spent its turn producing the answer, and the person waiting on it had no other way to receive it.

Three things changed:

- **A refused reply is now sent again.** When Google answers `429` it has declined to process the request at all, so nothing went out and sending again delivers the message exactly once. Those are retried on a growing schedule that fits inside the delivery budget, honoring Google's own `Retry-After` when it asks for longer, and all attempts of one reply carry a single tracking id.
- **A reply whose outcome is unknown is never sent again.** A `5xx`, or a connection dropping while FlipAi was reading the response, may mean Google already sent the text. Sending again would deliver the same answer twice and charge for it, so those stop after one attempt. Reading the inbox still repeats freely, because a repeated read costs nothing and is discarded.
- A refusal on the merits stays final in both directions.
- **A reply now has priority over asking Google for the inbox.** Both share one quota, so the request to Google stands aside while a reply is being delivered. What the page has already received is still taken immediately, because that costs Google nothing — a text arriving mid-retry is delivered right away rather than waiting out the whole retry sequence.
- **FlipAi asks for the inbox far less often.** The active poll moves from every 3 seconds to every 15, backing off to 5 minutes while Google is unhappy. Frequent polling is what earned the rate limit in the first place, and it was spending the same quota the reply needed. Inbound text is not slower for it: the page's own conversation updates are still read as they arrive, which is what carries a newly arrived text.

## Also fixed

- The message shown when a reply failed said "it will retry more slowly", which described the inbox poll and not the reply. Nothing retried. The reply now genuinely retries, and the message says what actually happened.
- Readiness and the poll that proves it are now held together by a test. Slowing the poll without widening the readiness window would have declared a healthy listener dead between two of its own heartbeats — flickering "Not connected" and blocking replies for the gap.

## Regression coverage

- A rate-limited reply is sent again and Google's requested wait is honored; a reply whose outcome is unknown never is; a refusal on the merits is final for both.
- All attempts of one reply produce an identical payload, carrying one tracking id.
- A reply in flight does not suppress the passive drain.
- `Retry-After` is understood as seconds and as an HTTP date, and nonsense values are ignored.
- The retry schedule grows, stays capped, and fits inside the delivery budget the bridge allows.
- The readiness window outlasts the poll interval with margin for a missed poll.

## Unchanged

- Sender authorization: the conversation's own phone number, never a contact name.
- Google Voice calling behavior, profile, settings, and call state machine.

No Authenticode/code-signing certificate is included in this release.
