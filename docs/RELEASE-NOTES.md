# FlipAi v0.46.69

This release fixes browser-agent reconnect behavior after restart or sign-in loss.

## Accurate connection status

- ChatGPT Chat, Claude Chat, Gemini, Grok, and Microsoft Copilot now show Connected only when the live private browser is actually signed in.
- If a session expires or fails to restore, FlipAi shows the agent as disconnected and presents Connect immediately; users no longer have to press Disconnect first.

## Sign-in windows open visibly

- Connect now restores and brings each agent sign-in window to the foreground instead of leaving it minimized or hidden behind FlipAi.

## Reliable reconnects without Task Manager

- FlipAi now closes and releases the WebView2 controller and profile cleanly before restarting a browser agent.
- This prevents the stale profile lock/race that could leave the next sign-in window blank white.
- Reconnecting multiple browser agents should no longer require killing FlipAi processes between agents.

## Regression coverage

- Added coverage that locks visible browser-agent connection status to live sign-in state.

No Authenticode/code-signing certificate is included in this release.
