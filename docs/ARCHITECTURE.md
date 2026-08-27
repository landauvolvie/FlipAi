# Architecture

## Process model

One `FlipAi.exe` binary runs in several internal modes:

1. **Launcher**: double-click entry point; ensures the watchdog is alive, then opens the FlipAi desktop window (a WebView2 frame over the loopback control server).
2. **Watchdog**: hidden process started at Windows user sign-in; owns the host lifecycle and restarts it after unexpected exit with bounded exponential backoff.
3. **Host**: hidden background process that owns Gmail polling, the local control server that serves the desktop UI, and the Codex/Claude connectors.
4. **Tray**: notification-area icon for reopening the window and quitting.
5. **Google Voice**: an Edge WebView2 view on `voice.google.com`, held open so
   the number stays signed in and able to ring. See the phone call flow below.

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

## Phone call flow

Google Voice is not a browser FlipAi drives from outside; it is a view FlipAi
owns. A fifth internal mode, `FlipAi.exe --google-voice`, hosts an Edge
WebView2 view on `voice.google.com` and nothing else. It is a separate process
so Google Voice stays signed in and listening with the FlipAi window closed,
and it holds a named mutex so there is exactly one of it.

That window has two states and no third:

- **docked** — a borderless tool window placed over the panel the Connections
  page reserves, owned by the FlipAi window so it travels with it, clipped to
  the part of the panel that is actually on screen;
- **parked** — moved past every display, still visible as far as Chromium is
  concerned so the page keeps running and a call still rings.

It is never restored as an ordinary window, never given a title bar, and never
claims a taskbar button or an Alt-Tab entry.

One call at a time is owned by the state machine in `voice_session.go`:

1. Something sees a ring. Two things can: the script FlipAi injects into the
   page, which reacts to the DOM change carrying the ring, and FlipAi reading
   the page itself several times a second through WebView2's in-process
   DevTools call, which keeps working when the page's own script has been
   replaced by a navigation. Neither decides anything; both report.
2. The machine authorizes the caller against the agents' own number lists
   (`decideVoiceCall`). An unauthorized caller is left completely alone, so
   Google Voice takes them to voicemail exactly as if FlipAi were not there.
3. An authorized caller is answered, and keeps being answered for the whole
   ring, escalating through a scripted click, a real pointer press through the
   browser's input pipeline, and the Windows accessibility Invoke.
4. The desktop app is pointed at the virtual cables *before* its voice session
   starts, because Windows hands a process the endpoint it had when the stream
   opened.
5. Any leftover voice session is ended, a new one is started, and FlipAi
   confirms through the app's accessibility tree that it really started. Only
   then is the call reported as a working conversation.
6. On hang-up the session is ended and confirmed ended. The next call starts
   from nothing.

No debugging port is opened for any of this: the DevTools call is in-process,
against FlipAi's own view. The Google Voice process does serve one loopback
endpoint, holding a token it generates itself, so the host can ask it to send an
image through the signed-in session it owns.

Audio never passes through FlipAi. Google Voice's speaker and microphone are
pinned inside the page to two virtual cable ends; the desktop app's are written
into the Windows per-application audio store. Nothing is recorded, transcribed
or uploaded.

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
