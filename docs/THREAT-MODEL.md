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
- No dangerous Claude bypass-permissions flag; unresolved Codex approval requests are declined.
- Watchdog restart is local only and uses a user-writable stop flag for intentional shutdown.

## Residual risk

Remote autonomous agents remain high-impact software. Prompt injection in content an agent reads, compromise of the Windows/Google/ChatGPT/Claude account, model/tool mistakes, browser vulnerabilities, or theft of both the Google Voice/SMS path and SMS code can still cause unwanted actions. Use least privilege and test on a non-critical account first.
