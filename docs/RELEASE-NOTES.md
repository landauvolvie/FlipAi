# FlipAi v0.46.4

This release cleans up Settings so the page contains only the app-level controls a normal user needs, while the detailed agent and connection configuration stays on the pages that own it.

## Settings cleanup

- Removed the large status tiles from the top of Settings.
- Updates are now a compact version/check area. Automatic installation and update-frequency controls are no longer exposed.
- FlipAi checks for a new release in the background on a 50-minute default cadence; installing a new version remains a deliberate action.
- Startup now shows only **Start FlipAi with Windows** and **Start before sign-in**.
- Removed Appearance, Notifications, This install, Shared routing, Local service, Log files, Service tools, Close to tray, and Repair startup from Settings.
- Fresh installs keep the light appearance by default.
- Agent-owned routing and conversation behavior remain on the Agents page instead of being duplicated in Settings.

## Google Voice calling

The calling section is less overwhelming: the main calling switch and Google Voice account stay visible, while detailed call status/diagnostics and desktop voice-app controls are grouped into expandable sections.

## Verified

Regression coverage checks the simplified Settings surface, the 50-minute update migration, and disabled unattended installs. The normal build workflow also runs the real-browser Google Voice harness, Linux and Windows test suites, `go vet`, race tests, Windows x64 build, Google Voice receiver validation, installer build, install/uninstall smoke test, Microsoft Defender scan when available, and SHA-256 generation.
