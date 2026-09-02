# FlipAi v0.46.11

This release continues the direct regular-ChatGPT investigation after v0.46.10 proved that the desktop Electron bridge is internal IPC rather than a Codex-style externally callable local backend.

## Independent ChatGPT cloud protocol map

- Maps OAuth authorize/token endpoint literals from the installed ChatGPT application package.
- Maps safe public OAuth client-id literals, redirect URIs, scopes, and PKCE mechanics without returning verifier/challenge values.
- Maps the regular ChatGPT conversation endpoint, request-field names, and new/continued-conversation state identifiers.
- Maps HTTP header names only; header values are never returned.
- Maps SSE/WebSocket response framing used by regular Chat.
- Identifies browser/session/device/anti-abuse dependency markers so a browser-cookie requirement cannot be mistaken for a clean OAuth flow.
- Adds a CLOUD PATH ASSESSMENT explaining whether an explicit independently authorized OAuth proof is justified or whether a dedicated signed-in WebView is the safer fallback.

## Privacy boundary

The mapper reads installed application-package code only. It does not read ChatGPT cookies, access or refresh tokens, passwords, Local Storage, IndexedDB, browser profile data, process memory, request bodies, packet payloads, or credential values. It does not use accessibility, mouse/keyboard automation, DevTools, or a hidden ChatGPT browser.

## What to do

Keep ChatGPT desktop open and signed in. In FlipAi go to **Agents -> ChatGPT Chat -> Map independent cloud auth** and copy the full result, especially **OAUTH ENDPOINT**, **PUBLIC CLIENT ID**, **REDIRECT URI**, **OAUTH SCOPE**, **OAUTH MECHANIC**, **CONVERSATION ENDPOINT**, **HEADER NAME**, **REQUEST FIELD**, **CONVERSATION STATE**, **STREAM FORMAT**, **SESSION DEPENDENCY**, and **CLOUD PATH ASSESSMENT**.

ChatGPT Chat remains intentionally **Not connected** in this release. No message is sent until the diagnostic proves a safe independent authorization path.
