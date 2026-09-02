# FlipAi v0.46.7

This release moves the regular ChatGPT Chat experiment to the next backend-discovery step after v0.46.6 proved that the running desktop app exposes no obvious ChatGPT-owned loopback or Chromium debugging transport on the test PC.

## Deeper ChatGPT backend diagnostic

- Renames the action to **Run deep backend diagnostic** so the next test is clearly separate from an Enable action.
- Keeps ignoring neighboring `codex-*` pipes; they never count as regular ChatGPT Chat connectivity.
- Still checks for a ChatGPT-owned loopback listener or Chromium debugging channel first.
- When no live local transport is exposed, FlipAi now inspects only the installed ChatGPT application package/static resources for protocol clues such as conversation routes, websocket paths, custom protocols, preload IPC names, and renderer/main-process bridge markers.
- The static scan prioritizes `app.asar`, preload/renderer/main JavaScript, and other program resources.
- ChatGPT user-data/profile locations are explicitly skipped. The diagnostic does not read cookies, tokens, Local Storage, IndexedDB, session storage, process memory, or full process command lines.
- URL query strings and fragments are removed before any endpoint is reported, and credential-like marker text is discarded.
- Static protocol clues never turn the agent into a connected state. They only tell the next build which background request or IPC shape is worth testing.

## Why this step exists

The corrected v0.46.6 diagnostic found 13 ChatGPT desktop processes but no ChatGPT-owned local listener or DevTools connection. That means the next useful evidence has to come from how the installed desktop application's own code describes its Chat/IPC/network paths, without falling back to accessibility automation or a hidden browser.

This release still does not route SMS to regular ChatGPT Chat. Its purpose is to identify the backend protocol safely enough that a later build can attempt a harmless background Chat message and verify whether it appears in normal ChatGPT history.

## Verified

The normal FlipAi CI covers Linux and Windows tests, `go vet`, race tests, Windows x64 build, desktop/background lifecycle checks, Google Voice receiver validation, installer build, install/uninstall smoke test, Microsoft Defender scan when available, and SHA-256 generation.
