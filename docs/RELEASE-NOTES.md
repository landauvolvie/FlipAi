# FlipAi v0.46.51

Google Voice media now travels through FlipAi's direct browser-backed SMS connection instead of depending on Gmail forwarding.

## What changed

- Incoming Google Voice MMS photos, voice/audio clips, and supported videos are captured from the signed-in Google Voice WebView as the actual file and attached directly to supported browser-chat agents. FlipAi does not transcribe the media and does not give the model a download link instead of the file.
- ChatGPT Chat, Claude Chat, Gemini Chat, Grok Chat, and Microsoft Copilot Chat now support the same direct inbound media handoff through their existing hidden WebView2 sessions.
- When a browser model returns an image, FlipAi captures the returned image and attempts to send it back as a real Google Voice MMS. Protected generated images can also be captured from the rendered browser content when their CDN URL cannot be fetched directly.
- Returned video, audio, files, oversized/unsupported media, or an image Google Voice cannot deliver fall back to the exact model conversation URL so the result is still reachable from the phone.
- Removed the Gmail / Google Voice connection controls from the visible app. The legacy Gmail implementation remains in the repository and is preserved on `archive/gmail-voice-bridge-v0.46.50` for rollback.
- FlipAi now starts its background bridge hidden automatically when the Windows user signs in, without requiring the main window to be opened. The optional boot setting starts the host earlier at Windows boot.
- Improved recovery of the direct Google Voice SMS background worker after reboot so a tray-running FlipAi does not remain idle until the main window is opened.

## Background behavior

Normal Google Voice and browser-agent operation remains off-screen. A visible browser window is used only when an account needs first-time sign-in or reconnection. Browser-backed agents still require an interactive signed-in Windows desktop session because WebView2 cannot run those signed-in browser sessions before any Windows user has logged in.

## Validation

The release pipeline runs the full Linux and Windows Go test suites, real-browser flow tests, vet and race tests, the Windows x64 build, Google Voice background smoke tests, Microsoft Defender scans, installer install/uninstall smoke tests, SBOM generation, checksums, and build provenance before publishing the release.

No Authenticode/code-signing certificate is included in this release.
