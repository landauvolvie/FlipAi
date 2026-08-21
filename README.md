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

- **Launcher/UI opener** — double-click `AISMSBridge.exe`; it makes sure the background bridge is alive and opens the local settings page.
- **Watchdog** — stays hidden and restarts the background host if the host crashes.
- **Background host** — polls Gmail and talks to Codex/Claude.

Closing the browser settings page **does not stop the bridge**. Only the **Quit Bridge** button stops the current watchdog/host. Launching the EXE again starts it again.

No administrator rights are required. “Start with Windows” copies the EXE to `%LOCALAPPDATA%\Programs\AISMSBridge\AISMSBridge.exe` and adds a current-user `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` entry. It starts after that Windows user signs in and continues while the workstation is locked. Sleep/hibernate pauses it.

## SMS routing

During setup choose an SMS security code. Every remote command must begin with it.

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

## Gmail / Google Voice

1. Enable Google Voice **Forward messages to email**.
2. In the bridge GUI, choose exactly one Gmail connection method. New installs have **no default**.

### Option 1 — Gmail App Password

This is the easiest independent setup and requires no Google Cloud project.

1. Turn on Google 2-Step Verification.
2. Create a 16-character Google App Password for the bridge.
3. Enter the Gmail address + App Password in the GUI.
4. Click **Test Gmail connection**.

The bridge connects directly to `imap.gmail.com:993` over TLS to read Voice notifications and `smtp.gmail.com:465` over TLS to send the Google Voice reply fallback. The App Password is protected locally with Windows DPAPI and is never stored in plaintext by the Windows build.

Google may not offer App Passwords for some managed/Advanced Protection/security-key-only accounts. Those users should choose OAuth instead.

### Option 2 — Your own Google API / OAuth project

1. Create your own Google Cloud project.
2. Enable Gmail API.
3. Create an OAuth **Desktop app**.
4. Upload that Desktop OAuth JSON in the local setup page.
5. Click **Connect / Reconnect Gmail**, then **Test Gmail connection**.

The OAuth backend requests `gmail.readonly` to inspect Google Voice notifications and `gmail.send` for the Google Voice email-reply fallback. OAuth tokens are protected locally with Windows DPAPI.

**OAuth testing warning:** for personal Google Cloud OAuth apps, refresh-token lifetime can be limited while the consent screen remains in Testing. If Gmail disconnects later, review the current Google OAuth publishing/test-user rules for that user's own project.

Incoming commands are accepted only after Google Voice sender checks, Google DKIM validation, the configured source phone number, and the SMS security code all pass.

## Codex connection

The bridge starts the official local interface:

```text
codex app-server --listen stdio://
```

It initializes the JSON-RPC connection, checks `account/read`, and refuses Codex work unless the account type is `chatgpt`. It creates/resumes a persistent Codex thread and sends each `C:` message as a turn.

If the App Server process dies, the bridge reports the failed task and automatically creates a fresh App Server connection on the next Codex SMS. The outer Windows watchdog separately restarts the entire bridge host if the host itself crashes.

A separately launched App Server is not guaranteed to expose every Desktop-only browser/computer tool. The prompt asks Codex to send the Google Voice browser reply when such a tool is actually available; otherwise Gmail is the fallback return path.

## Claude connection

The bridge invokes the official Claude Code CLI in non-interactive mode:

```text
claude -p "..." --output-format json
```

It stores the returned `session_id` and uses `--resume` for later `A:` messages. API-related Anthropic environment variables are stripped from the child process. `claude auth status` must report a signed-in non-API/Console account. Dangerous permission bypass is not used.

If the installed Claude Code version supports `--chrome`, the bridge enables it when configured so Claude can use its Chrome integration.

## Replies

The selected agent is asked to send a short Google Voice browser reply when it has an available authenticated browser/computer tool. It must emit `SMS_BRIDGE_SENT` only after that succeeds.

If that marker is absent, the bridge tries the safer fallback: send the short result through Gmail to the exact observed `@txt.voice.google.com` Reply-To address from the authenticated Google Voice notification.

## First setup

1. Run `AISMSBridge.exe`.
2. The background watchdog/host starts and your browser opens the localhost settings page.
3. Choose **App Password** or **Google API / OAuth** for Gmail; neither is preselected on a new install.
4. Complete the chosen Gmail setup and click **Test Gmail connection**.
5. Enter the allowed phone number and SMS security code.
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
- Codex approval requests that reach the unattended bridge are declined.
- Claude `--dangerously-skip-permissions` is never used.
- Prompt/result bodies are not intentionally stored in state or operational logs.
- No telemetry, obfuscation, packer, auto-updater, or browser-password extraction.

See [SECURITY.md](SECURITY.md).

## Build

```powershell
go test ./...
go vet ./...
go test -race ./...
go build -trimpath -ldflags "-H=windowsgui -s -w" -o AISMSBridge.exe .
```

## SmartScreen / antivirus

The binary is intentionally ordinary, unobfuscated Go code. A newly built **unsigned** executable can still receive a Windows SmartScreen reputation warning. For a public release, use Authenticode signing and publish SHA-256 checksums/reproducible CI artifacts. No project can honestly guarantee that every antivirus engine will always classify an unsigned new binary correctly.

## License

MIT.
