# FlipAi v0.46.92

Two things that were on the page but were never the answer got texted as one.

- **A saved task in ChatGPT's sidebar was sent as the reply to "hi my friend".** "Let me know when a new Dell with Intel, 32 GB RAM, touchscreen, and built-in 5G appears" is on the page, and the structural scan that reads a conversation FlipAi does not recognize by name treated it as a message. That scan now reads the conversation — `<main>` when the page has one — and never a sidebar, a header, a footer, a dialog or a suggestion chip.
- **"Museis working" was sent instead of Muse's answer.** That is the page's own busy line, and the two elements it is built from read as one run the name into the next word. A line like that is now recognized by its verb rather than by what stands in front of it, so it is never mistaken for a reply, whichever provider shows it.
- **A short reply has to hold still for three seconds, not one.** A status the page shows while it works sits unchanged for exactly about a second, which is all the old rule asked for. An answer of any length still goes out; a short one just has to settle properly first.
- **ChatGPT could text you your own message back.** Its last-resort branch took the newest block on screen without checking that the block was not the prompt itself. It checks now.
- The driver harness grew to thirty-seven scenarios. Two of them are these bugs: a sidebar item that updates a few times and then sits still while the answer is still being written, and a busy line built from two elements. Both reproduce the exact text that reached the phone — the Dell line and "Museis working" — against the previous drivers.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
