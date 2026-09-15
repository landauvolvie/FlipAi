# FlipAi v0.46.91

A turn script could outlast the deadline it was given, and a turn that used tools was texted before it had answered.

- **ChatGPT failed at exactly 95.4 seconds, with the answer finished in the page.** A turn script waited up to twenty seconds for the composer and only then started a fresh ninety-second deadline — a hundred and ten seconds against the ninety-five a turn is allowed, so the call was abandoned mid-turn. Every driver now fixes one budget when it starts and holds every later wait inside it. A test adds a script's waits up rather than taking the largest, which is what hid this.
- **Claude texted its opening line and nothing else.** "This will take a few minutes — pulling calendar and email first" sat unchanged for a second while Claude was still running tools, and that looked finished. A turn is now judged settled by the whole turn's text, not by one block of it, and a turn that is using tools has to be quiet for five seconds rather than one. The reply carries the model's prose — the opening line and the answer both.
- **Connector offers and file tiles are no longer part of the message.** "Connectors that could help · Microsoft 365 · Slack" and the "Morning brief · Code · HTML · Download" tile are controls with words on them, not sentences. Neither is the running tool log. All four reply-reading drivers strip them now.
- **A driver's deadline message is a contract again.** Rewording those messages this release would have stopped FlipAi recognizing a turn's checkpoint, so a model still writing its answer would have been reported as failed. All twelve of them are now held to that contract by a test.
- The driver harness grew to thirty-one scenarios, including a turn that says what it is about to do, runs tools for several seconds, offers connectors, and only then answers and attaches a file.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
