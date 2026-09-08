# FlipAi v0.46.64

This release fixes ChatGPT Work fresh sessions and makes ChatGPT rich-card answers usable over SMS.

## ChatGPT Work NEW

- `OW:` continues to select ChatGPT Work before sending the task.
- `OW NEW:` now selects and verifies Work before pressing New chat, instead of opening a normal Chat session first and then trying to recover Work.
- If ChatGPT's global New chat control still falls back to regular Chat, FlipAi opens a blank root conversation and re-selects Work before any prompt is sent.
- The safety check remains fail-closed: FlipAi still will not send the task unless Work mode is verified.

## Gmail cards and rich UI over SMS

- ChatGPT is now explicitly instructed to include the important contents of email previews, cards, widgets, calendar items, files, and other rich UI in plain text too.
- The repeated ChatGPT rendering artifact `Unable to display this message due to an error. Reload the page to try again.` is filtered from extracted SMS replies instead of being forwarded to the phone.
- This keeps useful text such as package/email summaries deliverable even when ChatGPT also renders rich cards.

## Progress labels

- Work-mode acknowledgements and heartbeats now say `ChatGPT Work working on it…` / `ChatGPT Work still working…` rather than incorrectly saying `ChatGPT Chat`.
- Browser-route display names are kept mode-aware for ChatGPT Work and Claude web Code/Cowork routes.

## Validation

- Added regression tests for `OW NEW:` parsing and mode preservation, Work-before-New ordering, Work recovery, rich-card plain-text prompting, card-render-error cleanup, and mode-aware SMS status labels.
- The normal release pipeline still runs Linux and Windows tests, browser checks, vet/race checks, installer smoke tests, Defender scans, checksums, SBOM generation, and provenance attestation before publishing.

No Authenticode/code-signing certificate is included in this release.
