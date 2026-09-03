# FlipAi v0.46.19

This release adds regular Claude Chat as a separate browser-backed FlipAi agent, alongside Claude Code and regular ChatGPT Chat.

## Claude Chat agent

Claude Chat signs into claude.ai through its own persistent FlipAi WebView2 profile. It has Connect, Test, and Disconnect controls, an independent H: SMS shortcut, sticky follow-up routing, NEW conversation support, its own allowed-phone list and security code, and the same receipt/progress controls as the other agents. Claude Code remains a separate agent and is not replaced.

Existing phone-number permissions are not silently copied into Claude Chat. Add a number under Claude Chat before H: can receive texts. This keeps Claude Chat authorization independent from Codex, Claude Code, and ChatGPT Chat.

## RAM and lifecycle protection

Claude Chat uses one process-level WebView2 owner protected by a Windows named mutex, cheap worker liveness checks, a persistent browser profile, controlled restart behavior, and explicit shutdown/update cleanup. A slow Claude renderer therefore cannot cause FlipAi to spawn repeated hidden browser trees and consume unbounded RAM.

## Verification

The release is gated by the normal Linux and Windows Go tests, vet/race checks, Windows x64 build, Google Voice embedded-browser checks, installer build/smoke test, Defender scan when available, and the release workflow before the installer is published.
