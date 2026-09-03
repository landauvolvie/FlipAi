# FlipAi v0.46.23

Gemini Chat and Grok Chat now use the same long-running WebView turn allowance as ChatGPT Chat, so a model reply that is already visible in the browser is not falsely reported as a Runtime.evaluate timeout.

## Gemini Chat

Gemini's WebView turn is no longer limited by the 8-second Google Voice DevTools deadline. The existing 90-second page driver now receives the same 95-second DevTools allowance as ChatGPT Chat.

Multiline SMS prompts are inserted into Gemini's Quill/contenteditable composer line by line. This preserves the blank line and shared SMS reply instruction instead of dropping everything after the user's first line.

## Grok Chat

Grok sign-in detection now uses Grok's actual ProseMirror/TipTap composer and explicit login-page detection instead of a copied Gemini selector plus a broad search for any Sign in/Log in button.

Grok response tracking now accepts either a newly-created assistant response or a changed last response container. It also ignores hidden Stop controls. A fast visible Grok answer therefore completes the FlipAi turn promptly, cancelling delayed working/progress texts instead of continuing to report that Grok is still working.

Regression coverage verifies the long DevTools timeout for ChatGPT, Gemini and Grok and locks in the Gemini multiline and Grok sign-in/response fixes.
