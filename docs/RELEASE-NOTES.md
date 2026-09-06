# FlipAi v0.46.54

The Agents page now matches FlipAi's public SMS routing, and every shortcut supports starting a fresh chat or session. This release includes all updater improvements from v0.46.53.

## Agents and SMS routing

- The Agents page shows the real public shortcuts: `O:` ChatGPT Chat, `OW:` ChatGPT Work, `OC:` Codex, `A:` Claude Chat, `AW:` Claude Cowork, `AC:` Claude Code Web, `AL:` Claude Code Local, `G:` Gemini, `M:` Microsoft Copilot, and `X:` Grok.
- Stale internal shortcut letters are no longer presented as the primary UI routing codes; stored legacy aliases remain compatible behind the scenes.
- Add the configured new-session word after any shortcut to start fresh. With the default word: `OW NEW: research this`, `AC NEW: fix this repository`, or `X NEW: explain this`.
- `OW NEW:` with no prompt resets only that exact destination. `OW NEW: <prompt>` resets it and runs the first task in the new session as one queued operation.
- Fresh browser sessions preserve the exact requested mode: ChatGPT Work remains Work, Claude Cowork remains Cowork, and Claude Code Web remains Code Web.
- Codex, Claude Code Local, Gemini, Microsoft Copilot, Grok, ChatGPT Chat, and Claude Chat also start their corresponding fresh conversation or session.
- Fresh turns retain normal attachment, progress, health, and reply handling.

## Validation

- Added tests for `NEW` across every public shortcut, reset-only syntax, custom new-session words, and the Agents-page shortcut map.
- The release pipeline runs the full Linux and Windows tests, real-browser checks, vet/race checks, Google Voice smoke tests, Defender scans, installer smoke tests, SBOM generation, checksums, and provenance before publishing.

No Authenticode/code-signing certificate is included in this release.
