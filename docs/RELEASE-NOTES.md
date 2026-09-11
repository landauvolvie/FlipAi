# FlipAi v0.46.78

Grok stale-turn cancellation and complete long Google Voice reply delivery fixes.

- Grok SMS turns are now bound to the exact browser worker that accepted them. If Grok disconnects or its WebView restarts mid-turn, FlipAi cancels the stale request instead of continuing to send `Grok Chat still working…` indefinitely.
- Direct Google Voice delivery no longer passes through the older bridge-level numbered-part cap before the 1,500-character transport splitter, so the full model answer reaches the phone even when it needs more than four SMS chunks.
- Consecutive Google Voice chunks are paced by one second after each confirmed send to avoid later chunks disappearing when the page accepts rapid back-to-back sends too quickly.
- Existing Gmail/IMAP reply behavior remains unchanged.
- Regression coverage verifies stale Grok worker cancellation and full direct-Google-Voice delivery without legacy truncation.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
