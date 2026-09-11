# FlipAi v0.46.77

Gemini/Grok reply reliability and long Google Voice reply delivery fixes.

- Gemini now waits for streaming output to settle before treating a browser reply as complete, preventing partial SMS answers.
- Gemini collects the complete authored response across sibling prose blocks instead of stopping at an early fragment.
- Grok restores its response fallback with strict user-bubble, composer, form, and prompt-echo guards so real replies are found without texting the user's prompt back.
- Grok turns that accepted a prompt but never expose generation activity or assistant output now fail after a bounded interval instead of sending extended working heartbeats.
- Direct Google Voice text delivery now splits replies into sequential messages of at most 1,500 Unicode characters. For example, a 2,000-character reply is sent as 1,500 characters followed by 500 characters.
- Long Google Voice replies keep their original text with no `1/2` or `2/2` labels, preserve Unicode safely, stay in order, and stop if a chunk fails instead of skipping ahead.
- Regression coverage includes Gemini streaming completion, Grok reply fallback, Google Voice chunk boundaries, Unicode preservation, ordering, and failure handling.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
