# FlipAi v0.46.53

FlipAi now has a compact, persistent update flow in the sidebar instead of a separate update screen or popup.

## What changed

- A newly published update starts downloading automatically in the background as soon as FlipAi discovers it.
- The version area shows a small circular download indicator with the live percentage while the installer is transferring.
- 100% is shown only after the installer has finished downloading and passed its published SHA-256 verification; the progress control then becomes a compact **Install update** button.
- The user can leave the verified update staged and choose when to press Install.
- If FlipAi or Windows is restarted while a verified update is waiting, FlipAi installs that staged update automatically on restart and reopens the FlipAi application afterward.
- Download state and percentage are persisted locally, failed downloads retry quietly, and incomplete `.part` files are never treated as installers.
- The update section remains removed from Settings and there are no update popups or unrelated interface changes.
- All Google Voice media, startup, Copilot, and exact AI-mode routing improvements from the previous releases remain included.

## Validation

The release pipeline runs the full Linux and Windows Go test suites, real-browser flow tests, vet and race tests, the Windows x64 build, Google Voice background smoke tests, Microsoft Defender scans, installer install/uninstall smoke tests, SBOM generation, checksums, and build provenance before publishing the release.

No Authenticode/code-signing certificate is included in this release.
