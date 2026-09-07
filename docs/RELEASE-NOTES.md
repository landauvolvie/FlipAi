# FlipAi v0.46.58

This release makes long-running AI tasks reliable across FlipAi's browser-chat integrations and keeps the user informed while a model is still working.

## Long-running model tasks

- The old 90-second browser response limit is now a checkpoint, not a failure.
- ChatGPT/ChatGPT Work, Gemini, Claude Chat, Grok, and Microsoft Copilot can continue working for as long as the provider itself remains active.
- FlipAi no longer imposes an elapsed-time hard cap on agent/model turns. A task ends when the model completes, the provider reports a real failure, or FlipAi is actually shutting down.
- The original prompt is not resent when a task crosses the checkpoint; FlipAi continues monitoring the same in-progress turn.

## Live progress updates

- FlipAi can capture concise visible provider statuses such as “Thinking…”, “Generating…”, “Searching…”, or “Finishing…” and surface them through the existing progress-message path.
- Long or reasoning-like text is deliberately filtered instead of being forwarded as progress, so progress updates stay short and do not expose detailed internal reasoning.
- When no safe provider status is available, the normal generic “still working” heartbeat remains the fallback.

## Generated images and media

- Generated-image work is no longer abandoned simply because several minutes have elapsed while the provider is still active.
- Temporary replies such as “Creating your image” remain non-final; FlipAi continues watching for the actual generated image and preserves it for Google Voice MMS delivery.
- The returned-media path continues to support ChatGPT, Gemini, Claude, Grok, and Microsoft Copilot browser chats.

## Reply instruction

- The default reply hint remains exactly: `Please keep your reply short and to the point.`
- The prompt no longer adds SMS/plain-text wording that can confuse a model about what task it is being asked to perform.

## Validation

- Added regression coverage to prevent a hard agent-turn timeout from being reintroduced.
- Added tests for long-turn provider detection, progress filtering, explicit failure handling, and Windows browser continuation behavior.
- The release pipeline runs Linux and Windows tests, real-browser call-flow checks, vet/race checks, Google Voice checks, Microsoft Defender scans, installer install/uninstall smoke tests, CycloneDX SBOM generation, SHA-256 checksums, and provenance attestation before publishing.

No Authenticode/code-signing certificate is included in this release.
