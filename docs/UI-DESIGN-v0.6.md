# FlipAi v0.6 UI direction

Modern, browser-hosted Windows utility UI served only on localhost. The background bridge remains a separate process, so closing the settings window never stops monitoring.

## First-run experience

1. Welcome — explains Google Voice → Gmail → FlipAi → Codex/Claude.
2. Gmail — choose App Password or personal OAuth project.
3. Phone security — add one or more allowed numbers and an SMS security code.
4. Agents — test Codex and Claude independently.
5. Startup — install per-user startup without administrator rights.
6. Ready — shows example `C:` and `A:` texts and live status.

## Visual language

- Light neutral canvas, dark ink, violet accent, green healthy state, amber attention state.
- Rounded but restrained panels, clear 8px spacing system, large readable type.
- Status pills for Bridge, Gmail, Codex, Claude, and Startup.
- Inline success/error banners; no unstyled browser error pages for normal setup actions.
- Responsive desktop/mobile layout.
- No external fonts, scripts, analytics, CDNs, or remote UI dependencies.

## Settings sections

- Overview
- Gmail connection
- Allowed phone numbers
- SMS security code
- Agent routing
- Codex path
- Claude path
- Working folder
- Start with Windows
- Diagnostics / status
- Quit Bridge

## Behavior

Closing the browser page does not stop FlipAi. Tray → Quit or Settings → Quit stops tray, host, and watchdog. First-run setup never requires administrator privileges.