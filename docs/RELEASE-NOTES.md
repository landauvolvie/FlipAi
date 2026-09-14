# FlipAi v0.46.82

Fixes a deadlock introduced in v0.46.81 that stopped every browser agent.

v0.46.81 began serializing DevTools calls on each background browser, to stop a
leftover page sampler colliding with the next turn's page driver. It took that
lock around the whole of `Call` — but `Call` itself issues further DevTools
calls on either side of the one it is making: the attachment upload before a
turn, and the returned-media scan after it. A Go mutex is not reentrant, so the
first browser turn on each provider re-entered the lock it already held.

That wedged the browser worker permanently. The page had already run the prompt
and the model had already answered, but the control channel never came back and
every later call on that browser blocked behind it forever — which is exactly
"the model gets the message and replies, but FlipAi does not send it back", on
ChatGPT Chat, Claude Chat, Grok Chat and Gemini Chat at once, with Microsoft
Copilot Chat and Muse never reaching their model because their worker was
already wedged by an earlier readiness probe.

- The lock now covers exactly one protocol call and nothing else. Everything
  `Call` does around it — the attachment upload, the media scan, starting a
  long-turn watcher — runs unlocked, so it can never re-enter.
- Waiting for the channel counts against the waiting call's own deadline. A
  short page probe issued while a 90-second turn holds the channel reports that
  it did not answer, instead of parking a goroutine until the turn ends; at one
  probe every 250ms a readiness poll would otherwise stack hundreds of them
  behind a single turn.
- Regression coverage fails if the lock is ever taken around `Call`'s body
  again, naming the nested calls that make it a deadlock, and separately
  asserts the single protocol call is still serialized.

Everything in v0.46.81 is retained: the empty-reply image trap, the request
budget that outlasts the worker's own, collecting an answer after a host-side
deadline, and an explicit prefix never being answered by another agent.

Validation: publication is gated by the release workflow's full Linux test suite, real-browser call-flow tests, Windows tests, vet, race tests, Windows build, Google Voice integration, installer install/uninstall smoke tests, Microsoft Defender checks, provenance, checksum, and CycloneDX SBOM generation.
