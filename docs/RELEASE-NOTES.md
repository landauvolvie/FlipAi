# FlipAi v0.45.0

This release completes the automatic Google Voice-to-desktop live Voice handoff after the v0.44 field test.

## Live Voice starts the correct control

When an allowed caller rings the Google Voice number, FlipAi now completes the whole sequence without requiring any manual button press:

1. answer the authorized Google Voice call;
2. find or launch the ChatGPT/Codex desktop voice frontend;
3. route the app to the virtual audio cables;
4. locate the real **Start new voice chat** control;
5. start live Voice;
6. verify that live Voice is actually active before the call is marked bridged.

The field test showed that ChatGPT can expose ordinary text dictation/microphone controls alongside the real live Voice control. The earlier matcher could select one of those first, which opened chat but did not start a spoken conversation.

v0.45 ranks all candidates and gives **Start new voice chat** highest priority. Voice Mode, live-voice and headphone/headset controls are accepted as compatible live-Voice labels. Generic microphone, mic, dictation and standalone `voice` matches are no longer accepted as live Voice.

## Automatic Electron accessibility recovery

If an already-running ChatGPT/Codex desktop app exposes only native window chrome — for example `ChatGPT, Minimize, Maximize, Close` — FlipAi automatically closes that inaccessible Electron process tree, restarts the same app with `--force-renderer-accessibility`, reacquires its window, reapplies audio routing, and then starts live Voice.

This recovery is limited to the title-bar-only accessibility signature and to one attempt per call. Conversation titles such as `Voice Chat Topic Summary` remain excluded from the clickable Voice-control matcher.

## Windows virtual-audio routing fix

The same field test exposed a Windows audio-policy interop mismatch. The per-app routing API expects the endpoint device ID as a native HSTRING handle. FlipAi was marshaling the Set argument as a CLR string, which some Windows builds reject even when the AudioPolicyConfig factory itself is available.

v0.45 uses the native HSTRING ABI, creates and releases the HSTRING explicitly, persists the Console and Multimedia roles used by Windows/EarTrumpet, and attempts the Communications role as best effort for voice apps. FlipAi still tries both the SWD endpoint path and raw MMDevice ID for Windows-version compatibility.

## Call end

When the Google Voice call ends, FlipAi exits the desktop voice session and resets the bridge state for the next caller.

## Verified

The release pipeline covers the real-browser Google Voice call-flow harness, the full Linux suite, Windows tests, `go vet`, race tests, Windows x64 build, Google Voice receiver checks, Microsoft Defender scanning when available, installer creation, installer install/uninstall smoke testing, SHA-256 generation, and release publishing.