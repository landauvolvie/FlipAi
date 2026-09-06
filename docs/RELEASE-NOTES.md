# FlipAi v0.46.44

The reply is now sent by the signed-in Google Voice page itself, not by a separate HTTP client.

## The fix

Replies kept failing with **"Google Voice web service is asking FlipAi to slow down"**, and slowing down did not help. Across three releases not one reply ever succeeded: the Outgoing count stayed at zero while inbound texts arrived normally.

That pattern rules out a passing rate limit. Reading the inbox worked and sending never did, from the same session, with the same cookies and the same authorization — so the difference is not the credentials, it is who is asking. Sending a text is the operation abuse protection cares about, and a Go HTTP client is not the caller Google is willing to accept it from.

Inbound kept working because FlipAi already reads the conversation updates the page receives on its own. Outbound had no such path, so it failed every time.

FlipAi now issues the request from inside the signed-in Google Voice page, using the page's own `fetch` with its own session. This is what the capture script had already demonstrated was possible: it wraps `fetch` on the Voice page and sees the page's calls to the web service, which means those calls are ordinary requests from that page rather than something routed through a hidden frame. A request FlipAi makes there is the same request, from the same origin, with the same cookies.

The separate HTTP client remains only for a page that could not attempt the request at all. It is deliberately not a fallback for a request the page did attempt: once a request has gone out, repeating it through a second client could be a second text message.

## Failures now say what Google said

The response body was discarded on every error, leaving a status code and nothing else to work from. Google's own explanation — a quota name, a rejected field, a reason — is now read before the status is judged and carried into the message shown in Activity, for both request paths.

## Regression coverage

- The message body reaches the page as one encoded literal that decodes back to exactly what went in, so nothing in a text can become code in a signed-in Google session.
- Both request paths agree on what each status means, and carry Google's own words with it.
- A request the page could not complete is never sent again; a refusal is.
- The page is tried before the HTTP client, and the fallback stays limited to a page that never attempted the request.

## Unchanged

- Sender authorization: the conversation's own phone number, never a contact name.
- Google Voice calling behavior, profile, settings, and call state machine.

No Authenticode/code-signing certificate is included in this release.
