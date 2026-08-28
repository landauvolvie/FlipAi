# FlipAi v0.44.0

This release finishes the automatic Google Voice-to-Codex voice handoff.

## Automatic Codex voice start

When an allowed caller rings the Google Voice number, FlipAi now completes the whole sequence without requiring any button press:

1. answer the authorized Google Voice call;
2. find or launch the ChatGPT/Codex desktop voice frontend;
3. route the app to the virtual audio cables;
4. locate the real **Start new voice chat** control;
5. start live Voice;
6. verify that live Voice is actually active before the call is marked bridged.

Opening the desktop app or opening a text chat by itself is not considered success.

## Live Voice control selection

A field test of the first v0.44 build exposed one more ambiguity in the Windows accessibility tree: ChatGPT can expose text dictation/microphone controls alongside the real live Voice control. The old matcher accepted the first broadly voice-related clickable control, so an answered call could reach ChatGPT and open chat without actually starting a spoken conversation.

FlipAi now scans all candidates and ranks the actual live Voice controls. **Start new voice chat** has highest priority; Voice Mode, live-voice and headphone/headset controls are accepted as compatible names. Generic microphone, mic, dictation and standalone `voice` matches are no longer treated as proof that live Voice can be started.

## Automatic Electron accessibility recovery

An already-running ChatGPT/Codex desktop app can expose only its native window frame to Windows accessibility — for example `ChatGPT, Minimize, Maximize, Close` — while its renderer controls are invisible.

FlipAi recognizes that exact signature and recovers automatically. It closes the inaccessible Electron process tree, restarts the same desktop app with `--force-renderer-accessibility`, reacquires the new window, reapplies per-app audio routing to the new process, and then starts the real voice chat. This recovery is attempted only for the title-bar-only accessibility signature and at most once per call, so an ordinary renamed or missing control will not cause an unnecessary app restart.

The clickable-control filtering remains in place, so conversation titles such as `Voice Chat Topic Summary` cannot be mistaken for the Voice button.

## Windows virtual-audio routing fix

The same field test also exposed a Windows audio-policy interop mismatch. The per-app routing API expects the endpoint device ID as a native HSTRING handle. FlipAi was marshaling that Set argument as a CLR string, which some Windows builds reject even when the AudioPolicyConfig factory itself is available.

v0.44 now uses the native HSTRING ABI, creates and releases the HSTRING explicitly, persists the Console and Multimedia roles used by Windows/EarTrumpet, and attempts the Communications role as best effort for voice apps. FlipAi still tries both the current SWD endpoint path and the raw MMDevice ID for Windows-version compatibility.

## Call end

When the Google Voice call ends, FlipAi exits the desktop voice session and resets the bridge state for the next caller.

## Verified

The release pipeline covers the real-browser call-flow harness, the full test suite, Windows tests, `go vet`, race tests, Windows x64 build, FlipAi desktop/background lifecycle, Google Voice receiver checks, Microsoft Defender scanning when available, installer creation, and installer install/uninstall smoke testing.