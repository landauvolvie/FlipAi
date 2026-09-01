# FlipAi v0.46.5

This release starts the regular ChatGPT Chat integration using the direct-backend approach first, without accessibility automation and without a hidden browser.

## ChatGPT Chat — direct backend experiment

- Adds a new **ChatGPT Chat** entry on the Agents page.
- The first published step is a safe **Probe direct backend** diagnostic.
- The probe looks for ChatGPT desktop processes, ChatGPT-owned loopback listeners, relevant local named pipes, and safe Chromium debugging metadata.
- It does **not** move the mouse, focus the ChatGPT window, type into the UI, inspect the accessibility tree, or open a hidden ChatGPT browser.
- It does **not** read or store ChatGPT cookies, bearer tokens, Local Storage, or full process command lines.
- Results are written to Activity so the real machine can tell us which local transport to implement next.
- ChatGPT Chat is intentionally **not an SMS destination yet**. The direct protocol must be proven on the installed desktop app before routing real messages through it.

## Why this is staged

Codex has a supported backend interface, while regular ChatGPT Chat does not currently expose the same documented CLI/app-server contract. Shipping the discovery probe first lets FlipAi identify a stable background path on the real Windows app without falling back to fragile UI clicking.

## Verified

The release keeps the normal FlipAi CI coverage: Linux and Windows tests, `go vet`, race tests, Windows x64 build, desktop/background lifecycle checks, Google Voice receiver validation, installer build, install/uninstall smoke test, Microsoft Defender scan when available, and SHA-256 generation.
