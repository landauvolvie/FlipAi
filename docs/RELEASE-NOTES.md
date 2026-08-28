# FlipAi v0.46.0

This release fixes the two low-level Windows failures captured by the v0.45 field diagnostic: ChatGPT exposed the correct **Start voice chat** control but never entered live Voice, and Windows refused FlipAi's per-app cable route with `HRESULT 0x80070057`.

## Verified live-Voice activation ladder

FlipAi no longer treats a successful Win32 mouse command as proof that ChatGPT handled the Voice button. It finds the same real live-Voice control, then tries distinct activation mechanisms in a controlled order: Windows UI Automation Invoke, focused keyboard Enter, legacy accessibility default action, and finally a real pointer press.

After every activation attempt, FlipAi reads ChatGPT's accessibility state again. It only succeeds when live Voice is actually active. If one method is accepted by Windows but ignored by Chromium, FlipAi advances to the next method instead of spending the whole call believing the button was clicked.

Dictation/microphone controls and conversation titles remain excluded from the live-Voice matcher.

## Electron audio-session routing

The v0.45 HSTRING/AudioPolicyConfig correction matched the current EarTrumpet interface, but the field test showed the remaining `0x80070057` failure on the ChatGPT window process. Electron applications can create their audio stream in a child/utility process rather than the PID that owns the top-level window.

v0.46 applies the persisted playback and recording endpoints across the live ChatGPT/Codex Electron process tree, while retaining the current Windows AudioPolicyConfig interface, native HSTRING ABI, SWD endpoint form and raw-MMDevice compatibility fallback.

FlipAi routes once before live Voice starts and **routes again immediately after live Voice is confirmed active**, when Electron's audio process definitely exists. That second pass is specifically for the caller-to-agent virtual cable path.

## Call behavior

The complete call path remains automatic: an authorized Google Voice call is answered, the selected desktop voice frontend is found or launched, the virtual audio route is applied, the real live-Voice control is activated and verified, and the session is torn down when the call ends. No manual Voice button press is part of the design.

## Verified

Before the version bump, this code passed the real-browser Google Voice call-flow harness, full Linux suite, Windows tests, `go vet`, race tests, Windows x64 build, Google Voice receiver check, Microsoft Defender scan, installer build, installer install/uninstall smoke test and SHA-256 generation. The v0.46 release workflow repeats the release gates before publishing.
