# FlipAi v0.46.96

v0.46.95 broke ChatGPT and Muse. This undoes the cause and puts a limit on that whole class of mistake.

- **The card rule removed the message, not the cards.** It deleted anything that *contained* a picture and a link — and every wrapper around an answer does. ChatGPT texted the page's own "ChatGPT is AI and can make mistakes" footer because the real message had been emptied, and on the next turn produced nothing at all. Only the innermost such element is a card now, and only when it holds no prose.
- **Cleaning can no longer cost the message.** Every strip is a guess about which parts of a page are furniture. If a strip removes most of what the model wrote, the guess was wrong, and the untouched text is sent instead. This bounds the damage from every rule of this kind, not just the one that went wrong.
- **The page's own disclaimer is never an answer**, and a candidate found by the structural scan must be inside the conversation when FlipAi can see where the conversation is. The line under the composer is not.
- **Nothing already on screen before the prompt can be the answer.** What was visible is now recorded by both routes FlipAi uses to read a page. Recording only the named messages meant that when a page stopped matching those mid-turn and the structural scan took over, furniture that had been there all along looked brand new.
- The driver harness grew to forty-nine scenarios, including an answer wrapped the way a real one is — bullets inside the containers a chat app puts around every message, with a picture and a link in the wrapper.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
