# FlipAi v0.46.61

This patch fixes the Google Voice failure reproduced after a Windows restart on September 7.

## Google Voice restart recovery

- A temporary signed-out page while the private WebView2 profile is restoring no longer erases the saved Google Voice connection.
- Once Google Voice is signed in, FlipAi explicitly records the connection again so the background supervisor can restore it after future app or Windows restarts.
- Explicit **Disconnect** is still the only path that removes the saved Google Voice SMS browser profile.

## Stale MMS recovery

- Fresh incoming MMS messages still retry briefly while Google Voice exposes the media element.
- Old `MMS Received` records whose media is no longer available are converted into non-executable checkpoint candidates instead of failing every second forever.
- This prevents the repeated `Could not read matching Gmail message` / `No MMS media element was found` loop after reconnecting Google Voice, and prevents an old MMS marker from being replayed as a new command.

## Validation

- Added regression coverage for Google Voice saved-session behavior across restart.
- Added regression coverage proving fresh MMS remains retryable while stale missing-media records are safely checkpointed.
- The normal release pipeline runs Linux and Windows tests, real-browser call-flow checks, vet/race checks, Google Voice checks, Microsoft Defender scans, installer smoke tests, SBOM generation, checksums, and provenance attestation before publishing.

No Authenticode/code-signing certificate is included in this release.
