# FlipAi v0.46.52

FlipAi now routes SMS shortcuts to the exact AI experience requested instead of treating every provider shortcut as a generic chat alias.

## What changed

- Added provider-grouped SMS shortcuts: `O:` for ChatGPT Chat, `OW:` for ChatGPT Work, `OC:` for Codex, `A:` for Claude Chat, `AW:` for Claude Cowork, `AC:` for Claude Code Web, `AL:` for local Claude Code, `G:` for Gemini, `M:` for Microsoft Copilot, and `X:` for Grok.
- ChatGPT Work and Claude Cowork are actively selected and verified in their signed-in browser sessions before FlipAi submits the task. If the requested mode cannot be verified, FlipAi fails safely instead of silently sending the task to regular chat.
- Claude Code Web opens the dedicated Claude Code web workflow and requires a repository-ready state before starting the task; FlipAi does not guess a repository or fall back to Claude Chat.
- Codex, Gemini, Microsoft Copilot, Grok, Claude Chat, and local Claude Code continue to use their existing dedicated execution paths under the new shortcut scheme.
- Browser-mode selection is preserved for incoming messages that include attachments, preventing Work or Cowork requests with files from falling back to ordinary chat.
- Existing installations retain permission-aware compatibility for older configured shortcuts while the new shortcut destination takes precedence when it is available.

## Validation

The release pipeline runs the full Linux and Windows Go test suites, real-browser flow tests, vet and race tests, the Windows x64 build, Google Voice background smoke tests, Microsoft Defender scans, installer install/uninstall smoke tests, SBOM generation, checksums, and build provenance before publishing the release.

No Authenticode/code-signing certificate is included in this release.
