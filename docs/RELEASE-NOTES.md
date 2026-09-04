# FlipAi v0.46.30

Agent logos now render correctly and consistently throughout FlipAi.

## Agent logos

- Replaced the hand-drawn approximations with current web-sourced SVG product marks for OpenAI / ChatGPT, Codex, Claude Code, Claude, Grok, and Gemini.
- The uploaded reference screenshots are not used as application assets.
- Codex and Gemini are rendered as isolated SVG images so their gradient IDs cannot collide when the same logo appears multiple times on one page.
- Removed legacy background and recoloring rules that were turning Codex into a blank square and Gemini into a blue block.
- The same shared marks are used on the Agents page, Activity filters, and Activity rows.

## Reliability

- Added regression tests for all six agent-to-logo mappings and for isolated SVG rendering.
- Browser, Linux, Windows, race, Google Voice, Defender, installer, and uninstall smoke checks passed before this release was prepared.
