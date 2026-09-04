# FlipAi v0.46.29

Agent symbols now match the correct product everywhere FlipAi identifies an agent.

## Agent symbols

- ChatGPT Chat now uses the OpenAI / ChatGPT knot instead of the Codex symbol.
- Codex now uses the blue-purple Codex terminal-cloud symbol.
- Claude Code now uses the orange Claude Code pixel-bot symbol.
- Claude Chat now uses the Claude orange star symbol.
- Gemini Chat now uses the multicolor Gemini star instead of the generic Google G.
- Grok Chat now uses the Grok orbital symbol instead of a generic X.
- The corrected symbols are shared across the Agents page, Activity filters, Activity rows, and other agent-brand UI so the same agent is represented consistently everywhere.

## Reliability

- Added regression coverage for all six current agent-to-symbol mappings.
- Preserved Activity filtering, message logging, updater behavior, and agent execution logic unchanged.
