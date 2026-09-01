# FlipAi v0.46.6

This release corrects the first ChatGPT Chat direct-backend diagnostic so it cannot report a successful ChatGPT connection merely because Codex pipes exist on the same PC.

## ChatGPT Chat diagnostic fix

- Fixes the false-positive result seen on the real Windows test PC where `codex-browser-use-*`, `codex-computer-use-*`, and `codex-ipc` were incorrectly counted as ChatGPT Chat backend candidates.
- Codex pipes are now listed separately and explicitly ignored for regular ChatGPT Chat connectivity.
- A globally visible named-pipe name by itself no longer turns the diagnostic green because the pipe namespace does not prove which process owns that pipe.
- The diagnostic only reports a proven candidate when Windows ties a loopback/Chromium transport to a ChatGPT process.
- The Agents page now says **Not connected** and labels the button **Run backend diagnostic** so it cannot be mistaken for an Enable button.
- The page explicitly states that the diagnostic does not enable ChatGPT and that SMS routing is unavailable until a usable direct backend is proven and tested.
- Adds a regression test for the exact false-positive pipe set found on the user's PC.

## What this means

The v0.46.5 green result did not prove that regular ChatGPT Chat was connected. The test machine showed the ChatGPT desktop app running, no ChatGPT-owned loopback listener, no ChatGPT-owned Chromium DevTools listener, and only Codex-named pipes. v0.46.6 reports that state accurately instead of presenting it as success.

This release does not enable regular ChatGPT Chat as an SMS agent yet. It makes the diagnostic trustworthy so the next direct-backend work starts from verified ChatGPT-owned transport evidence rather than Codex IPC from the neighboring agent.

## Verified

The normal FlipAi CI still covers Linux and Windows tests, `go vet`, race tests, Windows x64 build, desktop/background lifecycle checks, Google Voice receiver validation, installer build, install/uninstall smoke test, Microsoft Defender scan when available, and SHA-256 generation.
