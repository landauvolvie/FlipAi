# FlipAi v0.46.70

This release adds Muse.ai as a first-class browser chat provider alongside ChatGPT, Claude, Gemini, Grok, and Microsoft Copilot.

## Muse.ai integration

- Added Muse to the Agents page with Connect, Test, Disconnect, live connection status, SMS shortcut settings, allowed phone numbers, and optional PIN protection.
- Muse runs inside its own persistent WebView2 profile, isolated from every other FlipAi provider and from the user's normal browser profile.
- FlipAi explicitly recognizes `auth.muse.ai` and other sign-in states, so Muse is shown as Connected only when its live chat composer is actually available.
- A saved Muse session is restored in the background after FlipAi restarts.

## SMS routing

- `MU: your message` routes a turn to Muse.
- `MU NEW: your message` starts a fresh Muse conversation and sends the message there.
- Unprefixed follow-up messages continue using Muse through FlipAi's existing sticky-routing behavior until another provider is selected.
- Attachments and long-turn/progress handling use the same hardened browser-chat plumbing as the other supported web agents.

## Reliability and isolation

- Muse has its own connection runtime, browser worker, security settings, and conversation state.
- Connect/Test/Disconnect and status endpoints are registered in the local authenticated FlipAi UI.
- Regression coverage verifies Muse routing, NEW-conversation behavior, local action routes, isolated background WebView behavior, authentication detection, and central SMS dispatch.

No Muse API key is required; FlipAi uses the user's signed-in Muse web session.

No Authenticode/code-signing certificate is included in this release.
