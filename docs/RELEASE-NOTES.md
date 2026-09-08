# FlipAi v0.46.65

This release fixes two ChatGPT Work routing regressions reproduced from Google Voice.

## `OW NEW:` fresh Work sessions

- `OW NEW:` now verifies ChatGPT Work first, clicks ChatGPT's native New chat control inside the Work experience, waits for the fresh composer, and verifies Work again before any task is sent.
- The previous generic `chatgpt.com` root fallback is removed from this path because it could drop the browser back into regular Chat and cause `FlipAi could not verify ChatGPT Work mode`.
- The existing fail-closed safety remains: if Work cannot be verified, FlipAi does not send the task into the wrong experience.

## Sticky ChatGPT Work follow-ups

- Selecting `OW:` now persists the exact SMS route as `route:OW`, not only the shared ChatGPT engine ID.
- Unprefixed follow-ups therefore stay in ChatGPT Work instead of silently decoding back to regular ChatGPT Chat.
- Acknowledgements and heartbeats for those follow-ups now correctly say `ChatGPT Work working on it…` / `ChatGPT Work still working…`.
- Legacy single-letter sticky values remain readable for existing installs.

## Validation

- Added regression coverage proving an unprefixed follow-up after `OW:` keeps Work mode and the Work display label.
- Added regression coverage proving `OW NEW:` selects Work before New chat, re-verifies Work afterward, and never uses the generic-root fallback.
- The normal release pipeline still runs the full Linux and Windows test suites, browser checks, vet/race checks, installer smoke tests, Defender scans, SBOM generation, checksums, and provenance attestation before publishing.

No Authenticode/code-signing certificate is included in this release.
