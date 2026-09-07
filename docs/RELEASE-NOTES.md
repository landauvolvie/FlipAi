# FlipAi v0.46.59

This release fixes starting an explicit new ChatGPT Work session from SMS.

## ChatGPT Work NEW

- `OW:` continues to use ChatGPT Work exactly as before.
- `OW NEW:` now waits for ChatGPT's fresh page UI to finish rendering before FlipAi verifies Work mode.
- The fix targets the fresh-session path that could report a valid signed-in session before the Work selector and composer were ready.
- FlipAi still refuses to send a task if it cannot verify Work mode, so the safety check is preserved rather than bypassed.

## Validation

- Added regression coverage for the fresh-page readiness guard used before Work-mode verification.
- The normal release pipeline still runs Linux and Windows tests, real-browser call-flow checks, vet/race checks, Google Voice checks, Microsoft Defender scans, installer smoke tests, SBOM generation, checksums, and provenance attestation before publishing.

No Authenticode/code-signing certificate is included in this release.
