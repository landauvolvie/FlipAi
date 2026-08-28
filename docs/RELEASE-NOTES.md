# FlipAi v0.44.0

This release finishes the automatic Google Voice-to-Codex voice handoff.

## Automatic Codex voice start

When an allowed caller rings the Google Voice number, FlipAi now completes the whole sequence without requiring any button press:

1. answer the authorized Google Voice call;
2. find or launch the ChatGPT/Codex desktop voice frontend;
3. route the app to the virtual audio cables;
4. locate the real **Start new voice chat** control;
5. start voice mode;
6. verify that voice mode is actually active before the call is marked bridged.

Opening the desktop app by itself is not considered success.

## Automatic Electron accessibility recovery

The remaining field failure was an already-running ChatGPT/Codex desktop app exposing only its native window frame to Windows accessibility — for example `ChatGPT, Minimize, Maximize, Close` — while its renderer controls were invisible. The previous release could diagnose that case, but it still required the user to quit the app manually.

FlipAi now recognizes that exact signature and recovers automatically. It closes the inaccessible Electron process tree, restarts the same desktop app with `--force-renderer-accessibility`, reacquires the new window, reapplies per-app audio routing to the new process, and then starts the real voice chat. This recovery is attempted only for the title-bar-only accessibility signature and at most once per call, so an ordinary renamed or missing control will not cause an unnecessary app restart.

The clickable-control filtering remains in place, so conversation titles such as `Voice Chat Topic Summary` cannot be mistaken for the Voice button.

## Call end

When the Google Voice call ends, FlipAi still exits the desktop voice session and resets the bridge state for the next caller.

## Verified

The release pipeline covers the real-browser call-flow harness, the full test suite, Windows tests, `go vet`, race tests, Windows x64 build, FlipAi desktop/background lifecycle, Google Voice receiver checks, Microsoft Defender scanning when available, installer creation, and installer install/uninstall smoke testing.
