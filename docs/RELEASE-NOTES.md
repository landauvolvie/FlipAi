# FlipAi v0.46.66

This release fixes the remaining `OW NEW:` failure on ChatGPT Work.

## `OW NEW:`

- FlipAi now searches ChatGPT's mounted New chat controls even when the Work sidebar is collapsed or off-screen, including data-testid and navigation-link variants.
- If the current ChatGPT Work UI does not mount a separate New chat control at all, FlipAi no longer fails immediately. It creates a fresh Work session by switching from Work to Chat and back into Work through the same experience selector that already makes `OW:` work.
- FlipAi still verifies Work before the reset and waits for the fresh composer before sending the user's task.
- Regular Chat keeps its existing root-navigation fallback.

## Existing Work routing fixes retained

- Unprefixed follow-ups after `OW:` remain sticky to `route:OW`.
- Work acknowledgements and progress messages continue to say `ChatGPT Work`.

## Validation

- Added regression coverage for hidden/off-screen New chat controls and the Work -> Chat -> Work fallback.
- The normal Windows/Linux build, browser, security, installer, SBOM, checksum, and release workflows remain unchanged.

No Authenticode/code-signing certificate is included in this release.
