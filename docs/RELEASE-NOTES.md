# FlipAi v0.46.55

This release fixes generated-image replies over Google Voice and simplifies the default reply instruction sent to browser chat models.

## Image generation and MMS replies

- Long-running browser image generation no longer fails at the old 90-second response limit; FlipAi can keep waiting for generated media for up to 4 minutes.
- Progress text such as “Image”, “Creating your image”, or other transient working states is no longer treated as the final reply when an image is still being generated.
- FlipAi captures the generated browser-chat image and sends the actual image back through Google Voice as MMS instead of returning only a text status or link when direct media delivery is available.
- Returned browser media is preserved until the final Google Voice reply so progress messages cannot consume the image before delivery.
- The browser media path applies to ChatGPT, Gemini, Claude, Grok, and Microsoft Copilot chat integrations.

## Reply instruction

- The default reply hint is now exactly: `Please keep your reply short and to the point.`
- Existing installs using the previous SMS/plain-text reply instruction are migrated automatically.
- The old wording that mentioned SMS and plain text is no longer injected into model prompts.

## Validation

- Added tests for long browser image waits, generated-media capture and delivery, Windows browser behavior, and reply-hint migration.
- Branch CI completed successfully before merge.
- The release pipeline runs the full Linux and Windows tests, real-browser checks, vet/race checks, Google Voice smoke tests, Defender scans, installer smoke tests, SBOM generation, checksums, and provenance before publishing.

No Authenticode/code-signing certificate is included in this release.
