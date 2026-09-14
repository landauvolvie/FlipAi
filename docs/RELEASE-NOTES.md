# FlipAi v0.46.80

Browser agents deliver their answers again.

- A finished answer is delivered even when the provider page still looks busy. Every browser driver decided a turn was over only when no stop-like control was on the page. "Working" is inferred from the page's own buttons, so a provider that leaves such a control on screen after the answer is complete — voice mode, a stale streaming affordance, a renamed button — read as working forever. The model's answer sat finished in the browser while FlipAi texted "still working…" until the turn was abandoned. ChatGPT Chat, Claude Chat, Microsoft Copilot Chat and Muse now finish a turn once the answer has stopped changing, matching the escape Gemini and Grok already had, and the long-turn watcher applies the same rule to every provider as a backstop.
- Muse turns were never watched at all: the turn expression resolved to no provider, so the continuation never started and every Muse turn waited out its full extended window on a state file nothing was going to write.
- A page checkpoint that does not come back in time now starts the same continuation the checkpoint would have. The model is usually still answering, and its answer previously landed in the page with nothing watching for it.
- The wait for a generated image is bounded. It was entered on a turn that had already timed out, so an image that never arrived held that agent's whole SMS queue for the life of the process.
- The long-turn watcher is bounded, so a page that always reports itself as working can no longer keep a sampler running for the life of the process.
- A browser turn now waits up to 90 seconds for its background browser to finish restoring, instead of 15. Every browser agent reloads its saved session at startup, all at once, and a text queued while FlipAi was down is dispatched seconds after the bridge starts — straight into a browser that was still coming up. A disconnected agent is still refused immediately from its saved state.
- Regression coverage asserts every driver can finish a turn with a stuck stop control, that every turn expression resolves to its long-turn provider, that all browser-turn waits terminate, and that a timed-out page checkpoint still watches the page.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
