# FlipAi v0.46.17

This release combines the pending full Google Voice reply fix with a streamlined three-agent SMS setup.

## Complete ChatGPT replies

Long ChatGPT answers are delivered as one logical Google Voice reply instead of numbered `1/2`, `2/2` fragments. FlipAi reassembles the response before handing it to the Google Voice email gateway, so the full answer survives instead of only the first line of each fragment.

## Smarter ChatGPT acknowledgement

Codex and Claude still acknowledge immediately because their turns commonly run for minutes. ChatGPT Chat now waits 30 seconds by default. If ChatGPT answers before then, the phone receives only the answer. If it is still working after 30 seconds, FlipAi sends the normal `ChatGPT Chat working on it` receipt and continues waiting for the final answer.

The receipt delay is configurable per agent. Codex and Claude default to immediate; ChatGPT defaults to 30 seconds.

## One familiar agent setup

Codex, Claude, and ChatGPT Chat now share the same SMS-facing settings pattern: editable shortcut, shared new-conversation word, allowed phone numbers, optional per-agent PIN (off by default for new setup), receipt/progress controls, and one shared editable SMS instruction.

ChatGPT Chat now has its own phone allowlist and PIN instead of borrowing Codex or Claude permissions. Existing installations migrate their currently SMS-enabled phone numbers to ChatGPT Chat once so G: keeps working after upgrade. Fresh installs start directly in the new three-agent schema, so adding a number to one agent never silently adds it to another.

There is no default SMS agent. C:, A:, or the configured ChatGPT shortcut selects the agent for that phone, and unprefixed follow-up texts stay with the selected agent across app and PC restarts until another shortcut is used.

Connection controls are kept at the top of each agent pane. A disconnected agent shows Connect; a connected agent shows Disconnect and Test. The ChatGPT pane removes the duplicate chat/connection clutter and keeps detailed diagnostics collapsed.

The built-in shared SMS instruction is now simply: `Reply for SMS. Keep it brief and plain text.` Editing it in any agent pane changes it for all three agents.
