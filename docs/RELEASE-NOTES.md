# FlipAi v0.46.67

This release makes ChatGPT Chat and ChatGPT Work explicit route boundaries instead of letting the shared browser state leak between them.

## ChatGPT routing semantics

- `O:` always runs in regular ChatGPT Chat. If the shared browser is currently in Work and ChatGPT's picker cannot safely switch out, FlipAi opens the canonical regular Chat root rather than sending the request into Work.
- `O NEW:` always opens a fresh regular ChatGPT Chat conversation.
- `OW:` always runs in ChatGPT Work and keeps the existing Work conversation when it is already the selected route.
- `OW NEW:` always creates a fresh Work conversation, regardless of whether the browser currently shows Chat or Work.

## `OW NEW:` implementation

- `OW NEW:` no longer depends on Work exposing a `New chat` button.
- FlipAi first performs the same fresh regular-Chat reset used by the already-working `O NEW:` path, then switches that blank conversation into Work using the same selector used by `OW:`.
- The old Work conversation therefore cannot be reused by an explicit `OW NEW:` request.

## Mode verification

- FlipAi no longer treats a page as regular Chat merely because a visible Work label lacks `aria-selected` or similar selected-state attributes.
- Chat/Work picker detection now also recognizes `aria-haspopup` and tabindex-based controls.

No Authenticode/code-signing certificate is included in this release.
