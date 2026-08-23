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

- **Launcher / Settings opener** — launching FlipAi from the Start Menu makes sure the background bridge is alive and opens the local Settings page in the user's default browser.
- **Watchdog** — stays hidden and restarts the background host and tray process if either unexpectedly exits.
- **Background host** — monitors Gmail and talks to Codex/Claude.
- **System tray** — shows the FlipAi icon in the notification area. Double-click it or choose **Open FlipAi Settings** to reopen Settings. Choose **Quit FlipAi Completely** to stop the tray, host, and watchdog.

Closing the browser tab or browser window **does not stop FlipAi**. The background bridge continues running. Only an explicit Quit stops it.

Settings are opened with the Windows Shell API (`ShellExecuteW`) so the localhost URL is sent to the user's default browser. FlipAi does not use `explorer.exe` to open Settings.

No administrator rights are required. The bridge continues while Windows is locked. Sleep or hibernate pauses it until the computer wakes.

## First setup

1. Run `FlipAi-Setup-vX.Y.Z.exe`.
2. Complete the normal Windows installation wizard.
3. On the Finish page, leave **Launch FlipAi and complete setup** checked.
4. FlipAi starts its tray/background processes and opens the local Settings page.
5. Choose one Gmail connection method: **App Password** or **your own Google API/OAuth project**. There is no default.
6. Add one or more allowed phone numbers and create an SMS security code.
7. Test Gmail.
8. Test Codex, and Claude if you want `A:` routing.
9. Send a fresh Google Voice SMS.

Afterward, open FlipAi from either the **Start Menu** or the **system tray**.

## SMS routing and allowed numbers

Every remote command must come from an exact allowed Google Voice sender and begin with the configured SMS security code.

```text
482913 C: check GitHub and fix the failed build
482913 A: check Gmail and summarize today's messages
482913 C NEW
482913 A NEW
482913 STATUS
```

- `C:` routes to Codex.
- `A:` routes to Claude.
- No prefix uses the configured default agent.
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
claude -p "..." --output-format json
```

It strips Anthropic API-key environment variables and requires a signed-in subscription account. Dangerous permission bypass mode is not used.

## Replies

FlipAi delivers every reply itself. The agent is never asked to open a browser, find a conversation, or confirm that it sent anything.

When a turn finishes, FlipAi replies to the authenticated Google Voice conversation address (`…@txt.voice.google.com`), and Google converts that email back into an SMS. This is faster than a browser send by a wide margin, it cannot half-fail, and because delivery is decided in Go rather than by model output, nothing written in an incoming SMS can redirect or suppress a reply.

The agent therefore keeps exactly the capabilities it has at the desktop. Texting `C: open my browser and compare these two tabs` still uses the browser — because *you* asked for it, not because FlipAi told it to.

An answer longer than **Characters per text** is split across numbered messages (`1/3`, `2/3`, …) up to **Maximum texts per answer**, rather than being cut off.

Two optional status texts are on by default and can be turned off in Settings:

- **Text me when the agent starts** — a one-line confirmation within seconds of your text, so you know it landed before the work finishes.
- **Text me progress while it works** — periodic updates during a long turn, naming the current step when the agent reports one.

`STATUS` is answered directly by the bridge without involving an agent, so it stays instant even while a long turn is running. Commands that arrive during a turn are queued and run in order.

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
- Claude `--dangerously-skip-permissions` is never used.
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
- real uninstaller cleanup of app files, Start Menu shortcut, uninstall registration, and startup entry.

## SmartScreen / antivirus

FlipAi is intentionally ordinary, unobfuscated Go code. A newly built **unsigned** Setup EXE can still receive a Windows SmartScreen reputation warning or a third-party false positive. For broader public distribution, Authenticode signing is recommended.

## License

MIT.
