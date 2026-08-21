# AI SMS Bridge

> **Repository:** FlipAi — open-source SMS bridge for Codex and Claude on Windows.

## Download

For normal Windows use, download the newest `AISMSBridge-vX.Y.Z-windows-x64.zip` from the repository **Releases** page, extract it, and run `AISMSBridge.exe`. No administrator rights are required.

AI SMS Bridge lets a Windows user send a Google Voice SMS to **Codex** or **Claude Code** running on that PC.

```text
SMS -> Google Voice -> Gmail -> AI SMS Bridge -> C: Codex / A: Claude -> short SMS reply
```

The bridge is not an AI model and does not require an OpenAI or Anthropic API key. Codex uses its local App Server and requires **Sign in with ChatGPT**. Claude uses the local Claude Code CLI and requires a normal Claude subscription login. Gmail can be connected in one of two user-selected ways: a personal Google App Password over IMAP/SMTP, or the user's own Google OAuth/Gmail API project. There is no default Gmail method.

## Windows behavior

AI SMS Bridge is one clean executable with separate internal roles:

- **Launcher/UI opener** — double-click `AISMSBridge.exe`; it makes sure the background bridge is alive and opens the local settings page. The launcher then exits.
- **Watchdog** — stays hidden and restarts both the background host and tray process if either crashes.
- **Background host** — monitors Gmail and talks to Codex/Claude.
- **System tray** — shows **AI SMS Bridge** in the Windows notification area. Double-click it or choose **Open Settings** to reopen the GUI. Choose **Quit AI SMS Bridge** to stop the tray, host, and watchdog completely.

Closing the browser settings page with **X does not stop the bridge** because the settings page is not the background process. Only **Quit AI SMS Bridge** from the tray or the **Quit Bridge** button in Settings stops the bridge. Launching the EXE again starts it again. The tray re-registers itself if Windows Explorer restarts.

No administrator rights are required. “Start with Windows” copies the EXE to `%LOCALAPPDATA%\Programs\AISMSBridge\AISMSBridge.exe` and adds a current-user `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` entry. It starts after that Windows user signs in and continues while the workstation is locked. Sleep/hibernate pauses it.

## SMS routing and allowed numbers

During setup add one or more allowed SMS phone numbers and choose an SMS security code. Separate multiple numbers with commas, semicolons, or new lines. Every remote command must come from an **exact allowed sender** and begin with the code.

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
- A sender that is not on the allowlist is ignored even if the SMS body mentions an allowed number.

## Gmail / Google Voice

1. Enable Google Voice **Forward messages to email**.
2. In the bridge GUI, choose exactly one Gmail connection method. New installs have **no default**.

### Option 1 — Gmail App Password

This is the easiest independent setup and requires no Google Cloud project.

1. Turn on Google 2-Step Verification.
2. Create a 16-character Google App Password for the bridge.
3. Enter the Gmail address + App Password in the GUI.
4. Click **Test Gmail connection**.

The bridge connects directly to `imap.gmail.com:993` over TLS and uses **IMAP IDLE**, so Gmail can wake the bridge as soon as the Google Voice notification reaches the Inbox. A 30-second fallback poll protects against a dropped IDLE signal. SMTP on `smtp.gmail.com:465` is used only for the Google Voice email-reply fallback. The App Password is protected locally with Windows DPAPI and is never stored in plaintext by the Windows build.

Google may not offer App Passwords for some managed/Advanced Protection/security-key-only accounts. Those users should choose OAuth instead.

### Option 2 — Your own Google API / OAuth project

1. Create your own Google Cloud project.
2. Enable Gmail API.
3. Create an OAuth **Desktop app**.
4. Upload that Desktop OAuth JSON in the local setup page.
5. Click **Connect / Reconnect Gmail**, then **Test Gmail connection**.

The OAuth backend requests `gmail.readonly` to inspect Google Voice notifications and `gmail.send` for the Google Voice email-reply fallback. OAuth tokens are protected locally with Windows DPAPI. Without Google Pub/Sub there is no local Gmail API equivalent of IMAP IDLE, so OAuth mode checks about once per second for near-immediate delivery.

**OAuth testing warning:** for personal Google Cloud OAuth apps, refresh-token lifetime can be limited while the consent screen remains in Testing. If Gmail disconnects later, review the current Google OAuth publishing/test-user rules for that user's own project.

Incoming commands are accepted only after Google DKIM validation, exact extraction of the sender from Google Voice's structured `@txt.voice.google.com` envelope (or a strict subject-header fallback), exact membership in the configured phone-number allowlist, and the SMS security code all pass. The untrusted SMS body is never searched to decide who the sender is.

The bridge can react immediately once Gmail has the forwarded message. Any delay between the original SMS and Gmail receiving the Google Voice notification is controlled by Google Voice and is outside the bridge.

## Codex connection

The bridge starts the official local interface:

```text
codex app-server --listen stdio://
```

It initializes the JSON-RPC connection, checks `account/read`, and refuses Codex work unless the account type is `chatgpt`. It creates/resumes a persistent Codex thread and sends each `C:` message as a turn.

If the App Server process dies, the bridge reports the failed task and automatically creates a fresh App Server connection on the next Codex SMS. The outer Windows watchdog separately restarts the entire bridge host if the host itself crashes.

A separately launched App Server is not guaranteed to expose every Desktop-only browser/computer tool. Every remote turn therefore receives an explicit return-channel instruction: reply through Google Voice to the **exact authenticated sender number**, using an already-authenticated Google Voice/browser/Chrome session when available, never another allowed number. If browser delivery is unavailable, the bridge uses the authenticated Google Voice email Reply-To fallback.

## Claude connection

The bridge invokes the official Claude Code CLI in non-interactive mode:

```text
claude -p "..." --output-format json
```

It stores the returned `session_id` and uses `--resume` for later `A:` messages. API-related Anthropic environment variables are stripped from the child process. `claude auth status` must report a signed-in non-API/Console account. Dangerous permission bypass is not used.

If the installed Claude Code version supports `--chrome`, the bridge enables it when configured so Claude can use its Chrome integration. Claude receives the same exact-sender Google Voice return-channel instruction as Codex.

## Replies

For every accepted command, the selected agent is told the exact authenticated sender phone number and is instructed to send a short Google Voice browser reply to that same number when an authenticated browser/computer tool is available. It must emit `SMS_BRIDGE_SENT` only after confirming that Google Voice actually sent to that exact destination.

If that marker is absent, the bridge uses the safer fallback: send the short result through Gmail to the exact observed `@txt.voice.google.com` Reply-To address from the authenticated Google Voice notification. The agent is never asked to enter passwords, recovery codes, or 2FA secrets to sign into Google Voice.

## First setup

1. Run `AISMSBridge.exe`.
2. The background watchdog/host starts and your browser opens the localhost settings page.
3. Choose **App Password** or **Google API / OAuth** for Gmail; neither is preselected on a new install.
4. Complete the chosen Gmail setup and click **Test Gmail connection**.
5. Enter one or more allowed phone numbers and an SMS security code.
6. Test Codex.
7. Test Claude if you want `A:` routing.
8. Click **Start with Windows**.
9. Send a fresh Google Voice SMS.

Runtime files are stored under `%LOCALAPPDATA%\AISMSBridge`.

## Security notes

- No public listening port; the web UI binds to loopback only.
- Local setup actions require a random local session token/cookie.
- Google OAuth token **or** Gmail App Password is protected with Windows DPAPI.
- SMS code is stored as a salted, iterated hash rather than plaintext.
- Google Voice sender authorization uses trusted email envelope/header data, not phone numbers written inside the SMS body.
- Codex approval requests that reach the unattended bridge are declined.
- Claude `--dangerously-skip-permissions` is never used.
- Prompt/result bodies are not intentionally stored in state or operational logs.
- No telemetry, obfuscation, packer, auto-updater, browser-password extraction, process injection, remote-thread creation, keylogging, or code injection.
- The normal browser is opened through Windows Explorer instead of DLL-launch tricks.
- CI includes source-level checks for malware-style Windows techniques; a Defender scan can also be used when available on the runner.

See [SECURITY.md](SECURITY.md).

## Build

```powershell
go test ./...
go vet ./...
go test -race ./...
go build -trimpath -ldflags "-H=windowsgui -s -w" -o AISMSBridge.exe .
```

## Lifecycle and security tests

The Windows GitHub Actions build performs a real process-level smoke test on a fresh Windows runner: normal launch, launcher exit while background monitoring remains healthy, one-watchdog enforcement, host crash/restart, tray registration/process survival, tray crash/restart, and explicit Quit stopping all bridge processes. Windows-only unit tests also verify the current-user startup registry entry and the per-user install location.

Additional tests verify multiple-number normalization, exact Google Voice sender extraction, rejection of sender-spoof attempts in SMS content, exact return-destination instructions, immediate IMAP IDLE wakeups, and approximately one-second OAuth polling.

The only thing CI cannot visually inspect is the pixels of the notification-area icon on a human desktop; it verifies that the Windows tray process successfully registers with the shell and remains alive.

## SmartScreen / antivirus

The binary is intentionally ordinary, unobfuscated Go code and avoids process injection, browser credential scraping, keylogging, packers, DLL-launch browser tricks, and self-updaters. A newly built **unsigned** executable can still receive a Windows SmartScreen reputation warning or a third-party false positive. For a public release, Authenticode signing is strongly recommended. No project can honestly guarantee that every antivirus product will always trust a new unsigned binary.

## License

MIT.
