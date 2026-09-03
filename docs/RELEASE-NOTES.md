# FlipAi v0.46.21

Gemini Chat is now a fifth independent FlipAi agent, using the user's regular Gemini account in a dedicated persistent browser session at gemini.google.com. It does not use the Gemini API or require Gemini CLI.

## Gemini Chat

The Agents page now includes Gemini Chat with Connect, Test, Disconnect, routing, access, security, and shared SMS settings. The default SMS shortcut is `M:`. Once selected for a phone, unprefixed follow-up texts stay with Gemini Chat until another agent is selected.

Gemini Chat has its own WebView2 profile and its own phone/PIN security boundary. A saved sign-in is restored invisibly after FlipAi or Windows restarts, while the singleton browser owner prevents duplicate hidden Gemini browser trees.

## Connection and regression protection

Connect opens the dedicated Gemini sign-in window at gemini.google.com. Test performs a real turn through that browser session. Connect, Test, and Disconnect are all registered in FlipAi's authenticated POST action router, with route-level regression coverage so they cannot fall through to a local 404 like the Claude Chat v0.46.19 issue.

The release continues to run Linux/browser tests plus Windows unit, vet, race, build, lifecycle, Google Voice, Defender, installer, and real install/uninstall smoke gates before publishing.
