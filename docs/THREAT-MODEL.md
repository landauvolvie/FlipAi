# Threat model

## Assets

- Ability to invoke Codex or Claude with the Windows user's permissions.
- Gmail OAuth refresh token.
- ChatGPT-managed Codex authentication and Claude subscription authentication.
- Files, repositories, browser sessions, MCPs/connectors, and apps reachable by either agent.

## Trust boundaries

- SMS carrier / Google Voice.
- Google Voice-generated email and Gmail API.
- Local bridge/watchdog processes.
- Codex App Server.
- Claude Code / Chrome integration.

## Primary controls

- Google-signed Voice notification requirement (DKIM pass).
- Configured source phone number.
- Independent SMS security code stored only as a salted iterated hash.
- DPAPI-protected Google OAuth token.
- Loopback-only authenticated UI.
- Checkpoint-before-execute semantics to avoid duplicate remote execution.
- Gmail reply constrained to exact `txt.voice.google.com` domain.
- Codex ChatGPT-account check and Claude non-API/Console auth check.
- Unresolved Codex approval requests are declined. Neither agent is sandboxed
  from the user's own files or tools: a Codex turn runs with
  `approvalPolicy: never` and `danger-full-access`, and a Claude turn with the
  matching `--permission-mode bypassPermissions`, so both act with the normal
  permissions of the signed-in Windows user and neither requests elevation. The
  authorization chain above — not an agent sandbox — is what stops an
  unauthorized text from reaching either agent.
- Watchdog restart is local only and uses a user-writable stop flag for intentional shutdown.

## Optional settings that change the trade-off

- **Start before sign-in.** Creating the boot-time scheduled task needs a
  one-time administrator approval, and the task runs with S4U (no stored
  password). Because that logon has no account key, FlipAi re-protects its
  stored credentials for the machine (DPAPI local-machine scope) instead of for
  the signed-in account. They stay in the same per-user folder with the same
  file permissions, but an administrator or SYSTEM process on this PC could
  decrypt them. Turning the option off re-protects them for the account again.
- **In-app updates.** The app reads this repository's release feed, downloads
  the published installer, and verifies it against the checksum published
  beside it. That defeats a tampered download, not a compromise of the release
  itself: whoever can publish a FlipAi release can publish an installer this
  check accepts. The same is true of downloading the release by hand.

- **Automatic updates.** On by default, and they shorten the window between a
  release being published and it running on this PC to the check interval. The
  checksum verification is identical to the manual path — automation changes
  *when* an install happens, not *what* is trusted — but it does mean a
  published release installs without anyone reading the notes first. Turn it
  off in Settings → Updates to keep the install a deliberate act.

## Phone calling (experimental)

Turning Google Voice calling on adds two long-lived, signed-in browser
profiles that FlipAi owns: the Google Voice window (a Google session) and the
hidden Codex voice window (a ChatGPT session), both stored under the app's
per-user data folder with the same protections as the rest of it. The audio
bridge between them is a WebRTC connection negotiated through the FlipAi
process itself and carried directly between the two browser windows on this
PC; call audio is never written to disk, never uploaded by FlipAi, and never
played through, or captured from, a physical audio device. Who may ring
through to an agent is decided by the same per-agent allowlists as SMS, an
unidentifiable caller is never answered, and both injected pages strip
camera, screen-share, geolocation and clipboard access and pin their windows
to their own sites. Signing out deletes the Google Voice browser profile.

## Residual risk

Live conversation mode adds one long-running Claude Code child process and one
loopback endpoint, both described in SECURITY.md. It also means the SMS session
has a second writer — anyone with the user's claude.ai account, through Remote
Control. FlipAi still composes and sends every SMS reply itself in Go, and only
for turns it started, so agent output cannot redirect a reply; but a session
shared with a browser is a larger surface than a per-message subprocess, which is
why per-message remains the default.

Remote autonomous agents remain high-impact software. Prompt injection in content an agent reads, compromise of the Windows/Google/ChatGPT/Claude account, model/tool mistakes, browser vulnerabilities, or theft of both the Google Voice/SMS path and SMS code can still cause unwanted actions. Use least privilege and test on a non-critical account first.
