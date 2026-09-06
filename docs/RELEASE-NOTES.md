# FlipAi v0.46.53

This release finishes the direct Google Voice media/startup work and replaces the old update presentation with the compact update flow in the sidebar.

## Updates

- A newly published FlipAi version starts downloading automatically in the background.
- The existing version area now shows the real download percentage with a small circular progress indicator.
- At 100%, after checksum verification, the progress control becomes a compact **Install update** button.
- The user can keep working and choose when to install.
- If FlipAi or Windows is actually restarted while a verified update is staged, the owning watchdog installs it automatically and reopens the FlipAi application. Simply opening an already-running FlipAi does not force the install.
- Partial installer downloads stay under a non-executable `.part` filename and resume with HTTP Range requests after an app/PC restart when GitHub supports ranges.
- Update progress is stored in FlipAi's private update state so the sidebar survives restarts cleanly.
- The old page-wide update banner and Settings update card remain removed; no unrelated app UI is changed.

## Included direct Google Voice work

- Incoming Google Voice MMS media is delivered to supported browser agents as the actual local file, without transcription or a download-link substitution.
- Browser-chat returned images/files are captured; supported images are sent through Google Voice MMS and unsupported/oversized returned media falls back to the exact provider conversation URL.
- The Gmail connection UI remains retired while the old implementation is preserved in Git history and the `archive/gmail-voice-bridge-v0.46.50` rollback branch.
- Hidden Windows sign-in startup and direct Google Voice background-worker recovery remain enabled.

## Validation

The release pipeline runs Linux and Windows tests, real-browser flow tests, vet/race checks, Windows builds, Google Voice background smoke checks, Defender scans, installer install/uninstall smoke tests, SBOM generation, checksums and build provenance before publishing.
