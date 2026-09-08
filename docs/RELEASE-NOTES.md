# FlipAi v0.46.68

This release changes `OW NEW:` to literally reuse the two paths that are already proven on the live account instead of trying to invent a special Work-new transition.

## `OW NEW:` is now `O NEW` + normal `OW`

- Stage 1 uses the exact regular-Chat reset primitive used by the working `O NEW:` command.
- FlipAi does **not** ask ChatGPT Work to create or verify a new Work session during that reset.
- The user's command keeps its Work route marker.
- Stage 2 runs the user's message through the normal `OW:` send path, exactly as if the user had first created a fresh ChatGPT Chat and then sent `OW:`.
- This avoids the failing `/new`-in-Work path entirely.

## Existing routing semantics retained

- `O:` always targets regular ChatGPT Chat.
- `O NEW:` creates a fresh regular ChatGPT Chat.
- `OW:` targets ChatGPT Work and can continue the current Work conversation.
- `OW NEW:` resets through regular Chat first, then enters Work through the normal `OW:` route for the first user turn.

## Regression coverage

- Added a test that explicitly locks the fresh Work plan to `reset=Chat` while preserving `turn=Work`.
- Existing parsing tests continue to verify that `OW NEW:` retains the Work marker and the user's text.

No Authenticode/code-signing certificate is included in this release.
