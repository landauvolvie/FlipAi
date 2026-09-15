# FlipAi v0.46.97

v0.46.96 shipped a ChatGPT driver that could not run. This fixes it and adds the check that would have caught it.

- **ChatGPT failed every turn with `ReferenceError: pageFurnitureText is not defined`.** A guard was added to one branch of the turn, and the function it calls was left out of the file — the edit that would have added it never got written. Nothing caught it: the script parses fine, and the real-browser harness never reaches the branch that calls it. The function is back, and page scripts are now checked for anything they call and never declare, so this cannot ship again. Removing the function makes the new check fail with exactly the message the user's log carried.
- **Muse took nine seconds to type the prompt, against under one before.** Recording what was on screen before a turn started scanning the page twice, and the scan is the expensive thing these scripts do — it is asked for several times in a single poll. It now runs at most once a quarter second. That nine seconds came straight out of the turn's own budget.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
