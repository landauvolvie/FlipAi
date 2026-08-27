# Security Policy

AI SMS Bridge is a remote-control bridge. A valid SMS can cause an AI agent to act with the permissions of the Windows user running it.

## Protections

- No administrator rights required.
- No Windows service and no public TCP listener.
- Google Voice notifications must pass Google DKIM, exact sender extraction, phone allowlist, and SMS security-code checks.
- Multiple allowed phone numbers are supported; every number is normalized and compared by exact 10-digit equality.
- The untrusted SMS body is never searched to determine the sender. When available, authorization uses the structured `@txt.voice.google.com` Google Voice envelope; the `voice-noreply@google.com` fallback accepts only a full phone number from the expected Subject header.
- The agent return prompt contains only the exact sender of that command and explicitly forbids sending the result to another allowed number.
- SMS security code is stored as a salted, iterated SHA-256 hash.
- The selected Gmail credential is local-only: OAuth tokens and App Passwords are protected with Windows DPAPI on Windows.
- App Password mode uses direct TLS connections to Gmail IMAP (`993`) and SMTP (`465`); OAuth mode uses Google's Gmail HTTPS API.
- Gmail reply fallback can send only to the exact `txt.voice.google.com` Reply-To domain parsed from the authenticated incoming message.
- Local setup UI is loopback-only and authenticated with an HttpOnly SameSite cookie seeded by a random local token.
- The live-session hook endpoint (`/claude/hook`, used only by Claude conversation mode) is loopback-only and authenticated with a separate secret minted per host run and held in memory. It is never written to disk, and a request without it is refused, so another local process cannot post a fabricated Claude reply for FlipAi to text out.
- The Google Voice process serves one loopback endpoint, used by the FlipAi host to ask it to send an image through the signed-in Google Voice session it owns. It is authenticated with a token that process generates for itself and writes beside its port in the runtime state file (user-only, `0600`); a request without the token is refused, and the token is stripped from the state the desktop UI reads. **No browser debugging port is opened**: FlipAi reaches its own Google Voice page through WebView2's in-process DevTools call, so there is no local debugging endpoint for another process on the PC to connect to.
- Codex must use ChatGPT-managed auth; API/provider auth is rejected.
- Anthropic API environment variables are removed before Claude Code is launched; API/Console auth is rejected by the setup test.
- Unexpected Codex approval requests are declined rather than automatically approved.
- Both agents run an SMS turn with the same reach: the normal permissions of the signed-in Windows user, and no elevation. Codex turns use `approvalPolicy: never` with `danger-full-access`; Claude turns use the matching `--permission-mode bypassPermissions`. This is the point of the product — a text can drive the same tools the user drives at the desktop, including Chrome — and it is why the phone allowlist and SMS security code are the real boundary. Claude's permission mode can be narrowed on the Agents page; note that anything narrower refuses the browser and other MCP tools on an unattended turn.
- State intentionally excludes prompt and result bodies.
- No telemetry, obfuscation, packing, credential scraping, browser-password extraction, keylogging, process injection, remote-thread creation, or public webhooks.
- Update checks contact only `api.github.com` for this repository's latest release. They send no identifier, no configuration, and no message data. Nothing installs on its own: FlipAi shows that a release exists and installs it only when you click Install, after checking the download against the checksum published with the release.

## Mail latency

- Gmail App Password mode uses IMAP IDLE so Gmail can notify the bridge as soon as the forwarded Google Voice message reaches the Inbox. A 30-second fallback poll protects against dropped IDLE notifications.
- Gmail API/OAuth mode checks about once per second. True Gmail API push would require the user's own Google Pub/Sub setup, which FlipAi deliberately does not require.
- The bridge cannot control the upstream delay from Google Voice to Gmail; these latency guarantees begin once Gmail receives the forwarded notification.

## Antivirus-friendly design

FlipAi is intentionally conventional Windows software: ordinary Go code, per-user HKCU startup, loopback HTTP for local settings, normal TLS network connections, and normal child processes for Codex/Claude. It does not inject code into other processes, install drivers or services, scrape browser credential stores, keylog, use packers/obfuscators, or install updates without being asked. Windows URLs are opened through `explorer.exe`, not DLL-launch helpers.

The source test suite rejects known malware-style Windows API patterns. Public releases should also be Authenticode-signed when practical. An unsigned new executable can still receive SmartScreen reputation warnings or third-party antivirus false positives; no project can guarantee otherwise.

## Reliability model

- A hidden watchdog restarts the bridge host and tray after a crash with backoff.
- Explicit Quit creates a stop flag; host, tray, and watchdog all exit and stay stopped.
- Gmail/network failures are retried.
- Gmail messages are checkpointed before agent execution to reduce duplicate execution risk.
- Codex App Server can be recreated if it dies.
- Claude is started per task, so one Claude process crash does not kill the bridge host.
- Agent turns have a hard timeout.

## Residual risks

No autonomous computer-control bridge can eliminate all risk. Remaining risks include prompt injection from content an agent reads, compromise of the Windows/Google/ChatGPT/Claude account, unsafe model/tool behavior, browser vulnerabilities, stolen SMS security code plus account/number access, denial of service, OAuth expiry, sleep/hibernate, Google Voice/Gmail delivery delays, and upstream agent bugs.

Use a non-critical test machine/account first and keep agent permissions as narrow as practical.
