# Architecture

## Process model

One `FlipAi.exe` binary runs in several internal modes:

1. **Launcher**: double-click entry point; ensures the watchdog is alive, then opens the FlipAi desktop window (a WebView2 frame over the loopback control server).
2. **Watchdog**: hidden process started at Windows user sign-in; owns the host lifecycle and restarts it after unexpected exit with bounded exponential backoff.
3. **Host**: hidden background process that owns Gmail polling, the local control server that serves the desktop UI, and the Codex/Claude connectors.
4. **Tray**: notification-area icon for reopening the window and quitting.

Closing the window has no effect on the watchdog or host while "Close to tray" is on (the default); with it off, closing the window requests a full stop. Explicit Quit writes a local stop flag; the host observes the flag and exits, then the watchdog exits without restarting it.

## Message flow

1. Google Voice forwards a received SMS notification to Gmail.
2. Host polls Gmail using the user-selected backend: **App Password → TLS IMAP/SMTP**, or **OAuth → Gmail API (`gmail.readonly` + `gmail.send`)**. New installs have no default backend.
3. Host validates the Google Voice sender, Google DKIM result, configured phone number, and SMS security code.
4. The message is checkpointed before execution.
5. `C:` routes to Codex App Server over local stdio JSON-RPC; `A:` routes to Claude Code print mode.
6. Persistent Codex thread ID / Claude session ID provide conversational continuity.
7. The agent performs the task using the tools actually available to that local agent runtime.
8. The bridge itself sends the final result through Gmail to the validated `@txt.voice.google.com` Reply-To address, splitting a long answer into numbered texts. The agent is never asked to deliver anything, so nothing in its output can redirect a reply.

No public inbound port is required.

## Local control surface

The host serves the desktop UI on `127.0.0.1` only, behind a session cookie
holding the local token FlipAi passes when it opens its own window. Pages are
GET; every state change is a POST. See [UI-DESIGN.md](UI-DESIGN.md) for the page
structure. Pausing is handled inside the running bridge, so it takes effect on
the next mailbox check without a restart; settings that change how Gmail or the
agents are connected restart the host, which the watchdog brings straight back.

## Startup modes

Two independent options decide when FlipAi runs:

1. **At sign-in** — an `HKCU\...\Run` value, written by the installer or the
   Settings toggle. No administrator rights, no service, no scheduled task.
2. **Before sign-in** — a Task Scheduler entry with a boot trigger, created by
   an elevated `FlipAi.exe --boot-task install` helper that the Settings toggle
   launches with the `runas` verb. This is the only administrator prompt in the
   product, and it never appears during installation. The task runs as the same
   account with S4U logon, so credentials are re-protected in DPAPI machine
   scope while it is on (see the threat model).

## Updates

The host checks the GitHub release feed shortly after start and every 12 hours,
storing the result in `state.json`. When a newer version exists the window shows
a banner; installing downloads the published Setup EXE, verifies its SHA-256
against the published checksum file, and runs it silently. The installer detects
the existing install, skips every setup question, keeps the chosen startup
entry, and starts the bridge again on the new version.
