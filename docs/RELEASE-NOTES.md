# FlipAi v0.46.2

This release allows the same phone number to be authorized on both Codex and Claude.

## Shared phone numbers

A number can now appear in both agents' Allowed phone numbers lists. Each agent still keeps its own access setting (Texts and calls, Texts only, or Calls only), security code, workspace, conversation, and reply behavior.

When a shared number is allowed to text both agents, the configured SMS shortcuts choose the destination: `C:` routes to Codex and `A:` routes to Claude by default. If the message is unprefixed, FlipAi sends it to the configured default agent. A per-agent security code may still come first, for example `mycode A: check this`.

Sharing a number does not widen permissions. If the number is Texts only on Codex and Calls only on Claude, an `A:` text remains blocked because Claude was not granted SMS access.

Phone calls do not contain an SMS shortcut. If the same caller is allowed to call both agents, the configured default agent receives the call. If only one agent grants call access, that agent receives it.

## Fixed

The Agents page no longer rejects a number merely because it is already present on the other agent. Shared numbers also survive restart/config recovery instead of being silently removed from the second agent.

## Verified

The release workflow runs the real-browser Google Voice call-flow harness, full Linux and Windows test suites, `go vet`, race tests, Windows x64 build, Google Voice receiver validation, installer build, install/uninstall smoke test, Microsoft Defender scan when available, and SHA-256 generation before publishing.
