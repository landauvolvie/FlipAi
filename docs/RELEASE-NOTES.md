# FlipAi v0.46.60

This release follows the real-world `OW NEW:` failure seen after v0.46.59 and fixes the remaining ChatGPT Work fresh-session path.

## ChatGPT Work NEW

- `OW:` remains unchanged and continues to use ChatGPT Work.
- `OW NEW:` now prefers ChatGPT's own **New chat** control instead of immediately forcing a top-level navigation, which better preserves the selected Work experience.
- Work-mode detection now recognizes links as well as buttons/tabs/menu items, checks selected state on parent controls, understands `aria-current`, and can open a combined Chat/Work toggle before selecting Work.
- A top-level `chatgpt.com` navigation remains only as a fallback when the native New chat control cannot be found.
- FlipAi still verifies Work mode before sending the task; it does not silently fall back to normal Chat.

## Validation

- Added regression coverage for the native fresh-chat path and broader Work-mode control detection.
- The normal release pipeline still runs Linux and Windows tests, real-browser call-flow checks, vet/race checks, Google Voice checks, Microsoft Defender scans, installer smoke tests, SBOM generation, checksums, and provenance attestation before publishing.

No Authenticode/code-signing certificate is included in this release.
