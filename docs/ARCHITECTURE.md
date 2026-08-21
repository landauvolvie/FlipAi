# Architecture

## Process model

One signed/unsigned `AISMSBridge.exe` binary runs in three internal modes:

1. **Launcher**: double-click entry point; ensures the watchdog is alive, then opens the localhost settings page.
2. **Watchdog**: hidden process started at Windows user sign-in; owns the host lifecycle and restarts it after unexpected exit with bounded exponential backoff.
3. **Host**: hidden background process that owns Gmail polling, the local setup/status HTTP server, and the Codex/Claude connectors.

Closing the browser UI has no effect on the watchdog or host. Explicit Quit writes a local stop flag; the host observes the flag and exits, then the watchdog exits without restarting it.

## Message flow

1. Google Voice forwards a received SMS notification to Gmail.
2. Host polls Gmail using the user-selected backend: **App Password → TLS IMAP/SMTP**, or **OAuth → Gmail API (`gmail.readonly` + `gmail.send`)**. New installs have no default backend.
3. Host validates the Google Voice sender, Google DKIM result, configured phone number, and SMS security code.
4. The message is checkpointed before execution.
5. `C:` routes to Codex App Server over local stdio JSON-RPC; `A:` routes to Claude Code print mode.
6. Persistent Codex thread ID / Claude session ID provide conversational continuity.
7. The agent performs the task using the tools actually available to that local agent runtime.
8. The agent may send a short Google Voice browser reply if it really has an authenticated browser tool. Otherwise the bridge sends the short final result through Gmail to the validated `@txt.voice.google.com` Reply-To address.

No public inbound port is required.
