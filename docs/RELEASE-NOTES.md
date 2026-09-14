# FlipAi v0.46.87

Reading the page stopped costing more than the turn had.

- **The structural fallback added in v0.46.84 was far too expensive, and it regressed everything that used it.** Every poll rescanned the whole page for shadow roots several times over, cloned every candidate node to read its text, and compared every block against every other to find containers. On a real conversation that costs more than the quarter-second poll it runs in, so ChatGPT's turn could not finish inside its own ninety seconds and was abandoned at ninety-five. The scan is now linear, the shadow-root walk is done once, and the careful text read is reserved for the one reply FlipAi actually sends.
- **Muse sent the whole conversation.** A scroll container's text is every message joined together, and it was being treated as a single block. A container is no longer a candidate at all, and a matched block is lifted to the message the conversation holds directly — so the whole answer travels, and nothing older than it does.
- **Source cards are no longer part of the answer.** "CBS News", "www.thephoto-news.com", "Show all" are references the page renders under a reply, not sentences the model wrote.
- **Two-part replies are paced three seconds apart, not one.** FlipAi sends the parts in order and waits for each to be confirmed, but two texts handed to the carrier a second apart can still reach the phone in the other order. A wider gap is the only lever FlipAi has over that without numbering the parts and rewriting the model's answer.
- The driver harness grew from six scenarios to twenty-one: a conversation of realistic size, an answer followed by source cards, an interim status before the answer, and a reply with an action row under it, across all three drivers. The source-card and history scenarios both failed against the v0.46.86 drivers.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
