# FlipAi v0.46.12

This is the first live regular-ChatGPT browser connection in FlipAi. It replaces the diagnostic-only ChatGPT pane with a dedicated persistent WebView2 session that can sign in, run a real test turn, continue a normal ChatGPT conversation, and start a new one without controlling the visible ChatGPT desktop app.

## ChatGPT Chat connection

- **Connect ChatGPT** opens a one-time sign-in window using a FlipAi-owned WebView2 profile.
- The signed-in profile persists in FlipAi and is separate from the normal ChatGPT desktop/browser profile.
- **Test ChatGPT** sends a real harmless prompt and waits for the completed assistant answer.
- The Agents page includes a real ChatGPT message box for end-to-end conversation testing.
- Normal messages continue the current ChatGPT conversation; **Start a new ChatGPT chat** creates a new saved chat first.
- FlipAi records the ChatGPT conversation id from the normal conversation URL after a successful turn.
- **Disconnect** stops the dedicated browser and removes only FlipAi's ChatGPT profile.

## No Windows accessibility or focus automation

The ChatGPT browser runs through WebView2 owned by FlipAi. Sending uses the page's own composer and Send control inside that WebView, and receiving reads the completed assistant message inside the same page. It does not use Windows accessibility, SendKeys, global mouse input, coordinates, focus the normal ChatGPT app, copy its cookies/tokens, or enable a remote DevTools port.

## Activity diagnostics

The existing Activity tab now records ChatGPT browser open, verified sign-in, background worker start, test turns, chat turns, disconnects, failures, and end-to-end durations. Activity intentionally does not store ChatGPT prompts, assistant replies, cookies, or tokens.

## Reliability tests

The release suite covers the persistent-profile/runtime contract, private loopback authentication, no-global-UI-automation regression rules, conversation-id handling, Activity privacy, installer lifecycle, and the exact ChatGPT page driver in a real headless Chromium page that streams a changing assistant answer before completion.

## What to do

Install v0.46.12, open **Agents -> ChatGPT Chat**, press **Connect ChatGPT**, sign in to your normal ChatGPT account, then press **Test ChatGPT**. A successful test returns the real ChatGPT reply and conversation id. You can then use the message box on the same page to continue that chat or start a new one. If anything fails, open **Activity** and send the ChatGPT-related entries.

This release proves and exposes the live ChatGPT browser connection inside FlipAi. SMS routing to ChatGPT remains separate from Codex/Claude routing in this release and is not silently enabled until the browser connection has been proven on the user's real account.
