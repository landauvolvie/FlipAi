# FlipAi v0.46.14

This release adds regular ChatGPT Chat to Google Voice SMS as `G:` and replaces the old global default-agent behavior with a sticky per-phone selection. It also fixes the remaining full-Windows-restart failure of the hidden ChatGPT browser.

## G: is ChatGPT Chat

- `C:` selects Codex.
- `A:` selects Claude.
- `G:` selects regular ChatGPT Chat through FlipAi's already-connected private WebView.
- `G: NEW` starts a clean saved ChatGPT conversation.
- ChatGPT replies return through the same Google Voice reply path as the other agents.

## No default agent: selection is sticky

An explicit C:, A:, or G: becomes that phone number's active SMS destination. Every later unprefixed text keeps going to the same agent until another prefix changes it. The selection is stored per sender and survives FlipAi/Windows restarts. A shared phone with no prior selection is asked to choose C:, A:, or G: instead of silently falling back to Codex.

G uses the sender's existing allowed Codex/Claude phone authorization. If every allowed path for that sender requires a security code, a valid existing agent code is still required before G is accepted.

## Full reboot ChatGPT recovery

v0.46.13 correctly preserved the signed-in WebView profile, but Windows could terminate the process before FlipAi cleared process-only `Running`/`SignedIn`/control-port fields. On the next boot the tray could temporarily trust those stale fields and not launch the hidden browser until the first message forced a retry.

v0.46.14 verifies the private worker at tray startup, discards only stale process metadata while keeping the saved login/profile and conversation id, immediately starts the hidden WebView, waits longer for a cold-boot network/auth restore, and recycles a hidden worker that remains alive but never restores sign-in. No login popup is opened by recovery. The visible Connect ChatGPT window remains a one-time sign-in step only, unless ChatGPT itself later expires the saved account session.

## Diagnostics and tests

Activity records stale-state recovery, invisible restore attempts, liveness failures, auth restore and worker recycling without logging prompts, replies, cookies or tokens. Tests cover G routing, sticky follow-ups and switching, persistence by sender, no-default UI, reboot-stale-state recovery, the existing real Chromium ChatGPT driver, Windows race/lifecycle checks, Google Voice regression checks, Defender, and installer smoke tests.
