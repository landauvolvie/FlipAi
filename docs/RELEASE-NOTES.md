# FlipAi v0.46.76

Sticky SMS routing and browser-turn submission reliability fixes.

- SMS routing now preserves the selected model until the user explicitly switches with another model prefix. For example, after `X:` all unprefixed follow-ups stay on Grok; after `G:` they stay on Gemini; after `O:` they stay on ChatGPT.
- Grok now verifies that the prompt was actually accepted by the page before FlipAi waits for or returns a reply.
- Grok no longer mistakes the user's own prompt bubble or generic response UI for the assistant's answer.
- Gemini now compares response count/content instead of DOM-node identity, so a stale Angular re-render cannot masquerade as the answer to a new SMS turn.
- Gemini now verifies that the prompt was actually accepted before treating the turn as active.
- Grok and Gemini both reject prompt echoes at the transport boundary instead of texting the submitted question back as if it were the model's answer.
- Regression coverage verifies X -> Grok, G -> Gemini, and O -> ChatGPT remain sticky across unprefixed follow-ups until an explicit switch.

Validation: full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, desktop/background lifecycle, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, security scan, and SBOM checks passed before release.
