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
| **Connections** | Gmail method and credentials, subject-phrase matching, a message-flow test that checks the whole inbound path, and Google Voice calling |
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

**Agents → Claude → Advanced** shows the exact command to continue it:

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

- Leave an agent's box empty and it follows the **shared instruction** on the
  Agents → Shared defaults pane.
- Clear the shared box and it returns to the wording above, because every turn
  needs some framing.
- The editor shows a live character count and a preview of the exact prompt the
  agent receives, so there is no guessing about what was added.

Nothing else is added. FlipAi never tells the agent to open a browser, find a
conversation, or emit a delivery marker — delivery is decided in Go — so the
agent behaves as it does when you are sitting in front of it.

## Phone calls to the agent (experimental)

FlipAi can answer a call to your Google Voice number and put the caller through
to the voice mode of the ChatGPT or Claude desktop app you already pay for. No
AI API key is involved: the conversation happens inside the desktop app exactly
as it does when you talk to it yourself.

FlipAi runs Google Voice in its own window, watches for a ringing call, checks
the caller against a per-agent allowlist, answers it, switches the desktop app
into voice mode, and switches it back off when the call ends.

### What you have to provide

**Two virtual audio cables.** This is the part FlipAi cannot do for you, and
without it a call will connect in silence. A phone conversation needs two
independent audio paths, and Windows has no way to patch one application's
speaker into another's microphone on its own. Install a virtual audio cable
driver that gives you two separate cables — each cable appears in Windows as one
playback endpoint and one recording endpoint. FlipAi does not install, bundle,
or redistribute any audio driver.

Wire them like this:

| Direction | Set in | Endpoint |
| --- | --- | --- |
| Caller reaches the agent | FlipAi: Google Voice speaker | Cable 1 playback |
| | ChatGPT/Claude: microphone | Cable 1 recording |
| Agent reaches the caller | ChatGPT/Claude: speaker | Cable 2 playback |
| | FlipAi: Google Voice microphone | Cable 2 recording |

FlipAi applies the Google Voice side itself. The desktop app side you choose
once, inside that app's own audio settings. FlipAi refuses to save a
configuration where both sides share an endpoint, because that produces a call
in which nobody can hear anything.

### Setting it up

1. Settings → **Google Voice phone bridge** → **Open Google Voice**, and sign in.
   The endpoint pickers stay empty until this window exists, because Windows
   only reveals endpoint names to a page that holds microphone permission.
2. Choose the Google Voice microphone and speaker from the table above.
3. Agents → pick the agent → **Phone voice**: allow the agent on calls and list
   who may reach it.
4. Back in Settings, turn **Enable phone voice** on.

### If Open Google Voice does nothing

The window is created by a second FlipAi process, so a failure used to happen
out of sight of the button. Opening now waits for the window to exist and
reports what stopped it; the reason appears as a banner on the page, on the
Connections card, and in Activity.

The usual cause is a missing **Microsoft Edge WebView2 Runtime** — FlipAi cannot
draw the Google Voice window without it. Connections shows the installed
version, and Settings says so plainly when it is absent. Microsoft distributes
it free as the Evergreen Standalone Installer.

If the message says another FlipAi Google Voice process is running without a
window, quit FlipAi from the tray and start it again; that clears a wedged
window process.

### Google Voice has to be set to ring in the browser

**FlipAi cannot answer a call that never rings in its window.** Google Voice only
rings in a browser when you have switched that on in Google Voice itself: open
Google Voice, go to **Settings → Calls**, and turn on receiving calls on this
device. Until then an incoming call goes to your forwarding phones and never
reaches FlipAi.

Connections shows whether a call has ever rung in the window, and can list what
FlipAi can currently see on the page, so "nothing happens when I call" is
something you can look at rather than guess about.

### Who is allowed to call

The same numbers that may text an agent may call it, on the Agents page, as long
as the number is set to **Texts and calls** or **Calls only**. A number set to
**Texts only** is refused with a message saying so.

One extra entry exists for calls: **Allowed caller names** — the exact text
Google Voice displays. You need it whenever the caller is in your Google
Contacts, because Google Voice then shows a name and there is no number for
FlipAi to match. Placeholders such as "Unknown" or "Private" are refused, since
accepting one would let any anonymous call through.

When a call is refused, Connections shows what Google Voice displayed and why it
was not connected, and the agent's Phone voice card offers to add that name to
the list. A caller matching neither list is never answered.

### Limits worth knowing

- Windows only, and the desktop session has to be signed in. The window keeps
  running while the PC is locked, but it cannot start at the sign-in screen.
- ChatGPT desktop Voice is a full two-way conversation. Claude Desktop's voice
  support is dictation-oriented, so the Claude side may not hold up its end of a
  spoken conversation the same way.
- FlipAi drives the desktop app by focusing its window and sending its Voice
  shortcut, so the app has to be running and its window must not be blocked by
  another elevated window.
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
- the Google Voice window actually appearing on the runner's desktop, both from
  `FlipAi.exe --google-voice` directly and through the loopback endpoint the
  Open button calls, with the recorded diagnostics dumped when it does not.

### Call-bridge browser tests

`TestGoogleVoiceCallFlowInRealBrowser` runs the script FlipAi injects into
Google Voice inside headless Chromium, against a stand-in Google Voice page and
the real Go call bridge. Chromium's fake audio endpoints stand in for the two
virtual cables, and the browser genuinely applies the microphone and speaker
FlipAi selects, so the routing is checked rather than assumed. It covers
answering an approved caller, refusing an unknown one, contact-name callers,
refusing to treat a number elsewhere in the page as the caller, answering a
single ring only once, and the capabilities the call window must not keep.

It needs Node and Playwright and skips itself when they are absent, so the
Windows release job does not run it. What it cannot cover, and what only a real
call on a real PC can confirm: Google's own markup, WebView2, the telephony
itself, and whether the desktop AI app actually enters voice mode.

## SmartScreen / antivirus

FlipAi is intentionally ordinary, unobfuscated Go code. A newly built **unsigned** Setup EXE can still receive a Windows SmartScreen reputation warning or a third-party false positive. For broader public distribution, Authenticode signing is recommended.

## License

MIT.
