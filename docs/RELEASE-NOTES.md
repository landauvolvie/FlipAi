# FlipAi v0.46.50

Microsoft Copilot Chat is now available as a full browser-backed SMS agent in FlipAi.

## What changed

- Added Microsoft Copilot Chat as the seventh agent with `P:` SMS routing and sticky follow-up routing.
- The first Connect opens a normal Microsoft Copilot window so you can sign in once.
- After setup, FlipAi keeps Copilot in its own persistent WebView2 profile and runs it off-screen in the background.
- Normal turns fill Copilot's real prompt box and trigger its real Send control. FlipAi does not use a Copilot API, global mouse/keyboard input, Windows accessibility, or an external browser.
- Added NEW conversation support, per-agent allowed numbers and security code, acknowledgements/progress settings, the shared SMS instruction, connection status/test/disconnect, and image attachment support.
- Existing ChatGPT Chat, Claude Chat, Gemini Chat, Grok Chat, Codex, Claude Code, Google Voice SMS, and Google Voice calling behavior remain unchanged.

## Background behavior

Only initial Microsoft sign-in is visible. Routine Copilot operation stays hidden/off-screen and is supervised from FlipAi's signed-in tray session, matching the background architecture used by the other browser-backed agents.

## Validation

The release pipeline runs the full Linux and Windows Go test suites, the real-browser call-flow tests, vet and race tests, the Windows build, Google Voice background smoke tests, Microsoft Defender scans, installer install/uninstall smoke tests, SBOM generation, and build provenance before publishing the release.

No Authenticode/code-signing certificate is included in this release.
