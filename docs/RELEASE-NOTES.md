# FlipAi v0.37.0

This release fixes another real-PC failure in Codex generated-image replies over Google Voice.

## What the live test proved

Codex successfully generated the picture and returned its text caption, but the phone received only the caption. There was no Google Voice MMS error notice. That means the MMS sender was never reached: FlipAi had failed to obtain the generated image asset before delivery.

## What changed

FlipAi no longer depends only on the standalone Codex `item/completed` image event or on guessing where Codex wrote a file.

When the Codex turn completes, FlipAi now reads the `imageGeneration` item directly from that same live `turn/completed` notification. The completed turn contains the exact items that produced the final answer, so the image is captured before the thread is released and before the reply is sent.

The decoder accepts both `imageGeneration` and the older `image_generation` spelling, raw base64 and `data:image/...` values, plus `savedPath`/`saved_path` file fallbacks. The existing persisted-thread and filesystem readers remain compatibility fallbacks.

This does not ask Codex to save anything, does not run a second prompt, and does not generate the picture twice. It only copies the already-generated image bytes from Codex's completed turn.

## Better live diagnosis

An image request can no longer silently degrade to a normal text caption. If FlipAi still cannot obtain an image asset, the SMS now explicitly says:

`FlipAi could not locate the generated image asset.`

If the image is found but Google Voice itself cannot send the MMS, the existing message remains:

`FlipAi could not deliver the image through Google Voice MMS.`

Those two messages distinguish extraction failure from Google Voice delivery failure on the next real-phone test.

## Verification

The automated tests cover raw base64 images, data URLs, legacy image-generation item naming, text-only turns, and the actual Codex notification routing path that must retain the image for reply delivery. The normal Windows build, Google Voice/WebView2 checks, installer smoke test, and browser call-flow suite run before the release is published.
