# FlipAi v0.46.36

Google Voice SMS phone-authorization and exact-thread reply hardening.

## Google Voice SMS security

- Direct Google Voice SMS authorization is now based only on the sender's normalized phone number. A saved Google Voice contact name is never treated as permission.
- When Google Voice displays a contact name instead of the number, FlipAi resolves the phone number from Google Voice identity metadata before authorization.
- Phone numbers written inside the SMS body cannot be mistaken for the sender. The real-browser regression test includes a different valid-looking phone number inside the message text.
- Every inbound Google Voice text is logged before authorization. Unauthorized, unresolved, calls-only, or unverifiable conversations are blocked before they can reach the queue or any AI agent, and no reply is sent.
- Activity shows blocked Google Voice SMS events with **Blocked** status, the reason, and the normalized sender phone number.
- SMS permission continues to respect each agent's existing phone permissions; a calls-only number cannot gain SMS access through the direct Google Voice transport.

## Exact reply targeting

- Replies are bound to both the exact inbound Google Voice Messages thread and the same normalized sender phone number.
- Before sending, FlipAi verifies that the exact conversation row still resolves to the expected phone number. A missing or mismatched thread fails closed instead of sending elsewhere.
- Contact-name searching and the old ambiguous "single suggestion" fallback are not used for replies.

## Routing and calling

- Existing routing codes, security codes, sticky-agent behavior, STATUS, NEW, acknowledgements, progress updates, and all supported agents remain unchanged.
- Google Voice calling remains separate and untouched.

No Authenticode/code-signing certificate is included in this release.
