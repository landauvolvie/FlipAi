# FlipAi v0.46.20

This hotfix fixes the Claude Chat **Connect** screen showing `404 page not found`.

## Claude Chat Connect works

The v0.46.19 Agents page correctly posted Connect, Test, and Disconnect to Claude Chat-specific URLs, and the Claude Chat handlers already existed, but those three URLs were accidentally omitted from FlipAi's local HTTP router. The embedded FlipAi window therefore received a local 404 before Claude sign-in could open.

v0.46.20 registers all three Claude Chat action routes. Pressing **Connect** now reaches the Claude Chat sign-in handler and opens the dedicated persistent Claude browser session as intended. Test and Disconnect are wired through the same authenticated POST-only route table.

## Regression protection

A route-level test now requests all three Claude Chat action URLs and verifies that they reach FlipAi's POST-only action guard instead of falling through to 404. The normal Linux, Windows, race, browser, lifecycle, installer, and release gates still run before publishing.
