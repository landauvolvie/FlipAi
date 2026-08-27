# FlipAi v0.34.0

This release fixes the remaining Codex generated-image return path for Google Voice SMS/MMS.

## What was wrong

A live test on v0.32.0 showed the exact failure: Codex completed an image request and FlipAi returned the text caption, but no picture arrived. Because the Google Voice MMS failure notice did not appear either, the MMS sender had never been given an image asset.

The remaining problem was image discovery. FlipAi could capture a live `item/completed` `imageGeneration` event, and it also tried recent `generated_images` folders. But Codex can use a separate image save root, so a folder guess is not authoritative. On the affected machine the completed image existed in the Codex conversation while neither filesystem fallback found it.

## What changed

**The completed Codex thread is now the primary recovery source.** Live `item/completed` image capture remains first. If that event is missed, FlipAi starts a short-lived local Codex App Server reader and calls `thread/read` with the active thread id and `includeTurns: true`. It walks the newest completed turn for an `imageGeneration` item and extracts the exact base64 `result` or its `savedPath`.

This is a local history read only. It does **not** send another prompt, regenerate the image, ask Codex to save it, or consume another model/image-generation token round trip.

The configured working-directory and legacy `CODEX_HOME` scans remain compatibility fallbacks, but they are no longer the main recovery mechanism.

**The resolver now explains itself in the logs.** It records whether the image came from the live event, persisted thread history, working-directory fallback, or legacy fallback. If all sources fail, the reason from each failed path is logged so another live test cannot fail as silently as v0.32.0 did.

Once an image is resolved, delivery still uses FlipAi's Google Voice WebView2 MMS path rather than the Gmail `@txt.voice.google.com` reply gateway, because that email gateway did not forward image attachments in the live tests.

## Verified

CI covers the thread-history parser with real image bytes, including normal base64 results and data URLs, plus the existing test suite, race test, Windows build, Google Voice/WebView2 receiver smoke tests, installer build, install/uninstall, and artifact generation.

A carrier MMS can only be verified on a real Google Voice account, so the next live SMS image request remains the final end-to-end test.
