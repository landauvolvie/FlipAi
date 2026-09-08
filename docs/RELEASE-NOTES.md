# FlipAi v0.46.71

This patch fixes the Muse.ai Agents-page integration shipped in v0.46.70.

## Muse visibility fix

- Muse now appears in the final registered Agents page instead of being removed by a later live-connection UI registration pass.
- The Agents shortcut legend now consistently includes `MU = Muse` in both server-rendered markup and the client-side header update.
- Muse keeps its Connect, Test, Disconnect, live status, SMS shortcut, allowed phone numbers, PIN security, and progress settings in the visible Agents workbench.
- The fix does not change the existing Muse browser runtime, persistent WebView2 profile, SMS routing, or `MU NEW:` conversation behavior introduced in v0.46.70.

## Regression coverage

- Added a regression test against the actual final registered Agents template, so CI fails if a later UI pass removes the Muse card or pane again.
- Updated the unified Agents tests from seven to eight providers and added Muse coverage for shortcut, PIN, progress, shared SMS instruction, and prompt composition.
- Release validation keeps the executable version, installer version, and `VERSION` file synchronized at 0.46.71.

No Muse API key is required; FlipAi uses the user's signed-in Muse web session.

No Authenticode/code-signing certificate is included in this release.
