# FlipAi v0.46.53

This release finishes the direct Google Voice media/startup work from v0.46.51 and gives FlipAi a much cleaner update experience without changing the rest of the app layout.

## Update experience

- New releases begin downloading automatically in the background as soon as FlipAi detects them.
- The small control beside the FlipAi version now shows live percentage progress instead of a generic download icon.
- At 100%, the progress control becomes a compact **Install update** button; the user chooses when to click it.
- Partial installer downloads are kept as private `.part` files and resume after an app or computer restart when the server supports HTTP Range requests.
- A fully downloaded, checksum-verified update installs automatically the next time FlipAi or Windows restarts if the user has not installed it manually first.
- After either manual or restart-triggered installation, FlipAi always reopens the application window.
- Update banners and update controls remain removed from Settings; only the small version-area updater is shown.

## Included direct Google Voice work

- Incoming Google Voice MMS photos, audio/voice notes, and supported video are delivered to browser-backed AI agents as the actual file rather than the text “MMS received.”
- Browser-agent returned images/files are captured from the provider page. Images are sent back through Google Voice MMS when supported; unsupported/oversized media falls back to the exact provider conversation link.
- Gmail forwarding is no longer exposed in the app UI; the old Gmail implementation remains preserved in Git history/rollback branch.
- FlipAi starts hidden at Windows sign-in and recovers the direct Google Voice background listener without requiring the main window to be opened manually.

No transcription is added.
