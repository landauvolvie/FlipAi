# FlipAi — AI SMS Bridge

FlipAi is an open-source Windows bridge that turns Google Voice SMS messages into local Codex or Claude Code tasks on the user's PC.

```text
SMS -> Google Voice -> Gmail -> FlipAi -> C: Codex / A: Claude -> short reply
```

FlipAi does not require OpenAI or Anthropic API keys. Codex uses the local Codex App Server with **Sign in with ChatGPT**. Claude uses the local Claude Code CLI with a normal Claude subscription login.

## Download and install

For normal Windows use, download the newest **`FlipAi-Setup-vX.Y.Z.exe`** from GitHub **Releases** and run it.

The Setup EXE is a real **per-user Windows installer**. It does **not** request administrator/UAC elevation. It:

- installs FlipAi under `%LOCALAPPDATA%\Programs\FlipAi`;
- creates a **FlipAi** Start Menu shortcut;
- registers **FlipAi** in Windows **Installed apps / Programs and Features** with an uninstaller;
- installs the FlipAi icon used by the Start Menu, installer, and system tray;
- enables current-user startup by default through `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`;
- launches FlipAi after installation so the first-time setup page opens immediately.

The release intentionally publishes the installer rather than asking normal users to extract a folder containing README/source files.

## Windows behavior

FlipAi has several internal roles, all inside the installed `FlipAi.exe`:

- **Launcher / window** — launching FlipAi from the Start Menu makes sure the background bridge is alive and opens the FlipAi app window.
- **Watchdog** — stays hidden and restarts the background host and tray process if either unexpectedly exits.
- **Background host** — monitors Gmail, talks to Codex/Claude, and serves the app window on `127.0.0.1` only.
- **System tray** — shows the FlipAi icon in the notification area. Double-click it or choose **Open FlipAi Settings** to reopen the window. Choose **Quit FlipAi Completely** to stop the tray, host, and watchdog.

Closing the window **does not stop FlipAi**. The background bridge keeps running in the notification area, which Settings can change with **Close to tray**. Only an explicit Quit stops everything.

The window is a Microsoft Edge WebView2 frame over the local control server, so it looks and behaves like an ordinary desktop app: no address bar, no browser tab, and no external site involved. External links, such as the Google OAuth consent page, still open in the user's normal browser through the Windows Shell API (`ShellExecuteW`).

No administrator rights are required. The bridge continues while Windows is locked. Sleep or hibernate pauses it until the computer wakes.

## First setup

1. Run `FlipAi-Setup-vX.Y.Z.exe`.
2. Complete the normal Windows installation wizard.
3. On the Finish page, leave **Launch FlipAi and complete setup** checked.
4. FlipAi starts its tray/background processes and opens the app window.
5. Choose one Gmail connection method: **App Password** or **your own Google API/OAuth project**. There is no default.
6. Add one or more allowed phone numbers and create an SMS security code.
7. Test Gmail.
8. Test Codex, and Claude if you want `A:` routing.
9. Send a fresh Google Voice SMS.

Afterward, open FlipAi from either the **Start Menu** or the **system tray**.

## The FlipAi app

The window has a sidebar with five pages:

| Page | What it does |
| --- | --- |
| **Home** | Live status of Gmail, both agents, and the allowlist; recent activity; **Pause FlipAi**, which leaves incoming texts unread in Gmail until you resume |
| **Connections** | Gmail method and credentials, subject-phrase matching, and the Google Voice card: calling on or off, Google Voice itself shown inside the app, the audio path, and which agents can take a call |
| **Agents** | A pane per agent holding everything that agent owns — the numbers allowed to reach it and what each may do, its security code, its executable, working folder, SMS shortcut, access, conversation, reply behaviour, and the instruction sent with every text |
| **Activity** | Every stage of every message, filterable by stage, agent, text, and time, with how long each step took; export and clear |
| **Settings** | Updates, start with Windows, **start before sign-in**, startup repair, close to tray, theme, compact layout, alerts, shared message routing, the local service, log files, and reset |

Everything the UI reports is real state: an agent says **Ready** only because a
test actually succeeded, and says **Not tested yet** otherwise.

Every setting lives on exactly one page. Anything that belongs to one agent is
inside that agent's pane, and the pages that no longer own a setting link to the
one that does. Pressing any **Test** answers where you pressed it rather than
sending you to a page of its own.

## Updating

FlipAi checks its own GitHub releases in the background, so a newer version is
noticed without opening Settings. When one exists, the version in the sidebar
turns into **v0.13.0 → v0.14.0** on every page.

**Settings → Updates** has two controls:

- **Install updates automatically** (on by default) — FlipAi downloads the
  release, verifies it against the checksum published beside it, installs it,
  and comes back on the new version on its own. An installer whose checksum
  does not match is never run. An update never interrupts an SMS turn: if a
  turn is running when the update is found, or starts during the download, the
  install waits for the next check.
- **Check for updates every** — hourly, 6 hours (default), 12 hours, daily, or
  weekly.

You can still install on demand with **Settings → Updates → Install**. Either
way the install runs in place:

- the existing install is detected, so the wizard asks **no setup questions**;
- your Gmail connection, allowed numbers, security code, agent paths, and
  startup choice are kept;
- the bridge stops for a few seconds and comes back on the new version.

An update you started from inside the app **reopens the FlipAi window** when it
finishes. An automatic background update restores the tray and bridge without
stealing focus, since you did not ask for it at that moment. Before v0.13.0 an
in-app update restarted only the background bridge, so the app looked like it
never came back.

Downloading a release by hand and running it does the same thing — an installer
that finds FlipAi already installed goes straight to updating it.

The release check sends no identifier, no configuration, and no message data,
and contacts only `api.github.com` for this repository.

## Starting before sign-in

By default FlipAi starts when you sign in, because that is all a per-user
startup entry can do. After a reboot, nothing runs until someone signs in.

**Settings → Startup → Start before sign-in** changes that. Turning it on asks
Windows for administrator approval once and registers a scheduled task with a
power-on trigger, so FlipAi is already handling texts before anyone logs in.
Installation never asks for administrator rights — this switch is the only place
FlipAi does, and only when you turn it on.

Two things to know:

- The task runs as your Windows account without storing your password. Windows
  gives that kind of task no account key, so FlipAi re-protects its saved
  credentials for this PC instead of for your account while the option is on.
  An administrator on this PC could then read them; turning the option off
  re-protects them for your account again.
- Codex and Claude must be signed in on this Windows account, exactly as they
  are for normal use.


## SMS routing and allowed numbers

**A phone number belongs to one agent.** You add it under Codex or under Claude
on the Agents page, and it reaches that agent — the number decides, not a prefix.
Each number also carries what it may do: **texts and calls**, **texts only**, or
**calls only**. The same list decides who may phone the agent, so access is
answered in one place.

If you use one phone and want to reach both agents by text, you need a second
number: one number cannot be on both.

```text
check GitHub and fix the failed build
NEW
STATUS
```

- The sending number picks the agent.
- A `C:` or `A:` prefix still works, but only when it names the agent that
  number already reaches. Naming the other one is refused with an explanation.
- `NEW` starts a fresh conversation with that agent. Every later text continues it until you send `NEW` again. The active conversation is stored on disk, so it survives closing FlipAi, restarting it, and rebooting Windows.
- A security code is optional, off on a new install, and set per agent on that
  agent's pane. When it is on, put it in front of the message:
  `482913 check the build`.
- US/Canada numbers are normalized to 10 digits; `+1`, spaces, parentheses, and hyphens are accepted during setup.
- A sender not on the allowlist is ignored even if the SMS body contains an allowed number.

FlipAi extracts the sender from Google Voice's authenticated message envelope/Reply-To information rather than trusting phone numbers written in the SMS body.

## Gmail / Google Voice

Enable Google Voice **Forward messages to email**, then choose exactly one Gmail method in FlipAi.

### Option 1 — Gmail App Password

This is the simplest independent setup and requires no Google Cloud project.

1. Turn on Google 2-Step Verification.
2. Create a Google App Password.
3. Enter the Gmail address and App Password in FlipAi.
4. Run **Test Gmail**.

FlipAi connects directly to `imap.gmail.com:993` over TLS. It uses **IMAP IDLE**, so Gmail can wake FlipAi immediately when the Google Voice email arrives. A fallback timer protects against a dropped IDLE connection. SMTP is used only for the Google Voice email-reply fallback.

The App Password is protected locally with Windows DPAPI and is not stored as plaintext by the Windows build.

### Option 2 — Your own Google API / OAuth project

1. Create your own Google Cloud project.
2. Enable Gmail API.
3. Create OAuth credentials with application type **Desktop app**.
4. Upload the JSON in FlipAi.
5. Connect the Google account and run **Test Gmail**.

OAuth tokens are protected locally with Windows DPAPI. Without Google Pub/Sub, the local OAuth/Gmail API method checks approximately once per second.

## Google Voice sender security

An incoming command is accepted only after all of these checks pass:

1. the message looks like a real Google Voice notification;
2. Google's DKIM authentication passed;
3. FlipAi extracts the actual Google Voice SMS sender from trusted headers/envelope data;
4. that exact normalized number appears in the user's allowlist;
5. the SMS security code matches.

The untrusted message body is never used to decide who sent the SMS.

## Codex connection

FlipAi starts the local Codex interface:

```text
codex app-server --listen stdio://
```

It verifies that Codex is using ChatGPT-managed authentication and refuses provider/API-key auth. FlipAi can discover the normal per-user Codex Desktop runtime on Windows when `codex` is not already on PATH.

## Claude connection

FlipAi invokes Claude Code in non-interactive mode:

```text
claude -p "..." --output-format json --permission-mode bypassPermissions --name "FlipAi SMS" --chrome
```

It strips Anthropic API-key environment variables and requires a signed-in subscription account.

### Access level

An SMS turn gives Claude the same reach FlipAi already gives Codex: the normal permissions of the signed-in Windows user, with no elevation.

This matters because texting is unattended. Claude's narrower modes do not simply do less — they refuse. `acceptEdits` auto-approves **file edits and nothing else**, so Bash and every MCP tool still raise a permission prompt, and the Claude in Chrome tools are MCP tools. With nobody at the keyboard to answer that prompt, the call is refused and Claude reports, accurately, that it was not allowed to drive Chrome — even though the same account drives Chrome fine when you run `claude` yourself. Full user access is what makes a texted command behave like the same command typed at the desktop.

You can still narrow it under **Agents → Behavior → Claude permission mode**. If a turn is blocked, the reply names the tool and the mode that blocked it rather than leaving you with Claude's bare refusal.

> **Upgrading from 0.11.1 or earlier:** those builds rewrote this setting to `acceptEdits` on every load — an explicit `bypassPermissions` was overwritten too — so the stored value recorded what that rewrite produced rather than a choice. Upgrading moves it to full user access once. Change it on the Agents page if you want it narrower.

Claude refuses full-access mode when it is started with administrator privileges. FlipAi never needs elevation, so run it as your normal Windows user.

## Claude conversation mode

**Agents → Claude → Conversation mode** offers two ways for SMS to reach Claude.

### Per-message (default)

Each text is one `claude -p` request that resumes the stored session id. Nothing
runs in the background between texts, it works with the long-lived token below,
and it is what FlipAi falls back to whenever live mode cannot run. Leave this
selected unless you specifically want the browser view.

### Live session with Remote Control

FlipAi keeps **one** Claude Code session running for the whole conversation and
delivers each text into it, so the same session can be opened at
[claude.ai/code](https://claude.ai/code) and continued from a phone or browser.
SMS replies and anything typed in the browser share one transcript.

How it works: FlipAi starts `claude remote-control --spawn session` and delivers
each text into that session's **cross-session messaging inbox**, which is a named
pipe on Windows. Claude Code exports the pipe's address and token only into the
environment of the session's own hook processes, so FlipAi registers a small hook
that runs `FlipAi.exe --claude-hook` and posts back to FlipAi's loopback server —
first to report the inbox, then to hand back each finished answer.

Requirements, each reported on the Agents page when it is not met:

- **Claude Code 2.1.234 or newer** on Windows. Below that a session binds no
  inbox, so live mode cannot deliver a text at all and FlipAi stays in
  per-message mode.
- **A completed Claude Code sign-in** for the browser view. This is the important
  one: Remote Control **refuses a `claude setup-token`** — those tokens can only
  make model requests. Press **Connect Claude** on the Agents page to sign in.
  A stored token does not cost you the browser view when a sign-in exists;
  FlipAi runs the session on the sign-in and keeps the token as the fallback.
  With a token and *no* sign-in, live mode still runs and SMS still stays in one
  session, but the session will not appear at claude.ai/code, and the Agents
  page says so rather than implying a view that will never show up.
- **A first-party Claude subscription**, the same billing path FlipAi requires
  everywhere else.

Two behaviours worth knowing:

- **A text is never lost to live mode.** If the session cannot start, cannot be
  reached, or does not answer, FlipAi runs that text through per-message mode
  instead and records the reason in Activity.
- **Browser turns are not texted to you.** A live session has two writers, so
  FlipAi tags each injected prompt and only texts back the answer to a turn it
  started. Anything you type at claude.ai/code is logged in Activity and left
  alone.

The new-session command ends the running session and starts a fresh one, so a
new conversation really is new.

## Finding the Claude SMS conversation

Codex and Claude persist conversations differently, and FlipAi cannot paper over the difference:

- **Codex** — FlipAi starts durable (non-ephemeral) threads and hands each one back with `thread/unsubscribe` once its turn completes, so the same conversation opens in Codex Desktop.
- **Claude** — Claude Code has no equivalent handoff, and it deliberately keeps `claude -p` sessions **out of the interactive `/resume` picker**. The SMS conversation is a real, resumable session; it just is not in that list.

**Agents → Claude** shows the exact command to continue it:

```text
claude --resume <session id>
```

Resuming by id works from any folder on Claude Code 2.1.223 or newer, which searches every project on the machine. On older versions, run it from Claude's working folder.

FlipAi also names each conversation `FlipAi SMS <date time> <suffix>`, which is a working resume handle — `claude --resume "FlipAi SMS …"` — as long as the name is unique. That is why the name carries a timestamp and a random suffix instead of being the same string every time: Claude Code refuses an ambiguous name with `matches N sessions`.

### Move it to Claude Desktop

Claude Desktop keeps its own session history and cannot list a CLI session. There is a supported way to move one across — resume the session, then run:

```text
/desktop
```

Claude saves the session and opens it in the desktop app. This works on Windows x64 and macOS with a Claude subscription; it is not available with API-key auth or on Bedrock, Vertex, or Foundry.

FlipAi cannot do this for you on every text: `/desktop` is interactive-only and closes the CLI session as it hands over, so it is a deliberate step you take when you want to keep working on that conversation at the desktop.

### When the old conversation is gone

Claude Code deletes transcripts after 30 days by default (`cleanupPeriodDays`), and a transcript can also be deleted or corrupted. When FlipAi finds the stored conversation missing it starts a fresh one, retries your command on it, and prefixes the reply with:

```text
Previous Claude conversation was unavailable, so a new one was started.
```

Earlier builds returned the failure instead and kept the dead id, so every later Claude text failed the same way until you sent the new-session command. Codex has recovered from a missing rollout this way for some time; Claude now matches it.

## Connecting Claude

Claude Code CLI keeps its own sign-in, separate from the Claude desktop app. Being signed into Claude Desktop does **not** sign in the CLI.

There are two possible credentials, and only one of them can do everything:

| | Answers texts | Controls Chrome | Opens claude.ai/code | Expires |
|---|---|---|---|---|
| **`claude /login` session** | yes | yes | yes | on its own, eventually |
| **`claude setup-token` value** | yes | **no** | **no** | months |

The browser extension authenticates against the *sign-in*, so Claude Code turns Chrome off for a token session even when `--chrome` is passed. A machine holding only a token answers texts perfectly while quietly refusing every browser request — which is exactly what FlipAi's connect flow exists to prevent.

**Agents → Claude → Authentication & session** shows which credential you are on:

- **Connect Claude** opens a console window running the Claude Code sign-in on this Windows account. Complete it, then press **Check connection**.
- **Check connection** re-probes the machine and reports what FlipAi will actually use on the next text. No restart needed.
- **Disconnect** removes FlipAi's stored token. Your own Claude Code sign-in is left alone — that belongs to the Windows account, not to FlipAi.

### The long-lived token, as the fallback

A CLI browser session eventually expires with `OAuth session expired and could not be refreshed`, which strands an unattended bridge. To survive that, run this once in PowerShell and paste the value into the **Long-lived token** field:

```powershell
claude setup-token
```

FlipAi stores it with Windows DPAPI and passes it to Claude Code as `CLAUDE_CODE_OAUTH_TOKEN` — but **only when it has to**. With a `claude /login` session on the account, the token is withheld from every turn so Chrome and Remote Control keep working; it is used when that sign-in is gone, where the choice is between a bridge that answers without the browser and one that does not answer at all. The token is optional, and it is not permanent — Claude reports lifetimes up to a year and can ask for a fresh one.

## Replies

FlipAi delivers every reply itself. The agent is never asked to open a browser, find a conversation, or confirm that it sent anything.

When a turn finishes, FlipAi replies to the authenticated Google Voice conversation address (`…@txt.voice.google.com`), and Google converts that email back into an SMS. This is faster than a browser send by a wide margin, it cannot half-fail, and because delivery is decided in Go rather than by model output, nothing written in an incoming SMS can redirect or suppress a reply.

The agent therefore keeps exactly the capabilities it has at the desktop. Texting `C: open my browser and compare these two tabs` still uses the browser — because *you* asked for it, not because FlipAi told it to.

An answer longer than **Characters per text** is split across numbered messages (`1/3`, `2/3`, …) up to **Maximum texts per answer**, rather than being cut off.

Two optional status texts are on by default and can be turned off in Settings:

- **Text me when the agent starts** — a one-line confirmation within seconds of your text, so you know it landed before the work finishes.
- **Text me progress while it works** — periodic updates during a long turn, naming the current step when the agent reports one.

`STATUS` is answered directly by the bridge without involving an agent, so it stays instant even while a long turn is running. Commands that arrive during a turn are queued and run in order.

## What the agent is told about SMS

FlipAi adds exactly one instruction to your text before handing it over: your
own words inside an `<sms_command>` fence, then a single line explaining that
the answer is delivered as a text message. That line is why an answer comes back
phone-sized instead of terminal-sized.

It ships as:

> Your answer is delivered to the user as an SMS text message, so keep it brief and in plain text.

Codex and Claude each have their own editable copy, on their pane of the
**Agents** page:

- Leave an agent's box empty and it returns to the wording above, because every
  turn needs some framing.
- The editor shows a live character count and a preview of the exact prompt the
  agent receives, so there is no guessing about what was added.

Nothing else is added. FlipAi never tells the agent to open a browser, find a
conversation, or emit a delivery marker — delivery is decided in Go — so the
agent behaves as it does when you are sitting in front of it.

## Phone calls to the agent (experimental)

FlipAi can answer a call to your Google Voice number and put the caller through
to the **voice mode of the ChatGPT/Codex desktop app** running on this PC --
the same agent that can actually work on and control the computer. No AI API
key is involved: the conversation happens inside the desktop app exactly as it
does when you talk to it yourself.

Google Voice is a part of FlipAi, not a browser FlipAi drives. It runs in
FlipAi's own Edge WebView2 view -- the same component the FlipAi window itself
is drawn with -- created and kept alive by FlipAi, signed in once and loaded at
all times. There is no external browser to start, no separate application whose
windows appear on the desktop, and no state in which Google Voice is a window of
its own: it is either standing inside the FlipAi panel or parked off every
display, still running and still taking calls.

FlipAi watches for a ringing call -- in the page, in its frames, or announced
only by a notification -- checks the caller against the agents' own lists of
numbers, presses **Answer**, points the desktop app's audio at the virtual
cables, and starts a fresh voice session in it. When the caller hangs up, that
session is ended and confirmed ended, so the next call starts clean.

**An allowed caller is not left to voicemail because one click was ignored.**
Google Voice rings for about 25 seconds, and FlipAi keeps pressing Answer for
the whole of it, escalating through three different ways of pressing: the
page's own click, a real pointer press delivered through the browser's input
pipeline, and the Windows accessibility Invoke a screen reader would use.

There is deliberately **no separate auto-answer switch**: with calling enabled,
an authorized caller is always answered and an unauthorized caller never is.
Detecting and answering are one behavior, not two options.

### Where everything lives

- **Settings → Google Voice calling** is the whole setup: the switch that turns
  calling on, **Sign in** / **Sign out** for the Google account (sign-out
  deletes the saved browser session), the desktop apps a call is put through
  to, and a live status row for every part -- the window, the sign-in, the
  cables, the app's audio routing, the WebView2 runtime, and the browser
  permissions (which FlipAi grants itself; no Windows prompt ever needs
  answering).
- **Connections → Google Voice** is the live view: the real Google Voice
  standing inside the page, where you sign in and can watch a call arrive.
  Closing or leaving the preview never stops it -- Google Voice leaves the panel
  and keeps running out of sight, detecting and answering incoming calls. It is
  clipped to whatever part of the panel is on screen, so scrolling or a smaller
  FlipAi window moves it rather than making it disappear.

Nothing on either page waits for a Save button; every control writes as it is
changed, and the calling switch writes through an endpoint of its own so
nothing else can hold it up.

### How the sound actually gets to the desktop app

FlipAi never records, transcribes, or uploads the call. It moves sound between
two applications on this PC over virtual audio cables -- silent by
construction, no speaker plays them and no microphone hears them, and they
keep working while the PC is locked:

```text
caller -> Google Voice (inside FlipAi)
       -> Google Voice's "speaker"  == cable 1 ==  the desktop app's "microphone"
       -> the agent hears the caller and answers
       -> the desktop app's "speaker" == cable 2 ==  Google Voice's "microphone"
       -> caller hears the agent
```

**The routing is chosen and applied entirely by FlipAi -- there are no device
pickers anywhere.** FlipAi reads the machine's endpoint list, recognizes the
installed cable families (VB-CABLE, VB-CABLE A/B, VoiceMeeter and its AUX and
VAIO3 strips, VB-Audio Point), pins Google Voice's microphone
and speaker inside the page itself, and writes the desktop app's
per-application default microphone and speaker through the same Windows per-app
audio store the Settings app uses. The desktop app is wired while the phone is
still ringing, before its voice session opens a stream, because Windows hands a
process the endpoint it had when the stream opened. You never open the AI app's
audio settings, and your PC's real microphone and speakers are never part of
the call.

The one thing FlipAi cannot conjure is the cables themselves: Windows has no
built-in way to pipe one application's speaker into another's microphone, and
FlipAi does not install or redistribute audio drivers. Install **two** virtual
cables once -- VB-CABLE A+B, or VoiceMeeter, which includes two -- and the
status row flips to wired. With one cable FlipAi wires the caller-to-agent
half and says exactly what is missing; with none it says exactly what to
install. A call is still answered either way, and the call state says what is
wrong with the sound.

For the rare unrecognized cable, the four endpoint names in `voice-call.json`
(`googleVoiceInput/Output`, `agentInput/Output`) act as hand-edited overrides;
an override only applies while a matching device is actually present.

### Setting it up

1. Settings → **Google Voice calling** → turn **Answer phone calls with an
   agent** on. FlipAi loads Google Voice at Windows sign-in, keeps it running
   while the PC is locked, and brings it back if it ever stops.
2. **Sign in to Google Voice** (or sign in inside the live view on
   Connections).
3. In Google Voice itself, open **Settings → Calls** and turn on receiving
   calls on this device. **FlipAi cannot answer a call that never rings in its
   window**, and until this is on, an incoming call goes to your forwarding
   phones instead. The status card reminds you while no ring has ever been
   seen.

   Google Voice partly decides whether a browser may take calls from what that
   browser can do, and one thing it looks at is whether it can raise a
   notification. FlipAi supplies that capability to its own view when the
   WebView2 runtime does not, and says so among the controls on the status
   card when it had to.
4. Install two virtual audio cables (once): VB-CABLE A+B or VoiceMeeter.
   FlipAi finds them and wires everything by itself.
5. Agents → pick the agent → add your phone number and set it to **Texts and
   calls** or **Calls only**. That is what puts the agent on calls; there is
   no second switch to find. The status card says which agents can take a
   call.

### Who is allowed to call, per agent

Every permission belongs to one agent. The numbers that may text an agent are
the numbers that may call it, on that agent's own pane, and a number belongs to
exactly one agent:

- a number allowed under ChatGPT/Codex cannot reach Claude, and a text from it
  that asks for Claude by name is refused rather than routed;
- a number set to **Texts only** may not call; one set to **Calls only** may not
  text;
- **Allowed caller names** -- the exact text Google Voice displays when the
  caller is in your Google Contacts and there is no number to match -- belong to
  one agent too. Placeholders such as "Unknown" or "Private" are refused, since
  accepting one would let any anonymous call through.

A caller matching neither list is never answered, and the card shows what Google
Voice displayed and why the call was not connected. A ringing card that names
nobody is never given an identity from elsewhere on the page: the caller is
read from the ringing UI itself, or from the incoming-call notification, or
the call is refused.

### It runs in the background

Google Voice is parked out of sight rather than closed, because a closed page
cannot ring. Windows keeps it running while the PC is locked, and FlipAi brings
it back if it ever stops. Nothing flashes on screen at sign-in and nothing
appears in the taskbar or in Alt-Tab at any point.

It is parked off the edge of the desktop rather than minimized, deliberately. A
minimized browser window is one Chromium is entitled to treat as hidden: it
backgrounds the renderer and slows its timers to once a minute, far longer than
a call rings for. Off-screen it stays live. On top of that FlipAi starts the
view with background throttling, renderer backgrounding and occlusion detection
switched off, and does not rely on a timer to notice a call in the first place:
the page change a ringing call makes -- in the main document or in any
same-origin frame -- drives the check, an incoming-call notification triggers an
immediate burst of checks, and FlipAi reads the page itself several times a
second as an independent second pair of eyes.

That second view uses WebView2's own in-process DevTools call, against FlipAi's
view and no other. **No debugging port is opened and nothing listens for it**,
which is both safer and the only thing that works: the WebView2 runtime ignores
the loopback debugging switch a browser would honour. It is also what delivers
a real pointer press to a ringing call, and what attaches an image to an
outgoing message.

### If Google Voice does not appear

Google Voice runs in a second FlipAi process -- that is what lets it stay signed
in and listening with the FlipAi window closed -- so a failure happens out of
sight of the page. FlipAi waits for it and reports what stopped it, on the
Connections panel and in Activity.

The usual cause is a missing **Microsoft Edge WebView2 Runtime** -- FlipAi
cannot draw Google Voice, or its own window, without it. Settings shows the
installed version, and says so plainly when it is absent. Microsoft distributes
it free as the Evergreen Standalone Installer.

If the panel says Google Voice is listening in the background rather than
showing it, scroll the panel into view: it is clipped to the part of the panel
that is actually on screen, and below a certain size it is withdrawn rather
than hung over the rest of the app.

### Limits worth knowing

- Windows only, and the desktop session has to be signed in. The window keeps
  running while the PC is locked, but it cannot start at the sign-in screen.
- FlipAi starts the desktop app itself if it is not running: it looks where
  the Codex and ChatGPT desktop apps install themselves, then for their Start
  Menu shortcut, which is what reaches a Store-packaged app. A launch command
  configured in FlipAi always wins over both.
- Voice mode is started through the app's accessible Voice control, or its
  configured keyboard shortcut, and FlipAi then **checks that voice mode
  actually started** before reporting the call as a working conversation. If it
  did not, the status says which controls the app offered instead of claiming
  the call is fine.
- The per-app audio routing uses the same per-application store Windows'
  own Settings app writes. If Windows refuses it, the status row says so and
  names the one-time manual fallback: choose the cable ends once inside the
  app's own audio settings.
- ChatGPT desktop Voice is a full two-way conversation. Claude Desktop's voice
  support is dictation-oriented, so the Claude side may not hold up its end of
  a spoken conversation the same way.
- This is separate from SMS. Turning it on changes nothing about Gmail, message
  routing, or the SMS allowlist.

## Runtime data

Runtime configuration, encrypted credentials/tokens, state, and logs are stored under:

```text
%LOCALAPPDATA%\AISMSBridge
```

The legacy data-directory name is intentionally retained for upgrade compatibility. The installed application itself lives under `%LOCALAPPDATA%\Programs\FlipAi`.

## Uninstall

Use **Windows Settings -> Apps -> Installed apps -> FlipAi -> Uninstall**, or the classic **Programs and Features** Control Panel.

The uninstaller stops FlipAi, removes the Start Menu shortcut, removes the current-user startup entry, removes the installed app files, and removes the local FlipAi bridge data/credentials.

## Security notes

- No public listening port; the web UI binds to loopback only.
- Local setup actions require a random local session token/cookie.
- Google OAuth tokens or Gmail App Passwords are protected with Windows DPAPI.
- The SMS code is stored as a salted, iterated hash rather than plaintext.
- Codex approval requests that reach the unattended bridge are declined.
- Both agents act with the normal permissions of the signed-in Windows user and neither is sandboxed from your own files or tools — that is what the product does. The phone allowlist and SMS security code are the boundary, not an agent sandbox. See [Access level](#access-level).
- No telemetry, packer, obfuscation, browser-password extraction, keylogging, process injection, or code injection.
- No service, driver, HKLM startup entry, scheduled task, firewall rule, or administrator elevation is required.

A managed organization can still block user-profile executables or startup entries with AppLocker, WDAC, or endpoint-security policy. FlipAi does not attempt to bypass those controls.

See [SECURITY.md](SECURITY.md).

## Build

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Generate-FlipAiIcon.ps1 -OutputPath .\dist\FlipAi.ico
go test ./...
go vet ./...
go test -race ./...
go build -trimpath -ldflags "-H=windowsgui -s -w" -o .\dist\FlipAi.exe .
```

The GitHub workflow also compiles the no-admin Inno Setup installer.

## Release and lifecycle tests

Before a Windows artifact is accepted, GitHub Actions verifies:

- unit tests and `go vet`;
- Go race detector;
- Windows `FlipAi.exe` build;
- background watchdog/host/tray startup;
- branded tray icon loading;
- host crash and automatic restart;
- tray crash and automatic restart;
- explicit Quit stopping all FlipAi processes;
- Microsoft Defender scan when Defender is available;
- real Setup EXE installation without admin rights;
- installed `FlipAi.exe` and `FlipAi.ico` existence;
- Start Menu shortcut creation;
- Windows uninstall/Installed-apps registry registration;
- current-user startup registration;
- Settings opener targeting the FlipAi localhost URL;
- installed tray loading the branded icon;
- real uninstaller cleanup of app files, Start Menu shortcut, uninstall registration, and startup entry;
- Quit stopping every FlipAi process, the Google Voice window included;
- Google Voice coming up inside FlipAi's own WebView2 view, with **no Microsoft
  Edge application started**, exactly one Google Voice window however many times
  it is asked for, that window carrying no taskbar button and no Alt-Tab entry,
  FlipAi's own endpoint inside that process answering with its token and
  refusing without it, and a real DevTools call reaching the page in-process
  (`scripts/Assert-GoogleVoiceReceiver.ps1`).

### The call lifecycle

`voice_session.go` holds the whole life of a call -- ring, authorize, answer,
start the desktop voice session, hang up, tear down, next call -- as a state
machine with no Windows types in it, so it is tested directly. Those tests
cover an allowed caller being answered at once and kept being answered for the
whole ring, an unauthorized caller never being touched by any rung of the
ladder, a caller Google Voice names a moment after the card appears, a call
answered by hand, a single dropped frame not ending a live call, an unreadable
page never ending one, exactly one voice session started and one torn down per
call, a failed voice session never being described as a working conversation,
and the next call getting a fresh session.

The way voice mode is driven in the desktop app is checked the same way: the
accessibility report is parsed and asserted, including that starting voice mode
never presses a control that would end it, and that the Google Voice answer
path never presses Decline or Send to voicemail.

### Call-bridge browser tests

`TestGoogleVoiceCallFlowInRealBrowser` runs the script FlipAi injects into
Google Voice inside headless Chromium, against a stand-in Google Voice page and
the real Go call bridge. Chromium's fake audio endpoints stand in for the
cable ends, and the browser genuinely applies the microphone and speaker
FlipAi selects, so the routing is checked rather than assumed. It covers
answering an approved caller with no auto-answer option in the way, refusing
an unknown one, contact-name callers, a ring rendered inside a same-origin
iframe, a caller identified only by the incoming-call notification, refusing
to treat a number elsewhere in the page as the caller, answering a single ring
only once, answering without the poll timer, and the capabilities the call
window must not keep. The automatic cable detection has its own unit tests
over the real VB-CABLE and VoiceMeeter endpoint names.

It needs Node and Playwright and skips itself when they are absent, so the
Windows release job does not run it.

### What is verified where, honestly

Everything above runs without a phone. Be clear about the boundary:

| Verified in CI | Only verifiable on your Windows PC |
| --- | --- |
| The call state machine end to end, including authorization, the answer ladder, teardown and the next call | Google Voice's own markup: whether the ringing card FlipAi looks for is the card Google renders today |
| The injected page script driving a stand-in Google Voice page in real Chromium, with the browser genuinely applying the microphone and speaker FlipAi selects | Real telephony: that a call to your number rings in this browser at all, which needs "Receive calls on this device" on in Google Voice itself |
| Google Voice coming up inside FlipAi's own WebView2 view, alone, with no external browser and no taskbar window, and FlipAi reaching that page through WebView2's in-process DevTools call | Whether the Codex desktop app on your machine exposes a Voice control FlipAi can press, and whether pressing it starts a conversation |
| The cable plan, the audio-path invariants, and the per-app routing script's contents | Real audio over real virtual cables: that the caller hears the agent and the agent hears the caller |
| The Windows build, the installer, install and uninstall | The whole cycle repeating reliably on your line |

A Linux CI run proving the state machine is not a Windows call working. The
last column is the part that has to be tried on the real machine, and the
Connections status rows are written to say which step failed when it does.

## SmartScreen / antivirus

FlipAi is intentionally ordinary, unobfuscated Go code. A newly built **unsigned** Setup EXE can still receive a Windows SmartScreen reputation warning or a third-party false positive. For broader public distribution, Authenticode signing is recommended.

## License

MIT.
