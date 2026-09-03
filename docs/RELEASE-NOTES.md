# FlipAi v0.46.22

Grok Chat is now a sixth independent FlipAi agent, using the user's regular xAI Grok account in a dedicated persistent browser session at grok.com. It does not use the xAI API.

## Grok Chat

The Agents page now includes Grok Chat with Connect, Test, Disconnect, routing, access, security, and the same shared SMS instruction used by the other agents. The default SMS shortcut is `X:`. Once selected for a phone, unprefixed follow-up texts stay with Grok Chat until another agent is selected.

Grok Chat has its own WebView2 profile and its own phone/PIN security boundary. A saved sign-in is restored invisibly after FlipAi or Windows restarts. The browser worker uses the same singleton owner, cheap liveness probe, and restart throttling as ChatGPT Chat, Claude Chat, and Gemini Chat so a slow Grok renderer cannot spawn duplicate hidden WebView2 trees and fill RAM.

## grok.com browser control

FlipAi targets Grok's current TipTap / ProseMirror contenteditable prompt, with multiple fallback selectors for the prompt, send/stop controls, and response containers. Connect opens grok.com in FlipAi's private profile; Test performs a real browser turn through that saved session.

Connect, Test, and Disconnect are all registered in FlipAi's authenticated POST router, with route-level regression coverage so they cannot fall through to a local 404.

The normal Linux/browser tests plus Windows unit, vet, race, build, lifecycle, Google Voice, Defender, installer, and real install/uninstall smoke gates still run before publishing.
