# FlipAi v0.46.63

This release improves long-running SMS turns across every supported model and fixes ChatGPT final-reply delivery.

## Long-running model turns

- Fast requests that finish within 30 seconds send only the final answer.
- If a model is still working after 30 seconds, FlipAi sends the first short acknowledgement so the sender knows the request was received.
- Existing periodic progress updates remain available for genuinely long tasks.
- The behavior is shared across Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, Grok Chat, and Microsoft Copilot Chat.
- Long tasks continue running until the provider finishes or actually fails; progress messages never replace the final result.

## ChatGPT final reply

- Removed the internal `ChatGPT completed the turn.` fallback from SMS delivery.
- FlipAi now waits for real, non-empty assistant text before treating the ChatGPT turn as successfully complete.
- This preserves the long-turn continuation path while ensuring the final model response is what reaches Google Voice.

## Validation

- Added regression coverage for the universal 30-second acknowledgement.
- Added regression coverage preventing ChatGPT completion-status text from being used as the final reply.
- The normal release pipeline runs Linux and Windows tests, real-browser call-flow checks, vet/race checks, Google Voice checks, Microsoft Defender scans, installer smoke tests, SBOM generation, checksums, and provenance attestation before publishing.

No Authenticode/code-signing certificate is included in this release.
