# FlipAi v0.46.73

Updates now use a single small icon beside the app version.

- New releases download and verify in the background, with checks every 30 seconds. Download progress appears in the icon tooltip.
- The icon becomes clickable once the update is ready. Clicking it installs silently and reopens FlipAi.
- A downloaded update installs automatically on the next app start or Windows startup. Background startup stays in the background.
- Reopening the window while FlipAi is already running does not trigger installation.
- Installation uses the verified local download and works offline. Repeated clicks and overlapping startups share one installer handoff.
- Removed legacy update result pages and flash notices. No update banners, dialogs, or extra Settings controls.
- Update state is saved by file replacement so a restart cannot read a half-written progress record.

Validation includes updater lifecycle and browser interaction regression tests, plus the normal Linux/Windows tests, vet, race checks, installer checks, checksums, and release provenance pipeline.
