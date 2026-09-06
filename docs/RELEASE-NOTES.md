# FlipAi v0.46.49

Google Voice outbound SMS now follows Google Voice's own page controls instead of replaying the internal `sendsms` web-service request.

## What changed

- Incoming Google Voice SMS detection stays on the existing background path.
- Outgoing replies now open the exact conversation inside FlipAi's already-running hidden Google Voice WebView.
- FlipAi fills the real Google Voice message composer and triggers the page's real Send control.
- The page itself now creates the normal Google Voice network request.
- FlipAi waits for the outgoing message/composer state to confirm the send and surfaces page errors if Google Voice rejects it.
- Exact conversation and phone-number safety checks remain in place.
- The loop guard that prevents FlipAi from answering its own outgoing SMS remains unchanged.

## Background behavior

This remains fully backgrounded. FlipAi does not open a visible browser window, move the Windows mouse, type through global keyboard input, or use Windows accessibility. The interaction happens inside the existing off-screen WebView2 session, matching the browser-control approach already used by ChatGPT Chat, Claude Chat, Grok Chat, and Gemini Chat.

## Why

The previous outbound path manually reproduced Google Voice's private web-service send request. The new path lets Google Voice's own frontend perform the send, keeping the interaction much closer to the normal website workflow and removing FlipAi's custom outbound `sendsms` request construction.

## Validation

- Full Go test suite passes.
- Browser integration tests pass.
- Windows x64 build passes.
- Race tests and vet pass.
- Google Voice background-browser smoke test passes.
- Microsoft Defender scans and installer install/uninstall smoke tests pass.
- Security/CodeQL and SBOM checks pass.

No Authenticode/code-signing certificate is included in this release.
