# FlipAi v0.46.79

Texts reach the selected agent again after a cold start.

- Starting SMS processing is supervised instead of attempted once. FlipAi used to ask its Google Voice background browser a single time, about a second after the host started, whether it was connected. That browser restores its saved session at the same moment every connected agent browser restores its own, so after a Windows restart the answer was routinely "not yet" and FlipAi gave up for the whole run: the Google Voice listener came up a minute later and went on receiving and authorizing texts while no bridge existed to hand any of them to an agent. Every text was detected and then silently never answered — by the browser-backed agents and by local Codex/Claude alike — until FlipAi was restarted by hand.
- FlipAi now re-checks the transport every ten seconds until it is usable, starts SMS processing the moment it is, records each distinct reason it is still waiting once in the Activity log, and calls out a transport that never arrives instead of leaving a silent install.
- Connecting Google Voice while FlipAi is already running now takes effect without a restart. The connection was written to the config but the running host kept the transport it started with, so a first connection could not carry a text.
- A paused FlipAi says so on each text it receives. The listener keeps running while paused, so the Activity log showed a text arriving and its sender being allowed and then nothing at all, which reads exactly like a broken bridge. The pause itself is unchanged: the text is held, not dropped.
- A message that cannot be read is given up on after five minutes instead of being re-fetched every second for as long as FlipAi runs. One support export carried 5,361 copies of a single message's failure, which buried every other event in the log.
- Regression coverage verifies the supervised start, post-startup connection adoption, the paused notice, and the bounded unreadable-message retry.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
