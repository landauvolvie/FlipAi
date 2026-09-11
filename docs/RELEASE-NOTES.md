# FlipAi v0.46.75

Browser-agent reliability, clean SMS replies, and instruction-default fixes.

- Browser models now start with no FlipAi-added instructions by default. Users can still add their own custom instruction.
- ChatGPT SMS extraction uses the authored response text instead of dumping rich weather cards, charts, feedback controls, and other widget UI into SMS.
- Gemini SMS extraction excludes Gmail/action-card chrome such as To/Cc/Bcc/Edit/Cancel/Send, and normal replies such as `Yes` are submitted through Gemini's normal composer.
- Browser-agent connection tests are read-only and no longer send `Reply with exactly: FLIPAI_OK` or create test conversations in ChatGPT, Claude, Gemini, Grok, Copilot, or Muse.
- Browser thought/status/progress DOM text is no longer forwarded as user-visible progress. Long-running turns use generic progress heartbeats only.
- Previously connected browser models are restored at startup and live readiness is checked before work is accepted. Unavailable sessions fail with a reconnect message instead of pretending to be working.
- SMS execution is isolated per provider, so a stuck Grok turn cannot block ChatGPT, Gemini, Claude, Copilot, Muse, or Codex. Messages to the same provider remain ordered.
- Stalled browser turns are bounded instead of sending `still working...` indefinitely.
- Grok disconnect/profile cleanup retries while WebView2 releases its profile lock, addressing the EBWebView lockfile disconnect error.

Validation: full Linux test suite, real-browser call-flow tests, Windows tests/build, security scan, and SBOM checks passed before merge.
