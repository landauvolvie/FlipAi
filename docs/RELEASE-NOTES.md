# FlipAi v0.46.62

This release retires the broken pre-sign-in Windows startup path and cleans obsolete Gmail transport noise from the published app.

## Windows startup

- Removed the **Start before sign-in** option from Settings.
- The old S4U `FlipAi Boot` scheduled task can no longer be created by FlipAi.
- Upgraded installs remove the retired task automatically when possible.
- If an old pre-login task still launches, FlipAi exits before starting the host, tray, or persistent WebView2 browser profiles.
- Normal hidden startup after the user signs in to Windows remains enabled.
- Interactive RDP/remote sessions remain supported and are not mistaken for the retired pre-login startup path.

## Published Google Voice transport

- The published bridge now starts only through the direct Google Voice transport.
- Retired Gmail/OAuth connection controls are no longer exposed as live setup paths.
- Old Gmail startup and connection messages are suppressed from Activity.
- Historical Activity entries created by the retired Gmail bridge are filtered on read so upgraded installs no longer show stale Gmail failures.
- Real Google Voice candidate events are preserved and relabeled as Google Voice instead of being hidden.
- A fresh install that has not connected Google Voice is reported as waiting for Google Voice setup instead of producing a Gmail error.

## Validation

- Added regression coverage for the retired pre-sign-in startup path, RDP-safe interactive startup detection, and Gmail-log cleanup.
- The normal release pipeline runs Linux and Windows tests, real-browser call-flow checks, vet/race checks, Google Voice checks, Microsoft Defender scans, installer smoke tests, SBOM generation, checksums, and provenance attestation before publishing.

No Authenticode/code-signing certificate is included in this release.
