# Security Policy

AI SMS Bridge is a remote-control bridge. A valid SMS can cause an AI agent to act with the permissions of the Windows user running it.

## Protections

- No administrator rights required.
- No Windows service and no public TCP listener.
- Google Voice notification must pass sender, Google DKIM, allowed-phone, and SMS-code checks.
- SMS security code is stored as a salted, iterated SHA-256 hash.
- The selected Gmail credential is local-only: OAuth tokens and App Passwords are protected with Windows DPAPI on Windows.
- App Password mode uses direct TLS connections to Gmail IMAP (`993`) and SMTP (`465`); OAuth mode uses Google's Gmail HTTPS API.
- Gmail reply fallback can send only to the exact `txt.voice.google.com` domain parsed from the authenticated incoming message.
- Local setup UI is loopback-only and authenticated with an HttpOnly SameSite cookie seeded by a random local token.
- Codex must use ChatGPT-managed auth; API/provider auth is rejected.
- Anthropic API environment variables are removed before Claude Code is launched; API/Console auth is rejected by the setup test.
- Unexpected Codex approval requests are declined rather than automatically approved.
- Claude dangerous permission bypass is never enabled.
- State intentionally excludes prompt and result bodies.
- No telemetry, auto-update code, obfuscation, packing, credential scraping, or public webhooks.

## Reliability model

- A hidden watchdog restarts the bridge host after a crash with exponential backoff.
- Explicit Quit creates a stop flag; the host observes it and exits, and the watchdog will not restart it.
- Gmail/network failures are retried on later polling cycles.
- Gmail messages are checkpointed before agent execution to reduce duplicate execution risk.
- Codex App Server can be recreated if it dies.
- Claude is started per task, so one Claude process crash does not kill the bridge host.
- Agent turns have a hard timeout.

## Residual risks

No autonomous computer-control bridge can eliminate all risk. Remaining risks include prompt injection from content an agent reads, compromise of the Windows/Google/ChatGPT/Claude account, unsafe model/tool behavior, browser vulnerabilities, stolen SMS security code plus account/number access, denial of service, OAuth expiry, sleep/hibernate, and upstream agent bugs.

Use a non-critical test machine/account first and keep agent permissions as narrow as practical.
