# FlipAi v0.46.72

This patch fixes Muse sticky SMS routing after Muse has been selected with `MU:`.

## Muse sticky routing fix

- After `MU: ...` selects Muse, following SMS messages without a prefix now continue to Muse instead of falling back to Codex.
- The sticky command parser now dispatches Muse agent `U` through the Muse parser instead of the legacy Codex/Claude parser.
- Legacy sticky-agent validation and shared-number target validation now recognize Muse as a first-class provider.
- The fix does not change Muse WebView login/session handling or the other provider routes.

## Regression coverage

- Added a reproduction of the real failure: `MU: hi` stores `route:MU`, then an unprefixed `Check my last email` must resolve to Muse (`U`) with the original text preserved.
- Added coverage for legacy Muse sticky state and shared-number Muse targeting.
- Full tests and vet run before the patch metadata is committed.

No Muse API key is required; FlipAi uses the user's signed-in Muse web session.
