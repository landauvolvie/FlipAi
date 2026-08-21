# FlipAi v0.6 Windows test matrix

This release must not ship unless GitHub Actions passes the existing lifecycle suite plus the following checks.

## First-run / no-admin
- Launch from Downloads as a standard user.
- Background host and tray start without elevation.
- Local UI opens on loopback only.
- Start with Windows copies only into `%LOCALAPPDATA%` and writes only HKCU Run.
- No Windows service, Program Files write, HKLM write, firewall rule, driver, scheduled task, or elevation prompt.

## GUI lifecycle
- Closing/X-ing the settings browser leaves watchdog, host, and tray alive.
- Tray Open Settings reopens the local UI.
- Tray Quit stops all FlipAi processes and they stay stopped.
- Relaunch after Quit starts a single watchdog/host/tray set.

## Setup
- No Gmail method selected: host stays alive and UI explains setup is incomplete.
- App Password and OAuth are mutually explicit choices.
- Invalid/missing Gmail credentials show a friendly result page, not a crash.
- Allowed phone list validates/normalizes and rejects malformed numbers.
- SMS security code is required on first setup and never displayed later.
- Codex/Claude tests fail gracefully when executables or subscription auth are missing.
- Startup enable action is repeatable/idempotent.

## Runtime
- IMAP IDLE wake plus periodic fallback.
- OAuth rapid polling fallback.
- Unauthorized sender and wrong SMS code never execute.
- Agent failure returns a bounded failure reply when possible and host remains alive.
- Network loss does not crash host; later checks retry.
- Host/tray crash recovery remains functional.

## Antivirus cleanliness
- Build is unobfuscated/unpacked.
- Static source safety test forbids known injection/keylogging/credential-scraping APIs.
- Microsoft Defender scans the built EXE when available and must report no detections.
- SHA-256 is published with release artifacts.

## Remaining real-machine checks
CI cannot prove Google/ChatGPT/Claude account policy, third-party AV reputation, SmartScreen reputation, Explorer UI pixels, or the exact browser/computer tools installed in a user's Codex/Claude environment. Those need a standard-user Windows test machine before broad public release.