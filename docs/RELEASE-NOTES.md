# FlipAi v0.46.27

Activity now reflects every current FlipAi agent instead of treating newer agents like Codex.

## Activity

- Redesigned the Activity page with a cleaner live log layout and compact message statistics.
- Added distinct agent/provider icons for ChatGPT Chat, Codex, Claude Code, Claude Chat, Gemini, and Grok.
- Added one-click filtering for all six current agents.
- Activity search now matches agent/model names, provider/company names, messages, stages, sources, and statuses.
- Incoming and outgoing message flow is labeled clearly so it is easy to see what reached an agent and what was sent back through Google Voice.
- The Activity page refreshes automatically while it is open.
- Existing metadata-only privacy behavior is preserved: SMS bodies, prompts/results, security codes, passwords, and tokens are not stored in Activity.

## Reliability

- Added regression coverage for all current agent filters, labels, and both incoming/outgoing Activity directions.
- Verified the release with the Linux real-browser call-flow suite, full Linux tests, Windows tests, vet, race tests, Windows build, desktop lifecycle checks, Google Voice checks, Defender scan, and installer smoke test.
